package routing

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultStrategy = "circuit-breaker"

type Config struct {
	Strategy string          `json:"strategy,omitempty"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type Descriptor struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description,omitempty"`
	Defaults    map[string]any         `json:"defaults,omitempty"`
	Parameters  []ParameterDescriptor  `json:"parameters,omitempty"`
}

type ParameterDescriptor struct {
	Key          string   `json:"key"`
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue any      `json:"defaultValue,omitempty"`
	Description  string   `json:"description,omitempty"`
	Enum         []string `json:"enum,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
}

type Candidate struct {
	Index      int
	ProviderID string
	Provider   string
	Protocol   string
	Model      string
	BaseURL    string
	Tags       map[string]string
}

type SessionInput struct {
	Now        time.Time
	RequestID  uint64
	Protocol   string
	Alias      string
	Candidates []Candidate
}

type Decision struct {
	Candidate   Candidate
	Skip        bool
	SkipReason  string
	Annotations map[string]string
}

type Outcome string

const (
	OutcomeSuccess        Outcome = "success"
	OutcomeSkipped        Outcome = "skipped"
	OutcomeRetryableFail  Outcome = "retryable_failure"
	OutcomeTerminalFail   Outcome = "terminal_failure"
	OutcomePostCommitFail Outcome = "post_commit_failure"
)

type FailureReason string

const (
	FailureNone             FailureReason = ""
	FailureUnknown          FailureReason = "unknown"
	FailureTransport        FailureReason = "transport_error"
	FailureTimeout          FailureReason = "timeout"
	FailureRateLimited      FailureReason = "rate_limited"
	FailureUpstream5xx      FailureReason = "upstream_5xx"
	FailureUpstream4xx      FailureReason = "upstream_4xx"
	FailureStreamBroken     FailureReason = "stream_broken"
	FailureEmptyResponse    FailureReason = "empty_response"
	FailureProviderMissing  FailureReason = "provider_missing"
	FailureProviderDisabled FailureReason = "provider_disabled"
	FailureStrategySkipped  FailureReason = "strategy_skipped"
)

type AttemptFeedback struct {
	Candidate       Candidate
	StartedAt       time.Time
	FinishedAt      time.Time
	Duration        time.Duration
	FirstByte       time.Duration
	Outcome         Outcome
	FailureReason   FailureReason
	StatusCode      int
	ResponseStarted bool
	Retryable       bool
	Annotations     map[string]string
}

type Strategy interface {
	Name() string
	NewSession(SessionInput) Session
}

type Session interface {
	Next() (Decision, bool)
	Report(AttemptFeedback)
}

type Factory interface {
	Name() string
	Describe() Descriptor
	ResolveParams(json.RawMessage) (map[string]any, error)
	New(json.RawMessage, Dependencies) (Strategy, error)
}

type Dependencies struct {
	Clock Clock
	Store StateStore
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now()
}

type StateKey struct {
	Strategy   string
	Protocol   string
	ProviderID string
}

type ProviderState struct {
	Status              string    `json:"status,omitempty"`
	ConsecutiveFailures int       `json:"consecutiveFailures,omitempty"`
	ConsecutiveSuccesses int      `json:"consecutiveSuccesses,omitempty"`
	OpenUntil           time.Time `json:"openUntil,omitempty"`
	CooldownMs          int       `json:"cooldownMs,omitempty"`
	HalfOpenInFlight    int       `json:"halfOpenInFlight,omitempty"`
	OpenCount           int       `json:"openCount,omitempty"`
	LastFailureReason   string    `json:"lastFailureReason,omitempty"`
	LastFailureAt       time.Time `json:"lastFailureAt,omitempty"`
	LastSuccessAt       time.Time `json:"lastSuccessAt,omitempty"`
}

type StateStore interface {
	Snapshot(StateKey) ProviderState
	Update(StateKey, func(ProviderState) ProviderState) ProviderState
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

func RegisterFactory(factory Factory) error {
	if factory == nil {
		return fmt.Errorf("nil routing factory")
	}
	name := strings.TrimSpace(factory.Name())
	if name == "" {
		return fmt.Errorf("routing factory name is required")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		return fmt.Errorf("routing factory %q already registered", name)
	}
	registry[name] = factory
	return nil
}

func MustRegisterFactory(factory Factory) {
	if err := RegisterFactory(factory); err != nil {
		panic(err)
	}
}

func FactoryByName(name string) (Factory, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	factory, ok := registry[strings.TrimSpace(name)]
	return factory, ok
}

func ListDescriptors() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]Descriptor, 0, len(names))
	for _, name := range names {
		items = append(items, registry[name].Describe())
	}
	return items
}

func NormalizeConfig(cfg Config) Config {
	strategy := strings.TrimSpace(cfg.Strategy)
	if strategy == "" {
		strategy = DefaultStrategy
	}
	cfg.Strategy = strategy
	if len(cfg.Params) == 0 {
		cfg.Params = nil
	}
	return cfg
}

func ResolveParams(cfg Config) (map[string]any, error) {
	cfg = NormalizeConfig(cfg)
	factory, ok := FactoryByName(cfg.Strategy)
	if !ok {
		return nil, fmt.Errorf("unknown routing strategy %q", cfg.Strategy)
	}
	return factory.ResolveParams(cfg.Params)
}

func ValidateConfig(cfg Config) error {
	_, err := ResolveParams(cfg)
	return err
}

func Build(cfg Config, deps Dependencies) (Strategy, error) {
	cfg = NormalizeConfig(cfg)
	factory, ok := FactoryByName(cfg.Strategy)
	if !ok {
		return nil, fmt.Errorf("unknown routing strategy %q", cfg.Strategy)
	}
	if deps.Clock == nil {
		deps.Clock = realClock{}
	}
	if deps.Store == nil {
		deps.Store = NewMemoryStateStore()
	}
	return factory.New(cfg.Params, deps)
}

func MustBuild(cfg Config, deps Dependencies) Strategy {
	strategy, err := Build(cfg, deps)
	if err == nil {
		return strategy
	}
	strategy, fallbackErr := Build(Config{Strategy: DefaultStrategy}, deps)
	if fallbackErr == nil {
		return strategy
	}
	panic(fallbackErr)
}
