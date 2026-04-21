package routing

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

const circuitBreakerName = "circuit-breaker"

type CircuitBreakerParams struct {
	FailureThreshold      int  `json:"failureThreshold"`
	BaseCooldownMs        int  `json:"baseCooldownMs"`
	MaxCooldownMs         int  `json:"maxCooldownMs"`
	BackoffMultiplier     int  `json:"backoffMultiplier"`
	HalfOpenMaxRequests   int  `json:"halfOpenMaxRequests"`
	CloseAfterSuccesses   int  `json:"closeAfterSuccesses"`
	CountPostCommitErrors bool `json:"countPostCommitErrors"`
	RateLimitCooldownMs   int  `json:"rateLimitCooldownMs"`
}

type circuitBreakerFactory struct{}

type circuitBreakerStrategy struct {
	params CircuitBreakerParams
	clock  Clock
	store  StateStore
}

type circuitBreakerSession struct {
	strategy   *circuitBreakerStrategy
	protocol   string
	candidates []Candidate
	nextIndex  int
}

func init() {
	MustRegisterFactory(circuitBreakerFactory{})
}

func defaultCircuitBreakerParams() CircuitBreakerParams {
	return CircuitBreakerParams{
		FailureThreshold:      2,
		BaseCooldownMs:        30_000,
		MaxCooldownMs:         300_000,
		BackoffMultiplier:     2,
		HalfOpenMaxRequests:   1,
		CloseAfterSuccesses:   1,
		CountPostCommitErrors: true,
		RateLimitCooldownMs:   15_000,
	}
}

func (c circuitBreakerFactory) Name() string {
	return circuitBreakerName
}

func (c circuitBreakerFactory) Describe() Descriptor {
	defaults := paramsToMap(defaultCircuitBreakerParams())
	return Descriptor{
		Name:        circuitBreakerName,
		DisplayName: "Circuit Breaker",
		Description: "Skip recently unhealthy providers and probe them again after cooldown.",
		Defaults:    defaults,
		Parameters: []ParameterDescriptor{
			intParam("failureThreshold", "Consecutive retryable failures required before opening the circuit.", 1, nil, defaults["failureThreshold"]),
			intParam("baseCooldownMs", "Initial cooldown applied after the circuit opens.", 1000, nil, defaults["baseCooldownMs"]),
			intParam("maxCooldownMs", "Upper bound for exponential cooldown backoff.", 1000, nil, defaults["maxCooldownMs"]),
			intParam("backoffMultiplier", "Multiplier used when the same provider keeps reopening.", 1, nil, defaults["backoffMultiplier"]),
			intParam("halfOpenMaxRequests", "Maximum concurrent probe requests allowed while half-open.", 1, nil, defaults["halfOpenMaxRequests"]),
			intParam("closeAfterSuccesses", "Successful probe count required to close the circuit again.", 1, nil, defaults["closeAfterSuccesses"]),
			boolParam("countPostCommitErrors", "Whether stream failures after response start should count against provider health.", defaults["countPostCommitErrors"]),
			intParam("rateLimitCooldownMs", "Optional cooldown override for 429 responses.", 0, nil, defaults["rateLimitCooldownMs"]),
		},
	}
}

func (c circuitBreakerFactory) ResolveParams(raw json.RawMessage) (map[string]any, error) {
	params, err := decodeCircuitBreakerParams(raw)
	if err != nil {
		return nil, err
	}
	return paramsToMap(params), nil
}

func (c circuitBreakerFactory) New(raw json.RawMessage, deps Dependencies) (Strategy, error) {
	params, err := decodeCircuitBreakerParams(raw)
	if err != nil {
		return nil, err
	}
	return &circuitBreakerStrategy{params: params, clock: deps.Clock, store: deps.Store}, nil
}

func (s *circuitBreakerStrategy) Name() string {
	return circuitBreakerName
}

func (s *circuitBreakerStrategy) NewSession(input SessionInput) Session {
	return &circuitBreakerSession{
		strategy:   s,
		protocol:   input.Protocol,
		candidates: append([]Candidate(nil), input.Candidates...),
	}
}

func (s *circuitBreakerSession) Next() (Decision, bool) {
	for s.nextIndex < len(s.candidates) {
		candidate := s.candidates[s.nextIndex]
		s.nextIndex++
		key := StateKey{Strategy: circuitBreakerName, Protocol: s.protocol, ProviderID: candidate.ProviderID}
		now := s.strategy.clock.Now()
		skip := false
		reason := ""
		s.strategy.store.Update(key, func(state ProviderState) ProviderState {
			if state.Status == "open" && !state.OpenUntil.IsZero() && !now.Before(state.OpenUntil) {
				state.Status = "half-open"
				state.OpenUntil = time.Time{}
				state.ConsecutiveSuccesses = 0
			}
			switch state.Status {
			case "open":
				skip = true
				reason = "circuit_open"
			case "half-open":
				if state.HalfOpenInFlight >= s.strategy.params.HalfOpenMaxRequests {
					skip = true
					reason = "half_open_busy"
					return state
				}
				state.HalfOpenInFlight++
			}
			return state
		})
		if skip {
			return Decision{Candidate: candidate, Skip: true, SkipReason: reason}, true
		}
		return Decision{Candidate: candidate}, true
	}
	return Decision{}, false
}

func (s *circuitBreakerSession) Report(feedback AttemptFeedback) {
	key := StateKey{Strategy: circuitBreakerName, Protocol: s.protocol, ProviderID: feedback.Candidate.ProviderID}
	now := feedback.FinishedAt
	if now.IsZero() {
		now = s.strategy.clock.Now()
	}
	countFailure := feedback.Outcome == OutcomeRetryableFail || (feedback.Outcome == OutcomePostCommitFail && s.strategy.params.CountPostCommitErrors)
	reachable := feedback.Outcome == OutcomeSuccess || feedback.Outcome == OutcomeTerminalFail
	s.strategy.store.Update(key, func(state ProviderState) ProviderState {
		if state.Status == "half-open" && state.HalfOpenInFlight > 0 {
			state.HalfOpenInFlight--
		}
		if reachable {
			state.LastSuccessAt = now
			state.LastFailureReason = ""
			state.ConsecutiveFailures = 0
			if state.Status == "half-open" {
				state.ConsecutiveSuccesses++
				if state.ConsecutiveSuccesses >= s.strategy.params.CloseAfterSuccesses {
					return ProviderState{LastSuccessAt: now}
				}
				return state
			}
			state.ConsecutiveSuccesses = 0
			state.Status = ""
			state.OpenUntil = time.Time{}
			state.CooldownMs = 0
			state.OpenCount = 0
			return state
		}
		if !countFailure {
			return state
		}
		state.LastFailureAt = now
		state.LastFailureReason = string(feedback.FailureReason)
		if state.Status == "half-open" {
			return s.openState(state, feedback, now)
		}
		state.ConsecutiveFailures++
		if state.ConsecutiveFailures < s.strategy.params.FailureThreshold {
			return state
		}
		return s.openState(state, feedback, now)
	})
}

func (s *circuitBreakerSession) openState(state ProviderState, feedback AttemptFeedback, now time.Time) ProviderState {
	state.Status = "open"
	state.ConsecutiveSuccesses = 0
	state.HalfOpenInFlight = 0
	state.OpenCount++
	state.CooldownMs = s.cooldownMs(state, feedback)
	state.OpenUntil = now.Add(time.Duration(state.CooldownMs) * time.Millisecond)
	return state
}

func (s *circuitBreakerSession) cooldownMs(state ProviderState, feedback AttemptFeedback) int {
	if feedback.FailureReason == FailureRateLimited && s.strategy.params.RateLimitCooldownMs > 0 {
		return s.strategy.params.RateLimitCooldownMs
	}
	cooldown := float64(s.strategy.params.BaseCooldownMs)
	if state.OpenCount > 1 && s.strategy.params.BackoffMultiplier > 1 {
		cooldown *= math.Pow(float64(s.strategy.params.BackoffMultiplier), float64(state.OpenCount-1))
	}
	if cooldown > float64(s.strategy.params.MaxCooldownMs) {
		cooldown = float64(s.strategy.params.MaxCooldownMs)
	}
	if cooldown < 1000 {
		cooldown = 1000
	}
	return int(cooldown)
}

func decodeCircuitBreakerParams(raw json.RawMessage) (CircuitBreakerParams, error) {
	params := defaultCircuitBreakerParams()
	if len(raw) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return CircuitBreakerParams{}, fmt.Errorf("decode circuit-breaker params: %w", err)
	}
	if params.FailureThreshold < 1 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.failureThreshold must be greater than 0")
	}
	if params.BaseCooldownMs < 1000 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.baseCooldownMs must be at least 1000")
	}
	if params.MaxCooldownMs < params.BaseCooldownMs {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.maxCooldownMs must be greater than or equal to baseCooldownMs")
	}
	if params.BackoffMultiplier < 1 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.backoffMultiplier must be greater than 0")
	}
	if params.HalfOpenMaxRequests < 1 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.halfOpenMaxRequests must be greater than 0")
	}
	if params.CloseAfterSuccesses < 1 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.closeAfterSuccesses must be greater than 0")
	}
	if params.RateLimitCooldownMs < 0 {
		return CircuitBreakerParams{}, fmt.Errorf("routing.params.rateLimitCooldownMs must be greater than or equal to 0")
	}
	return params, nil
}

func paramsToMap(params CircuitBreakerParams) map[string]any {
	return map[string]any{
		"failureThreshold":      params.FailureThreshold,
		"baseCooldownMs":        params.BaseCooldownMs,
		"maxCooldownMs":         params.MaxCooldownMs,
		"backoffMultiplier":     params.BackoffMultiplier,
		"halfOpenMaxRequests":   params.HalfOpenMaxRequests,
		"closeAfterSuccesses":   params.CloseAfterSuccesses,
		"countPostCommitErrors": params.CountPostCommitErrors,
		"rateLimitCooldownMs":   params.RateLimitCooldownMs,
	}
}

func intParam(key string, description string, min float64, max *float64, defaultValue any) ParameterDescriptor {
	minValue := min
	return ParameterDescriptor{
		Key:          key,
		Type:         "int",
		Required:     true,
		DefaultValue: defaultValue,
		Description:  description,
		Min:          &minValue,
		Max:          max,
	}
}

func boolParam(key string, description string, defaultValue any) ParameterDescriptor {
	return ParameterDescriptor{
		Key:          key,
		Type:         "bool",
		Required:     true,
		DefaultValue: defaultValue,
		Description:  description,
	}
}
