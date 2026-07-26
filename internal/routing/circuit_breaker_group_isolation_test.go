package routing

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func dualGroupCandidates() (fragile, stable Candidate) {
	fragile = Candidate{
		Index:      0,
		ProviderID: "vendor-cb",
		GroupID:    "fragile",
		Provider:   "vendor-cb",
		Protocol:   "openai-responses",
		Model:      "model-cb",
		BaseURL:    "https://vendor-cb.example.test/v1",
	}
	stable = Candidate{
		Index:      1,
		ProviderID: "vendor-cb",
		GroupID:    "stable",
		Provider:   "vendor-cb",
		Protocol:   "openai-responses",
		Model:      "model-cb",
		BaseURL:    "https://vendor-cb.example.test/v1",
	}
	return fragile, stable
}

func newCircuitBreakerForTest(t *testing.T, clock Clock, store StateStore, rawParams string) Strategy {
	t.Helper()
	params := json.RawMessage(rawParams)
	strategy, err := Build(Config{Strategy: circuitBreakerName, Params: params}, Dependencies{Clock: clock, Store: store})
	if err != nil {
		t.Fatalf("Build circuit-breaker: %v", err)
	}
	return strategy
}

func nextDecision(t *testing.T, session Session) Decision {
	t.Helper()
	decision, ok := session.Next()
	if !ok {
		t.Fatal("expected decision, session exhausted")
	}
	return decision
}

func reportRetryable(session Session, candidate Candidate, reason FailureReason, status int, at time.Time) {
	session.Report(AttemptFeedback{
		Candidate:     candidate,
		StartedAt:     at,
		FinishedAt:    at,
		Outcome:       OutcomeRetryableFail,
		FailureReason: reason,
		StatusCode:    status,
		Retryable:     true,
	})
}

func reportSuccess(session Session, candidate Candidate, at time.Time) {
	session.Report(AttemptFeedback{
		Candidate:  candidate,
		StartedAt:  at,
		FinishedAt: at,
		Outcome:    OutcomeSuccess,
	})
}

func TestCandidateStableIdentityKeepsFieldsSeparate(t *testing.T) {
	t.Parallel()

	c := Candidate{ProviderID: "vendor-a", GroupID: "premium", Model: "model-a"}
	id := c.StableIdentity()
	if id.ProviderID != "vendor-a" || id.GroupID != "premium" || id.Model != "model-a" {
		t.Fatalf("StableIdentity() = %#v", id)
	}
	if id.Model == id.GroupID || id.Model == id.ProviderID+"/"+id.GroupID || id.Model == id.GroupID+"/"+c.Model {
		t.Fatalf("Model must not embed GroupID; got Model=%q GroupID=%q", id.Model, id.GroupID)
	}

	key := StateKeyForCandidate(circuitBreakerName, "openai", c)
	if key.ProviderID != "vendor-a" || key.GroupID != "premium" || key.Model != "model-a" {
		t.Fatalf("StateKeyForCandidate() = %#v", key)
	}

	legacy := ProviderScopeStateKey(circuitBreakerName, "openai", "vendor-a")
	if legacy.GroupID != "" || legacy.Model != "" || legacy.ProviderID != "vendor-a" {
		t.Fatalf("ProviderScopeStateKey() = %#v, want explicit zero GroupID/Model", legacy)
	}
}

func TestCircuitBreakerIsolatesFailuresAcrossGroups(t *testing.T) {
	t.Parallel()

	fragile, stable := dualGroupCandidates()
	params := `{
		"failureThreshold":1,
		"baseCooldownMs":60000,
		"maxCooldownMs":60000,
		"backoffMultiplier":2,
		"halfOpenMaxRequests":1,
		"closeAfterSuccesses":1,
		"countPostCommitErrors":true,
		"rateLimitCooldownMs":60000
	}`

	// Same provider, different groups: 401 / 429 / 5xx on fragile must not pollute stable.
	for _, tc := range []struct {
		name   string
		reason FailureReason
		status int
	}{
		{name: "401", reason: FailureUpstream4xx, status: 401},
		{name: "429", reason: FailureRateLimited, status: 429},
		{name: "5xx", reason: FailureUpstream5xx, status: 503},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := &manualClock{now: time.Unix(1_700_000_000, 0).UTC()}
			store := NewMemoryStateStore()
			strategy := newCircuitBreakerForTest(t, clock, store, params)

			session := strategy.NewSession(SessionInput{
				Now:        clock.Now(),
				Protocol:   "openai-responses",
				Alias:      "cb-iso",
				Candidates: []Candidate{fragile, stable},
			})
			d1 := nextDecision(t, session)
			if d1.Skip || d1.Candidate.GroupID != "fragile" {
				t.Fatalf("first decision = %#v, want fragile attempt", d1)
			}
			reportRetryable(session, fragile, tc.reason, tc.status, clock.Now())

			d2 := nextDecision(t, session)
			if d2.Skip || d2.Candidate.GroupID != "stable" {
				t.Fatalf("second decision after %d = %#v, want stable attempt (not contaminated)", tc.status, d2)
			}
			reportSuccess(session, stable, clock.Now())

			fragileState := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile))
			stableState := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", stable))
			if fragileState.Status != "open" {
				t.Fatalf("fragile state after %d = %#v, want open", tc.status, fragileState)
			}
			if stableState.Status == "open" || stableState.ConsecutiveFailures != 0 {
				t.Fatalf("stable state after fragile %d = %#v, want healthy", tc.status, stableState)
			}

			// Distinct state keys: provider-only key must not equal group-scoped keys.
			providerOnly := ProviderScopeStateKey(circuitBreakerName, "openai-responses", fragile.ProviderID)
			if providerOnly == StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile) {
				t.Fatalf("group-scoped key must differ from provider-only wrapper")
			}
			if StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile) == StateKeyForCandidate(circuitBreakerName, "openai-responses", stable) {
				t.Fatalf("sibling groups must use distinct StateKeys")
			}
		})
	}
}

func TestCircuitBreakerSkipsOnlyOpenGroupOnNextSession(t *testing.T) {
	t.Parallel()

	fragile, stable := dualGroupCandidates()
	clock := &manualClock{now: time.Unix(1_700_000_000, 0).UTC()}
	store := NewMemoryStateStore()
	strategy := newCircuitBreakerForTest(t, clock, store, `{
		"failureThreshold":1,
		"baseCooldownMs":60000,
		"maxCooldownMs":60000,
		"backoffMultiplier":2,
		"halfOpenMaxRequests":1,
		"closeAfterSuccesses":1,
		"countPostCommitErrors":true,
		"rateLimitCooldownMs":60000
	}`)

	openSession := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	d := nextDecision(t, openSession)
	reportRetryable(openSession, d.Candidate, FailureRateLimited, 429, clock.Now())

	nextSession := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	skip := nextDecision(t, nextSession)
	if !skip.Skip || skip.SkipReason != "circuit_open" || skip.Candidate.GroupID != "fragile" {
		t.Fatalf("expected fragile circuit_open skip, got %#v", skip)
	}
	attempt := nextDecision(t, nextSession)
	if attempt.Skip || attempt.Candidate.GroupID != "stable" {
		t.Fatalf("expected stable attempt after sibling open, got %#v", attempt)
	}
}

func TestCircuitBreakerHalfOpenRecoveryIsGroupScoped(t *testing.T) {
	t.Parallel()

	fragile, stable := dualGroupCandidates()
	clock := &manualClock{now: time.Unix(1_700_000_000, 0).UTC()}
	store := NewMemoryStateStore()
	strategy := newCircuitBreakerForTest(t, clock, store, `{
		"failureThreshold":1,
		"baseCooldownMs":60000,
		"maxCooldownMs":60000,
		"backoffMultiplier":2,
		"halfOpenMaxRequests":1,
		"closeAfterSuccesses":1,
		"countPostCommitErrors":true,
		"rateLimitCooldownMs":60000
	}`)

	// Open fragile only.
	s1 := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	d := nextDecision(t, s1)
	reportRetryable(s1, d.Candidate, FailureUpstream5xx, 500, clock.Now())
	_ = nextDecision(t, s1)
	reportSuccess(s1, stable, clock.Now())

	// Before cooldown: fragile remains open; stable remains usable.
	s2 := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	if d := nextDecision(t, s2); !d.Skip || d.SkipReason != "circuit_open" || d.Candidate.GroupID != "fragile" {
		t.Fatalf("pre-cooldown fragile decision = %#v", d)
	}
	if d := nextDecision(t, s2); d.Skip || d.Candidate.GroupID != "stable" {
		t.Fatalf("pre-cooldown stable decision = %#v", d)
	}
	reportSuccess(s2, stable, clock.Now())

	// After cooldown: fragile becomes half-open probe; success closes only fragile.
	clock.advance(60 * time.Second)
	s3 := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	probe := nextDecision(t, s3)
	if probe.Skip || probe.Candidate.GroupID != "fragile" {
		t.Fatalf("half-open probe decision = %#v, want fragile attempt", probe)
	}
	fragileHalfOpen := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile))
	if fragileHalfOpen.Status != "half-open" || fragileHalfOpen.HalfOpenInFlight != 1 {
		t.Fatalf("fragile half-open state = %#v", fragileHalfOpen)
	}
	stableDuringProbe := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", stable))
	if stableDuringProbe.Status == "open" || stableDuringProbe.Status == "half-open" {
		t.Fatalf("stable contaminated during half-open probe: %#v", stableDuringProbe)
	}

	reportSuccess(s3, fragile, clock.Now())
	fragileClosed := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile))
	if fragileClosed.Status != "" || fragileClosed.ConsecutiveFailures != 0 || fragileClosed.HalfOpenInFlight != 0 || fragileClosed.OpenCount != 0 {
		t.Fatalf("fragile after half-open success = %#v, want closed/healthy", fragileClosed)
	}

	// Sibling stable must still have its own independent key and remain non-open.
	stableAfter := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", stable))
	if stableAfter.Status == "open" {
		t.Fatalf("stable after fragile recovery = %#v", stableAfter)
	}

	// Re-open fragile via half-open failure path and ensure stable stays clean.
	s4 := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile},
	})
	d = nextDecision(t, s4)
	reportRetryable(s4, d.Candidate, FailureRateLimited, 429, clock.Now())
	clock.advance(60 * time.Second)
	s5 := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai-responses",
		Candidates: []Candidate{fragile, stable},
	})
	probe = nextDecision(t, s5)
	if probe.Skip || probe.Candidate.GroupID != "fragile" {
		t.Fatalf("second half-open probe = %#v", probe)
	}
	reportRetryable(s5, fragile, FailureUpstream5xx, 502, clock.Now())
	if got := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", fragile)); got.Status != "open" {
		t.Fatalf("fragile after half-open fail = %#v, want open", got)
	}
	if got := store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai-responses", stable)); got.Status == "open" {
		t.Fatalf("stable polluted after fragile half-open fail = %#v", got)
	}
	if d := nextDecision(t, s5); d.Skip || d.Candidate.GroupID != "stable" {
		t.Fatalf("stable still required after fragile re-open, got %#v", d)
	}
}

func TestCircuitBreakerIsolatesSameProviderDifferentModels(t *testing.T) {
	t.Parallel()

	a := Candidate{ProviderID: "vendor", GroupID: "default", Protocol: "openai", Model: "model-a"}
	b := Candidate{ProviderID: "vendor", GroupID: "default", Protocol: "openai", Model: "model-b"}
	clock := &manualClock{now: time.Unix(1_700_000_100, 0).UTC()}
	store := NewMemoryStateStore()
	strategy := newCircuitBreakerForTest(t, clock, store, `{
		"failureThreshold":1,
		"baseCooldownMs":30000,
		"maxCooldownMs":30000,
		"backoffMultiplier":1,
		"halfOpenMaxRequests":1,
		"closeAfterSuccesses":1,
		"countPostCommitErrors":true,
		"rateLimitCooldownMs":0
	}`)

	session := strategy.NewSession(SessionInput{
		Now:        clock.Now(),
		Protocol:   "openai",
		Candidates: []Candidate{a, b},
	})
	d := nextDecision(t, session)
	reportRetryable(session, d.Candidate, FailureUpstream5xx, 500, clock.Now())
	d2 := nextDecision(t, session)
	if d2.Skip || d2.Candidate.Model != "model-b" {
		t.Fatalf("model-b should remain available, got %#v", d2)
	}

	if store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai", a)).Status != "open" {
		t.Fatalf("model-a should be open")
	}
	if store.Snapshot(StateKeyForCandidate(circuitBreakerName, "openai", b)).Status == "open" {
		t.Fatalf("model-b must not share model-a circuit state")
	}
}
