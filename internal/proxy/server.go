package proxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
)

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
}

type anthropicErrorEnvelope struct {
	Type  string         `json:"type"`
	Error anthropicError `json:"error"`
}

type upstreamFailure struct {
	status int
	header http.Header
	body   []byte
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type protocolErrorWriter func(http.ResponseWriter, int, string, string)

type authContext struct {
	unrestricted bool
	reason       string
}

// Server is the local ocswitch HTTP proxy.
type Server struct {
	runtime             atomic.Pointer[serverRuntime]
	logger              *log.Logger
	traces              RequestTraceStore
	store               routing.StateStore
	baseURLLatencyCache *providerBaseURLLatencyCache
}

type serverRuntime struct {
	cfg       *config.Config
	client    *http.Client
	transport *http.Transport
	policy    routing.Strategy
}

type providerBaseURLLatencySample struct {
	latencyMs  int64
	measuredAt time.Time
	reachable  bool
}

type providerBaseURLLatencyCache struct {
	mu    sync.RWMutex
	items map[string]providerBaseURLLatencySample
	ttl   time.Duration
}

func newProviderBaseURLLatencyCache(ttl time.Duration) *providerBaseURLLatencyCache {
	return &providerBaseURLLatencyCache{items: map[string]providerBaseURLLatencySample{}, ttl: ttl}
}

func newServerRuntime(cfg *config.Config, store routing.StateStore) *serverRuntime {
	cfg.Server.FailoverStatusCodes = config.NormalizeFailoverStatusCodes(cfg.Server.FailoverStatusCodes)
	firstByteTimeout := timeoutDuration(cfg.Server.FirstByteTimeoutMs, config.DefaultFirstByteTimeoutMs)
	responseHeaderTimeout := timeoutDuration(cfg.Server.ResponseHeaderTimeoutMs, config.DefaultResponseHeaderTimeoutMs)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   timeoutDuration(cfg.Server.ConnectTimeoutMs, config.DefaultConnectTimeoutMs),
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeoutDuration(cfg.Server.ConnectTimeoutMs, config.DefaultConnectTimeoutMs),
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: minDuration(responseHeaderTimeout, firstByteTimeout),
		DisableCompression:    false,
		ForceAttemptHTTP2:     true,
	}
	return &serverRuntime{
		cfg:       cfg,
		transport: transport,
		client: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
		policy: routing.MustBuild(cfg.Server.Routing, routing.Dependencies{Store: store}),
	}
}

func providerBaseURLCacheKey(providerID, baseURL string) string {
	return providerID + "\n" + strings.TrimSpace(baseURL)
}

func (c *providerBaseURLLatencyCache) get(providerID, baseURL string) (providerBaseURLLatencySample, bool) {
	if c == nil {
		return providerBaseURLLatencySample{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sample, ok := c.items[providerBaseURLCacheKey(providerID, baseURL)]
	if !ok || time.Since(sample.measuredAt) > c.ttl {
		return providerBaseURLLatencySample{}, false
	}
	return sample, true
}

func (c *providerBaseURLLatencyCache) put(providerID, baseURL string, probe *opencode.ProviderBaseURLProbe) {
	if c == nil || probe == nil || strings.TrimSpace(baseURL) == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[providerBaseURLCacheKey(providerID, baseURL)] = providerBaseURLLatencySample{
		latencyMs:  probe.LatencyMs,
		measuredAt: time.Now(),
		reachable:  probe.Reachable,
	}
}

// New constructs a Server from cfg.
func New(cfg *config.Config, stores ...RequestTraceStore) *Server {
	var traces RequestTraceStore
	if len(stores) > 0 {
		traces = stores[0]
	}
	if traces == nil {
		traces = NewTraceStore(defaultTraceLimit)
	}
	store := routing.NewMemoryStateStore()
	s := &Server{
		logger:              log.New(log.Writer(), "[ocswitch] ", log.LstdFlags|log.Lmicroseconds),
		traces:              traces,
		store:               store,
		baseURLLatencyCache: newProviderBaseURLLatencyCache(60 * time.Second),
	}
	s.runtime.Store(newServerRuntime(cfg, store))
	return s
}

// ReloadConfig applies hot-reloadable proxy settings to new requests. Existing
// in-flight requests continue using the runtime snapshot captured at request start.
func (s *Server) ReloadConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		return errs[0]
	}
	next := newServerRuntime(cfg, s.store)
	previous := s.runtime.Swap(next)
	if previous != nil && previous.transport != nil {
		previous.transport.CloseIdleConnections()
	}
	return nil
}

func (s *Server) currentRuntime() *serverRuntime {
	state := s.runtime.Load()
	if state == nil {
		panic("proxy runtime is not initialized")
	}
	return state
}

func (s *Server) orderedProviderBaseURLs(ctx context.Context, provider *config.Provider) []string {
	if provider == nil {
		return nil
	}
	baseURLs := provider.EffectiveBaseURLs()
	if len(baseURLs) <= 1 || config.NormalizeProviderBaseURLStrategy(provider.BaseURLStrategy) != config.ProviderBaseURLStrategyLatency {
		return baseURLs
	}
	type scoredBaseURL struct {
		baseURL string
		latency int64
		ok      bool
	}
	scored := make([]scoredBaseURL, 0, len(baseURLs))
	missing := make([]string, 0, len(baseURLs))
	for _, baseURL := range baseURLs {
		if sample, ok := s.baseURLLatencyCache.get(provider.ID, baseURL); ok && sample.reachable {
			scored = append(scored, scoredBaseURL{baseURL: baseURL, latency: sample.latencyMs, ok: true})
			continue
		}
		missing = append(missing, baseURL)
	}
	for _, baseURL := range missing {
		probe, _ := opencode.ProbeProviderBaseURL(ctx, provider.Protocol, baseURL, firstProviderAPIKey(provider), provider.Headers)
		s.baseURLLatencyCache.put(provider.ID, baseURL, probe)
		if probe != nil && probe.Reachable {
			scored = append(scored, scoredBaseURL{baseURL: baseURL, latency: probe.LatencyMs, ok: true})
		}
	}
	if len(scored) == 0 {
		return baseURLs
	}
	slices.SortStableFunc(scored, func(a, b scoredBaseURL) int {
		switch {
		case a.latency < b.latency:
			return -1
		case a.latency > b.latency:
			return 1
		default:
			return 0
		}
	})
	ordered := make([]string, 0, len(baseURLs))
	seen := map[string]bool{}
	for _, item := range scored {
		ordered = append(ordered, item.baseURL)
		seen[item.baseURL] = true
	}
	for _, item := range baseURLs {
		if !seen[item] {
			ordered = append(ordered, item)
		}
	}
	return ordered
}

// ListenAndServe starts the HTTP listener until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	return s.ListenAndServeWithReady(ctx, nil)
}

// ListenAndServeWithReady starts the HTTP listener until ctx is cancelled and
// reports whether the listening socket was bound successfully.
func (s *Server) ListenAndServeWithReady(ctx context.Context, ready chan<- error) error {
	state := s.currentRuntime()
	addr := fmt.Sprintf("%s:%d", state.cfg.Server.Host, state.cfg.Server.Port)
	mux := http.NewServeMux()
	mux.HandleFunc(config.ProtocolLocalRequestPath(config.ProtocolOpenAIResponses), s.handleResponses)
	mux.HandleFunc(config.ProtocolLocalRequestPath(config.ProtocolAnthropicMessages), s.handleMessages)
	mux.HandleFunc(config.ProtocolLocalRequestPath(config.ProtocolOpenAICompatible), s.handleCompletions)
	mux.HandleFunc(config.ProtocolLocalModelsPath(config.ProtocolOpenAIResponses), s.handleModels)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	listener, err := net.Listen("tcp", addr)
	if ready != nil {
		ready <- err
	}
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       timeoutDuration(state.cfg.Server.RequestReadTimeoutMs, config.DefaultRequestReadTimeoutMs),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// handleModels exposes a minimal /v1/models listing of alias names. OpenCode
// does not rely on this, but clients sometimes probe it for connectivity.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	state := s.currentRuntime()
	auth := s.authorize(state, r)
	if auth.reason != "" {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", auth.reason)
		return
	}
	data := []map[string]any{}
	for _, aliasName := range s.availableAliasNamesForAuth(state, auth) {
		data = append(data, map[string]any{
			"id":       aliasName,
			"object":   "model",
			"owned_by": config.AppName,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

func (s *Server) authorize(state *serverRuntime, r *http.Request) authContext {
	legacy := strings.TrimSpace(state.cfg.Server.APIKey)
	raw, ok, reason := requestAPIKey(r)
	if reason != "" {
		return authContext{reason: reason}
	}
	if legacy == "" {
		return authContext{unrestricted: true}
	}
	if !ok {
		return authContext{reason: "missing api key"}
	}
	if legacy != "" && constantTimeEqual(raw, legacy) {
		return authContext{unrestricted: true}
	}
	return authContext{reason: "unknown api key"}
}

func requestAPIKey(r *http.Request) (string, bool, string) {
	bearer := ""
	if h := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(h, "Bearer ") {
		bearer = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	headerKey := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if bearer != "" && headerKey != "" && bearer != headerKey {
		return "", false, "conflicting api keys"
	}
	if bearer != "" {
		return bearer, true, ""
	}
	if headerKey != "" {
		return headerKey, true, ""
	}
	return "", false, ""
}

func constantTimeEqual(a, b string) bool {
	if a == "" || b == "" || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) availableAliasNamesForAuth(state *serverRuntime, _ authContext) []string {
	return state.cfg.AvailableAliasNames()
}

func (s *Server) availableTargetsForAuth(state *serverRuntime, _ authContext, alias config.Alias) []config.Target {
	return state.cfg.AvailableTargets(alias)
}

var reqCounter uint64

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolRequest(config.ProtocolOpenAIResponses, w, r)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolRequest(config.ProtocolAnthropicMessages, w, r)
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolRequest(config.ProtocolOpenAICompatible, w, r)
}

// handleProtocolRequest is the main alias→failover proxy entry.
func (s *Server) handleProtocolRequest(protocol string, w http.ResponseWriter, r *http.Request) {
	state := s.currentRuntime()
	protocol = config.NormalizeProviderProtocol(protocol)
	writeProtocolError := protocolErrorWriterFor(protocol)
	reqID := atomic.AddUint64(&reqCounter, 1)
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		writeProtocolError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	auth := s.authorize(state, r)
	if auth.reason != "" {
		writeProtocolError(w, http.StatusUnauthorized, "invalid_api_key", auth.reason)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 50<<20))
	if err != nil {
		status, msg := requestReadError(err)
		writeProtocolError(w, status, "invalid_request_error", msg)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeProtocolError(w, http.StatusBadRequest, "invalid_request_error", "invalid json: "+err.Error())
		return
	}
	aliasName, _ := payload["model"].(string)
	if aliasName == "" {
		writeProtocolError(w, http.StatusBadRequest, "invalid_request_error", "missing model field")
		return
	}
	rawModel := aliasName
	aliasName = normalizeAliasName(aliasName)
	trace := RequestTrace{
		ID:             reqID,
		StartedAt:      startedAt,
		Protocol:       protocol,
		RawModel:       rawModel,
		Alias:          aliasName,
		RequestHeaders: sanitizeHeaderMap(r.Header),
		RequestParams:  sanitizeJSONValue("", payload),
	}
	if stream, ok := payload["stream"].(bool); ok {
		trace.Stream = stream
	}
	defer func() {
		trace.FinishedAt = time.Now()
		trace.DurationMs = trace.FinishedAt.Sub(trace.StartedAt).Milliseconds()
		trace.AttemptCount = len(trace.Attempts)
		trace.Failover = len(trace.Attempts) > 1
		if trace.FirstByteMs == 0 {
			for _, attempt := range trace.Attempts {
				if attempt.FirstByteMs > 0 {
					trace.FirstByteMs = attempt.FirstByteMs
					break
				}
			}
		}
		if trace.FirstTokenMs == 0 {
			for _, attempt := range trace.Attempts {
				if attempt.FirstTokenMs > 0 {
					trace.FirstTokenMs = attempt.StartedAt.Sub(trace.StartedAt).Milliseconds() + attempt.FirstTokenMs
					break
				}
			}
		}
		if err := s.traces.Add(context.Background(), trace); err != nil {
			s.logger.Printf("req=%d trace persist failed: %v", reqID, err)
		}
	}()
	s.logger.Printf("req=%d incoming model=%q alias=%q stream=%v", reqID, rawModel, aliasName, payload["stream"])
	// Three-layer alias resolution:
	// 1) manual FindAlias  2) auto FindAutoAlias (if enabled)  3) direct provider fallback
	aliasSource := "manual"
	alias := state.cfg.FindAlias(aliasName)
	if alias == nil {
		if state.cfg.IsAutoAliasEnabled() {
			if auto := state.cfg.FindAutoAlias(aliasName); auto != nil {
				alias = auto
				aliasSource = "auto"
				s.logger.Printf("req=%d alias=%q resolved via auto alias fallback=true source=auto", reqID, aliasName)
			}
		}
	}
	if alias == nil {
		if providers := state.cfg.FindProvidersByModel(aliasName); len(providers) > 0 {
			// Virtual alias: Protocol=request protocol so AvailableTargets drops protocol mismatches.
			virtual := config.Alias{
				Alias:    aliasName,
				Protocol: protocol,
				Enabled:  true,
				Targets:  make([]config.Target, 0, len(providers)),
			}
			for _, p := range providers {
				virtual.Targets = append(virtual.Targets, config.Target{
					Provider: p.ID,
					Model:    aliasName,
					Enabled:  true,
				})
			}
			alias = &virtual
			aliasSource = "provider_fallback"
			s.logger.Printf("req=%d alias=%q resolved via direct provider fallback=true source=provider_fallback providers=%d", reqID, aliasName, len(providers))
		}
	}
	if alias == nil {
		s.logger.Printf("req=%d alias lookup failed for model=%q alias=%q", reqID, rawModel, aliasName)
		msg := fmt.Sprintf("alias %q not found", aliasName)
		finishLocalRequestTrace(&trace, http.StatusNotFound, "alias_missing", msg)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", msg)
		return
	}
	if !config.ProtocolsMatch(alias.Protocol, protocol) {
		// Manual/auto alias found but protocol mismatch: do not fall through to other layers.
		msg := fmt.Sprintf("alias %q does not support protocol %q", aliasName, protocol)
		finishLocalRequestTrace(&trace, http.StatusNotFound, "protocol_mismatch", msg)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", msg)
		return
	}
	if !alias.Enabled {
		// Manual/auto alias found but disabled: do not fall through to other layers.
		s.logger.Printf("req=%d alias=%q disabled source=%s", reqID, aliasName, aliasSource)
		msg := fmt.Sprintf("alias %q is disabled", aliasName)
		finishLocalRequestTrace(&trace, http.StatusNotFound, "alias_disabled", msg)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", msg)
		return
	}
	targets := s.availableTargetsForAuth(state, auth, *alias)
	if len(targets) == 0 {
		s.logger.Printf("req=%d alias=%q has no available targets source=%s fallback=%v", reqID, aliasName, aliasSource, aliasSource != "manual")
		msg := fmt.Sprintf("alias %q has no available targets", aliasName)
		finishLocalRequestTrace(&trace, http.StatusNotFound, "no_available_target", msg)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", msg)
		return
	}
	if aliasSource != "manual" {
		s.logger.Printf("req=%d alias=%q routing with fallback=true source=%s targets=%d", reqID, aliasName, aliasSource, len(targets))
	}

	failoverCount := 0
	var lastRetryable *upstreamFailure
	candidates := make([]routing.Candidate, 0, len(targets))
	for index, t := range targets {
		provider := state.cfg.FindProvider(t.Provider)
		baseURL := ""
		if provider != nil {
			baseURL = provider.BaseURL
		}
		candidates = append(candidates, routing.Candidate{Index: index, ProviderID: t.Provider, Provider: t.Provider, Protocol: protocol, Model: t.Model, BaseURL: baseURL})
	}
	attempt := 0
	resetCircuitTried := false
	for {
		session := state.policy.NewSession(routing.SessionInput{Now: startedAt, RequestID: reqID, Protocol: protocol, Alias: aliasName, Candidates: candidates})
		attemptedTarget := false
		circuitOpenSkips := 0
		otherSkips := 0
		circuitSkippedProviders := map[string]bool{}
		for {
			decision, ok := session.Next()
			if !ok {
				break
			}
			attempt++
			t := config.Target{Provider: decision.Candidate.ProviderID, Model: decision.Candidate.Model, Enabled: true}
			attemptTrace := TraceAttempt{
				Attempt:   attempt,
				Provider:  t.Provider,
				Model:     t.Model,
				StartedAt: time.Now(),
				Result:    "pending",
			}
			if decision.Skip {
				if decision.SkipReason == "circuit_open" {
					circuitOpenSkips++
					circuitSkippedProviders[decision.Candidate.ProviderID] = true
				} else {
					otherSkips++
				}
				attemptTrace.Skipped = true
				attemptTrace.Result = "skipped"
				attemptTrace.Error = decision.SkipReason
				attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
				trace.Attempts = append(trace.Attempts, attemptTrace)
				session.Report(routing.AttemptFeedback{Candidate: decision.Candidate, StartedAt: attemptTrace.StartedAt, FinishedAt: time.Now(), Duration: time.Since(attemptTrace.StartedAt), Retryable: true, Outcome: routing.OutcomeSkipped, FailureReason: routing.FailureStrategySkipped})
				failoverCount++
				continue
			}
			p := state.cfg.FindProvider(t.Provider)
			if p == nil || !p.IsEnabled() || !config.ProtocolsMatch(protocol, p.Protocol) {
				otherSkips++
				s.logger.Printf("req=%d alias=%s attempt=%d target provider %q unavailable, skipping", reqID, aliasName, attempt, t.Provider)
				attemptTrace.Skipped = true
				attemptTrace.Result = "skipped"
				attemptTrace.Error = fmt.Sprintf("provider %q unavailable", t.Provider)
				attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
				trace.Attempts = append(trace.Attempts, attemptTrace)
				reason := routing.FailureProviderMissing
				if p != nil && !p.IsEnabled() {
					reason = routing.FailureProviderDisabled
				}
				session.Report(routing.AttemptFeedback{Candidate: decision.Candidate, StartedAt: attemptTrace.StartedAt, FinishedAt: time.Now(), Duration: time.Since(attemptTrace.StartedAt), Retryable: true, Outcome: routing.OutcomeSkipped, FailureReason: reason})
				failoverCount++
				continue
			}
			attemptedTarget = true
			s.logger.Printf("req=%d alias=%s attempt=%d provider=%s remote_model=%s failovers=%d", reqID, aliasName, attempt, p.ID, t.Model, failoverCount)
			cloned := cloneMap(payload)
			cloned["model"] = t.Model
			state.cfg.ApplyRequestRewriteRules(aliasName, t.Provider, t.Model, cloned)
			attemptTrace.RequestParams = sanitizeJSONValue("", cloned)
			newBody, err := json.Marshal(cloned)
			if err != nil {
				s.logger.Printf("req=%d marshal error: %v", reqID, err)
				attemptTrace.Result = "internal_error"
				attemptTrace.Error = "marshal error"
				attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
				trace.Attempts = append(trace.Attempts, attemptTrace)
				session.Report(routing.AttemptFeedback{Candidate: decision.Candidate, StartedAt: attemptTrace.StartedAt, FinishedAt: time.Now(), Duration: time.Since(attemptTrace.StartedAt), Retryable: false, Outcome: routing.OutcomeTerminalFail, FailureReason: routing.FailureUnknown})
				trace.Error = "marshal error"
				writeProtocolError(w, http.StatusInternalServerError, "server_error", "marshal error")
				return
			}

			handled, success, retryable, upstreamErr, failure := s.tryProviderBaseURLs(state, r.Context(), protocol, w, r, p, t, newBody, aliasName, attempt, failoverCount, &attemptTrace, &trace)
			attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
			attemptTrace.Attempt = len(trace.Attempts) + 1
			trace.Attempts = append(trace.Attempts, attemptTrace)
			trace.FinalProvider = p.ID
			trace.FinalModel = t.Model
			trace.FinalURL = attemptTrace.URL
			trace.StatusCode = attemptTrace.StatusCode
			if trace.FirstByteMs == 0 {
				trace.FirstByteMs = attemptTrace.FirstByteMs
			}
			if trace.FirstTokenMs == 0 && attemptTrace.FirstTokenMs > 0 {
				trace.FirstTokenMs = attemptTrace.StartedAt.Sub(trace.StartedAt).Milliseconds() + attemptTrace.FirstTokenMs
			}
			feedback := routing.AttemptFeedback{
				Candidate:       decision.Candidate,
				StartedAt:       attemptTrace.StartedAt,
				FinishedAt:      time.Now(),
				Duration:        time.Since(attemptTrace.StartedAt),
				FirstByte:       time.Duration(attemptTrace.FirstByteMs) * time.Millisecond,
				Retryable:       retryable,
				ResponseStarted: handled && attemptTrace.FirstByteMs > 0,
				StatusCode:      attemptTrace.StatusCode,
				FailureReason:   classifyFailureReason(attemptTrace, retryable),
			}
			clientCanceled := errors.Is(upstreamErr, errClientCanceled) || traceAttemptIsClientCanceled(attemptTrace)
			if clientCanceled {
				feedback.Outcome = routing.OutcomeClientCanceled
				feedback.FailureReason = routing.FailureClientCanceled
			} else if success {
				feedback.Outcome = routing.OutcomeSuccess
			} else if retryable {
				feedback.Outcome = routing.OutcomeRetryableFail
			} else if handled && attemptTrace.FirstByteMs > 0 {
				feedback.Outcome = routing.OutcomePostCommitFail
			} else {
				feedback.Outcome = routing.OutcomeTerminalFail
			}
			session.Report(feedback)
			if clientCanceled {
				trace.Error = errorString(upstreamErr)
				return
			}
			if handled {
				trace.Success = success
				if !success {
					trace.Error = errorString(upstreamErr)
				}
				return
			}
			if !retryable {
				s.logger.Printf("req=%d alias=%s attempt=%d final failure: %v", reqID, aliasName, attempt, upstreamErr)
				trace.Error = errorString(upstreamErr)
				return
			}
			if failure != nil {
				lastRetryable = failure
			}
			s.logger.Printf("req=%d alias=%s attempt=%d retryable: %v", reqID, aliasName, attempt, upstreamErr)
			failoverCount++
		}
		if !attemptedTarget && !resetCircuitTried && len(candidates) > 0 && circuitOpenSkips == len(candidates) && otherSkips == 0 {
			resetCircuitTried = true
			s.logger.Printf("req=%d alias=%s all targets circuit-open; clearing circuit state and retrying once", reqID, aliasName)
			s.resetCircuitBreakerStates(state, protocol, circuitSkippedProviders)
			continue
		}
		break
	}

	if lastRetryable != nil {
		trace.StatusCode = lastRetryable.status
		trace.Error = fmt.Sprintf("upstream %d", lastRetryable.status)
		copyResponseHeaders(w.Header(), lastRetryable.header)
		w.WriteHeader(lastRetryable.status)
		if len(lastRetryable.body) > 0 {
			_, _ = w.Write(lastRetryable.body)
		}
		return
	}

	trace.StatusCode = http.StatusBadGateway
	trace.Error = fmt.Sprintf("all upstream targets failed for alias %q", aliasName)
	writeProtocolError(w, http.StatusBadGateway, "server_error", fmt.Sprintf("all upstream targets failed for alias %q", aliasName))
}

func (s *Server) resetCircuitBreakerStates(state *serverRuntime, protocol string, providerIDs map[string]bool) {
	if s == nil || state == nil || s.store == nil {
		return
	}
	strategy := state.policy.Name()
	for providerID := range providerIDs {
		if providerID == "" {
			continue
		}
		s.store.Update(routing.StateKey{Strategy: strategy, Protocol: protocol, ProviderID: providerID}, func(routing.ProviderState) routing.ProviderState {
			return routing.ProviderState{}
		})
	}
}

func (s *Server) tryProviderBaseURLs(
	state *serverRuntime,
	ctx context.Context,
	protocol string,
	w http.ResponseWriter,
	clientReq *http.Request,
	provider *config.Provider,
	target config.Target,
	body []byte,
	aliasName string,
	attempt int,
	failoverCount int,
	attemptTrace *TraceAttempt,
	trace *RequestTrace,
) (handled bool, success bool, retryable bool, err error, failure *upstreamFailure) {
	if clientRequestCanceled(clientReq) {
		cancelErr := clientRequestCancelError(clientReq)
		markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
		return false, false, false, cancelErr, nil
	}
	baseURLs := s.orderedProviderBaseURLs(ctx, provider)
	if len(baseURLs) == 0 {
		return false, false, false, fmt.Errorf("provider %q has no base URLs", provider.ID), nil
	}
	apiKeys := providerAPIKeyOptions(provider, traceID(trace))
	var lastRetryable *upstreamFailure
	var lastErr error
	for baseURLIndex, baseURL := range baseURLs {
		for apiKeyPosition, apiKey := range apiKeys {
			if clientRequestCanceled(clientReq) {
				cancelErr := clientRequestCancelError(clientReq)
				if baseURLIndex > 0 || apiKeyPosition > 0 {
					resetRetryTraceAttempt(attemptTrace)
				}
				markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
				return false, false, false, cancelErr, nil
			}
			if baseURLIndex > 0 || apiKeyPosition > 0 {
				resetRetryTraceAttempt(attemptTrace)
			}
			currentAttempt := attempt
			if trace != nil {
				currentAttempt = len(trace.Attempts) + 1
			}
			providerCopy := *provider
			providerCopy.BaseURL = baseURL
			providerCopy.APIKey = apiKey.Value
			providerCopy.APIKeys = nil
			if attemptTrace != nil {
				attemptTrace.URL = strings.TrimRight(baseURL, "/") + config.ProtocolUpstreamRequestPath(protocol)
				attemptTrace.APIKeyIndex = 0
				attemptTrace.APIKeyMasked = ""
				if apiKey.Value != "" {
					attemptTrace.APIKeyIndex = apiKey.Index
					attemptTrace.APIKeyMasked = maskSensitiveValue(apiKey.Value)
				}
			}
			handled, success, retryable, err, failure = s.tryOnce(state, ctx, protocol, w, clientReq, &providerCopy, target, body, aliasName, currentAttempt, failoverCount, attemptTrace, trace)
			if errors.Is(err, errClientCanceled) {
				return handled, success, false, err, nil
			}
			if handled || success || !retryable {
				if success {
					s.baseURLLatencyCache.put(provider.ID, baseURL, &opencode.ProviderBaseURLProbe{BaseURL: baseURL, Reachable: true, LatencyMs: attemptTrace.FirstByteMs, StatusCode: attemptTrace.StatusCode})
				}
				return handled, success, retryable, err, failure
			}
			lastErr = err
			if failure != nil {
				lastRetryable = failure
			}
			if baseURLIndex == len(baseURLs)-1 && apiKeyPosition == len(apiKeys)-1 {
				break
			}
			if attemptTrace != nil && trace != nil {
				failedAttempt := *attemptTrace
				failedAttempt.Attempt = len(trace.Attempts) + 1
				failedAttempt.DurationMs = time.Since(failedAttempt.StartedAt).Milliseconds()
				trace.Attempts = append(trace.Attempts, failedAttempt)
			}
		}
	}
	return false, false, true, lastErr, lastRetryable
}

func resetRetryTraceAttempt(attempt *TraceAttempt) {
	if attempt == nil {
		return
	}
	attempt.StartedAt = time.Now()
	attempt.DurationMs = 0
	attempt.FirstByteMs = 0
	attempt.FirstTokenMs = 0
	attempt.StatusCode = 0
	attempt.Success = false
	attempt.Retryable = false
	attempt.Skipped = false
	attempt.Result = "pending"
	attempt.Error = ""
	attempt.RequestHeaders = nil
	attempt.ResponseHeaders = nil
	attempt.ResponseBody = ""
	attempt.URL = ""
	attempt.APIKeyIndex = 0
	attempt.APIKeyMasked = ""
}

type providerAPIKeyOption struct {
	Index int
	Value string
}

func providerAPIKeyOptions(provider *config.Provider, requestID uint64) []providerAPIKeyOption {
	if provider == nil {
		return []providerAPIKeyOption{{}}
	}
	keys := provider.EffectiveAPIKeys()
	if len(keys) == 0 {
		return []providerAPIKeyOption{{}}
	}
	items := make([]providerAPIKeyOption, 0, len(keys))
	for index, key := range keys {
		items = append(items, providerAPIKeyOption{Index: index + 1, Value: key})
	}
	if len(items) <= 1 || requestID == 0 {
		return items
	}
	offset := int((requestID - 1) % uint64(len(items)))
	return append(items[offset:], items[:offset]...)
}

func firstProviderAPIKey(provider *config.Provider) string {
	options := providerAPIKeyOptions(provider, 0)
	if len(options) == 0 {
		return ""
	}
	return options[0].Value
}

func traceID(trace *RequestTrace) uint64 {
	if trace == nil {
		return 0
	}
	return trace.ID
}

// tryOnce proxies one attempt. Returns (handled, success, retryable, err, failure).
// handled=true means a downstream response has already been started or completed.
// retryable=true means failure happened before any bytes flushed downstream.
func (s *Server) tryOnce(
	state *serverRuntime,
	ctx context.Context,
	protocol string,
	w http.ResponseWriter,
	clientReq *http.Request,
	provider *config.Provider,
	target config.Target,
	body []byte,
	aliasName string,
	attempt int,
	failoverCount int,
	attemptTrace *TraceAttempt,
	trace *RequestTrace,
) (handled bool, success bool, retryable bool, err error, failure *upstreamFailure) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	upstreamURL := strings.TrimRight(provider.BaseURL, "/") + config.ProtocolUpstreamRequestPath(protocol)
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return false, false, false, fmt.Errorf("build request: %w", err), nil
	}
	copyForwardHeaders(upReq.Header, clientReq.Header)
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", clientReq.Header.Get("Accept"))
	config.ApplyProtocolAuthHeaders(upReq.Header, protocol, provider.APIKey)
	config.ApplyProtocolDefaultHeaders(upReq.Header, protocol)
	for k, v := range provider.Headers {
		upReq.Header.Set(k, v)
	}
	upReq.ContentLength = int64(len(body))
	if attemptTrace != nil {
		attemptTrace.RequestHeaders = sanitizeHeaderMap(upReq.Header)
	}

	startedAt := time.Now()
	firstByteTimeout := timeoutDuration(state.cfg.Server.FirstByteTimeoutMs, config.DefaultFirstByteTimeoutMs)
	resp, err := state.client.Do(upReq)
	if err != nil {
		if isUpstreamCanceledByClient(clientReq, err) {
			cancelErr := clientCanceledError(err)
			markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
			return false, false, false, cancelErr, nil
		}
		if attemptTrace != nil {
			attemptTrace.Retryable = true
			attemptTrace.Result = "transport_error"
			attemptTrace.Error = fmt.Sprintf("upstream dial/transport: %v", err)
		}
		return false, false, true, fmt.Errorf("upstream dial/transport: %w", err), nil
	}
	defer resp.Body.Close()
	if attemptTrace != nil {
		attemptTrace.StatusCode = resp.StatusCode
		attemptTrace.ResponseHeaders = sanitizeHeaderMap(resp.Header)
	}

	if isRetryableStatusCode(resp.StatusCode, state.cfg.Server.FailoverStatusCodes) {
		failure = captureRetryableFailure(resp)
		sanitizedBody := sanitizeResponseBody(resp.Header.Get("Content-Type"), failure.body)
		if attemptTrace != nil {
			attemptTrace.Retryable = true
			attemptTrace.Result = "retryable_failure"
			attemptTrace.Error = fmt.Sprintf("upstream %d: %s", resp.StatusCode, sanitizedBody)
			attemptTrace.ResponseBody = sanitizedBody
		}
		return false, false, true, fmt.Errorf("upstream %d: %s", resp.StatusCode, sanitizedBody), failure
	}
	if resp.StatusCode >= 400 {
		s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
		if attemptTrace != nil {
			attemptTrace.Result = "final_failure"
		}
		s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		if attemptTrace != nil {
			attemptTrace.ResponseBody = sanitizeResponseBody(resp.Header.Get("Content-Type"), bodyBytes)
		}
		_, _ = w.Write(bodyBytes)
		return true, false, false, fmt.Errorf("upstream %d", resp.StatusCode), nil
	}

	remaining := firstByteTimeout - time.Since(startedAt)
	if clientRequestCanceled(clientReq) {
		cancelErr := clientRequestCancelError(clientReq)
		markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
		return false, false, false, cancelErr, nil
	}
	if remaining <= 0 {
		return false, false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout), nil
	}
	firstChunk, firstErr := readFirstChunkWithContext(requestContext(clientReq), resp.Body, remaining)
	if firstErr != nil {
		if errors.Is(firstErr, errClientCanceled) || isUpstreamCanceledByClient(clientReq, firstErr) {
			cancelErr := clientCanceledError(firstErr)
			markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
			return false, false, false, cancelErr, nil
		}
		if errors.Is(firstErr, errFirstByteTimeout) {
			if attemptTrace != nil {
				attemptTrace.Retryable = true
				attemptTrace.Result = "first_byte_timeout"
				attemptTrace.Error = fmt.Sprintf("upstream first byte timeout after %s", firstByteTimeout)
			}
			return false, false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout), nil
		}
		if errors.Is(firstErr, io.EOF) {
			if len(firstChunk) == 0 {
				if attemptTrace != nil {
					attemptTrace.Retryable = true
					attemptTrace.Result = "empty_response"
					attemptTrace.Error = "upstream closed before first byte"
				}
				return false, false, true, fmt.Errorf("upstream closed before first byte"), nil
			}
		} else {
			if attemptTrace != nil {
				attemptTrace.Retryable = true
				attemptTrace.Result = "first_read_error"
				attemptTrace.Error = fmt.Sprintf("upstream first read: %v", firstErr)
			}
			return false, false, true, fmt.Errorf("upstream first read: %w", firstErr), nil
		}
	}
	if attemptTrace != nil {
		attemptTrace.FirstByteMs = time.Since(startedAt).Milliseconds()
	}
	if trace != nil && trace.FirstByteMs == 0 && attemptTrace != nil {
		trace.FirstByteMs = attemptTrace.FirstByteMs
	}
	recordFirstToken := func() {
		if attemptTrace == nil || attemptTrace.FirstTokenMs > 0 {
			return
		}
		attemptStartedAt := attemptTrace.StartedAt
		if attemptStartedAt.IsZero() {
			attemptStartedAt = startedAt
		}
		attemptTrace.FirstTokenMs = positiveDurationMs(time.Since(attemptStartedAt))
		if trace != nil && trace.FirstTokenMs == 0 {
			trace.FirstTokenMs = positiveDurationMs(time.Since(trace.StartedAt))
		}
	}

	isEventStream := false
	streamIdleTimeout := timeoutDuration(state.cfg.Server.StreamIdleTimeoutMs, config.DefaultStreamIdleTimeoutMs)
	streamPrecommitBuffer := nonNegativeDuration(state.cfg.Server.StreamPrecommitBufferMs)
	if mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); parseErr == nil {
		isEventStream = mediaType == "text/event-stream"
	}
	usageCollector := newUsageCollector(protocol, resp.Header.Get("Content-Type"))
	sseState := newSSEStreamState(protocol)
	firstChunkClassified := false

	if isEventStream && streamPrecommitBuffer > 0 {
		precommit, precommitErr := s.runSSEPrecommitBuffer(ssePrecommitInput{
			ctx:              requestContext(clientReq),
			body:             resp.Body,
			firstChunk:       firstChunk,
			protocol:         protocol,
			idleTimeout:      streamIdleTimeout,
			precommitWindow:  streamPrecommitBuffer,
			usageCollector:   usageCollector,
			classifier:       sseState,
			precommitStarted: time.Now(),
		})
		if precommit.firstTokenWorth {
			recordFirstToken()
		}
		if precommitErr != nil {
			if errors.Is(precommitErr, errClientCanceled) {
				markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, precommitErr)
				return false, false, false, precommitErr, nil
			}
			if trace != nil {
				applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "upstream stream ended before downstream commit"))
			}
			if attemptTrace != nil {
				attemptTrace.Retryable = true
				attemptTrace.Result = precommit.result
				attemptTrace.Error = precommitErr.Error()
			}
			return false, false, true, precommitErr, nil
		}
		firstChunk = precommit.buffered.Bytes()
		firstChunkClassified = true
		if precommit.terminal {
			s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
			s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
			copyResponseHeaders(w.Header(), resp.Header)
			w.WriteHeader(resp.StatusCode)
			flusher, _ := w.(http.Flusher)
			if len(firstChunk) > 0 {
				if _, werr := w.Write(firstChunk); werr != nil {
					return true, false, false, markDownstreamWriteError(clientReq, trace, usageCollector, attemptTrace, werr), nil
				}
			}
			if flusher != nil {
				flusher.Flush()
			}
			if trace != nil {
				applyUsageToTrace(trace, usageCollector.Usage())
			}
			if attemptTrace != nil {
				attemptTrace.Result = "success"
				attemptTrace.Success = true
			}
			return true, true, false, nil, nil
		}
	}

	s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
	s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if len(firstChunk) > 0 {
		if !firstChunkClassified {
			usageCollector.Add(firstChunk)
		}
		if isEventStream && !firstChunkClassified {
			signal := sseState.Add(firstChunk)
			if signal.firstTokenWorth {
				recordFirstToken()
			}
			if signal.terminal {
				if _, werr := w.Write(firstChunk); werr != nil {
					return true, false, false, markDownstreamWriteError(clientReq, trace, usageCollector, attemptTrace, werr), nil
				}
				if flusher != nil {
					flusher.Flush()
				}
				if trace != nil {
					applyUsageToTrace(trace, usageCollector.Usage())
				}
				if attemptTrace != nil {
					attemptTrace.Result = "success"
					attemptTrace.Success = true
				}
				return true, true, false, nil, nil
			}
		}
		if _, werr := w.Write(firstChunk); werr != nil {
			return true, false, false, markDownstreamWriteError(clientReq, trace, usageCollector, attemptTrace, werr), nil
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	buf := make([]byte, 16<<10)
	for {
		var (
			n    int
			rerr error
		)
		n, rerr = readChunkWithContext(requestContext(clientReq), resp.Body, buf, streamIdleTimeout)
		if n > 0 {
			usageCollector.Add(buf[:n])
			terminal := false
			if isEventStream {
				signal := sseState.Add(buf[:n])
				if signal.firstTokenWorth {
					recordFirstToken()
				}
				terminal = signal.terminal
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, false, false, markDownstreamWriteError(clientReq, trace, usageCollector, attemptTrace, werr), nil
			}
			if flusher != nil {
				flusher.Flush()
			}
			if terminal {
				if trace != nil {
					applyUsageToTrace(trace, usageCollector.Usage())
				}
				if attemptTrace != nil {
					attemptTrace.Result = "success"
					attemptTrace.Success = true
				}
				return true, true, false, nil, nil
			}
		}
		if rerr != nil {
			if errors.Is(rerr, errClientCanceled) || isUpstreamCanceledByClient(clientReq, rerr) {
				cancelErr := clientCanceledError(rerr)
				if trace != nil {
					applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "client canceled before stream completed"))
				}
				markAttemptClientCanceled(attemptTrace, TraceResultClientCanceled, cancelErr)
				return true, false, false, cancelErr, nil
			}
			if errors.Is(rerr, io.EOF) {
				if trace != nil {
					applyUsageToTrace(trace, usageCollector.Usage())
				}
				if attemptTrace != nil {
					attemptTrace.Result = "success"
					attemptTrace.Success = true
				}
				return true, true, false, nil, nil
			}
			if isEventStream && errors.Is(rerr, errStreamIdleTimeout) {
				message := fmt.Sprintf("upstream stream idle timeout after %s", streamIdleTimeout)
				if _, werr := w.Write(sseStreamErrorEvent(protocol, "upstream_stream_idle_timeout", message)); werr != nil {
					return true, false, false, markDownstreamWriteError(clientReq, trace, usageCollector, attemptTrace, werr), nil
				}
				if flusher != nil {
					flusher.Flush()
				}
				if trace != nil {
					applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "upstream stream idle timeout before usage finalized"))
				}
				if attemptTrace != nil {
					attemptTrace.Result = "stream_idle_timeout"
					attemptTrace.Error = message
				}
				return true, false, false, fmt.Errorf("%s", message), nil
			}
			s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream body read failed after response start: %v", aliasName, attempt, provider.ID, target.Model, rerr)
			if trace != nil {
				applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "upstream stream terminated before usage finalized"))
			}
			if attemptTrace != nil {
				attemptTrace.Result = "stream_error"
				attemptTrace.Error = rerr.Error()
			}
			return true, false, false, rerr, nil
		}
	}
}

func isRetryableStatusCode(statusCode int, failoverStatusCodes []int) bool {
	if statusCode >= 500 {
		return true
	}
	return slices.Contains(failoverStatusCodes, statusCode)
}

type ssePrecommitInput struct {
	ctx              context.Context
	body             io.Reader
	firstChunk       []byte
	protocol         string
	idleTimeout      time.Duration
	precommitWindow  time.Duration
	usageCollector   usageCollector
	classifier       *sseStreamState
	precommitStarted time.Time
}

type ssePrecommitResult struct {
	buffered        bytes.Buffer
	terminal        bool
	firstTokenWorth bool
	result          string
}

func (s *Server) runSSEPrecommitBuffer(in ssePrecommitInput) (ssePrecommitResult, error) {
	var result ssePrecommitResult
	ctx := in.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if in.precommitStarted.IsZero() {
		in.precommitStarted = time.Now()
	}
	if in.classifier == nil {
		in.classifier = newSSEStreamState(in.protocol)
	}
	if len(in.firstChunk) > 0 {
		_, _ = result.buffered.Write(in.firstChunk)
		if in.usageCollector != nil {
			in.usageCollector.Add(in.firstChunk)
		}
		signal := in.classifier.Add(in.firstChunk)
		result.firstTokenWorth = result.firstTokenWorth || signal.firstTokenWorth
		if signal.terminal {
			result.terminal = true
			return result, nil
		}
		if signal.commitWorth || result.buffered.Len() >= ssePrecommitBufferCapBytes {
			return result, nil
		}
	}
	buf := make([]byte, 16<<10)
	for {
		remaining := in.precommitWindow - time.Since(in.precommitStarted)
		if remaining <= 0 {
			result.result = "precommit_no_content_timeout"
			return result, fmt.Errorf("upstream stream precommit buffer expired after %s", in.precommitWindow)
		}
		deadline := minDuration(in.idleTimeout, remaining)
		n, rerr := readChunkWithContext(ctx, in.body, buf, deadline)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			_, _ = result.buffered.Write(chunk)
			if in.usageCollector != nil {
				in.usageCollector.Add(chunk)
			}
			signal := in.classifier.Add(chunk)
			result.firstTokenWorth = result.firstTokenWorth || signal.firstTokenWorth
			if signal.terminal {
				result.terminal = true
				return result, nil
			}
			if signal.commitWorth || result.buffered.Len() >= ssePrecommitBufferCapBytes {
				return result, nil
			}
		}
		if rerr != nil {
			if errors.Is(rerr, errClientCanceled) {
				result.result = TraceResultClientCanceled
				return result, rerr
			}
			if errors.Is(rerr, io.EOF) {
				result.result = "precommit_incomplete_stream"
				return result, fmt.Errorf("upstream stream ended before commit-worthy SSE content")
			}
			if errors.Is(rerr, errStreamIdleTimeout) {
				if remaining <= in.idleTimeout {
					result.result = "precommit_no_content_timeout"
					return result, fmt.Errorf("upstream stream precommit buffer expired after %s", in.precommitWindow)
				}
				result.result = "precommit_stream_idle_timeout"
				return result, fmt.Errorf("upstream stream idle timeout before downstream commit after %s", in.idleTimeout)
			}
			result.result = "precommit_stream_error"
			return result, fmt.Errorf("upstream stream read before downstream commit: %w", rerr)
		}
	}
}

func jsonNumberToInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0, false
		}
		return int64(typed), true
	case int:
		if typed < 0 {
			return 0, false
		}
		return int64(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return typed, true
	default:
		return 0, false
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func applyUsageToTrace(trace *RequestTrace, usage tokenUsage) {
	if trace == nil {
		return
	}
	trace.Usage = TraceUsage{
		RawInputTokens:     cloneInt64Ptr(usage.rawInputTokens),
		RawOutputTokens:    cloneInt64Ptr(usage.rawOutputTokens),
		RawTotalTokens:     cloneInt64Ptr(usage.rawTotalTokens),
		InputTokens:        cloneInt64Ptr(usage.inputTokens),
		OutputTokens:       cloneInt64Ptr(usage.outputTokens),
		ReasoningTokens:    cloneInt64Ptr(usage.reasoningTokens),
		CacheReadTokens:    cloneInt64Ptr(usage.cacheReadTokens),
		CacheWriteTokens:   cloneInt64Ptr(usage.cacheWriteTokens),
		CacheWrite1HTokens: cloneInt64Ptr(usage.cacheWrite1HTokens),
		Source:             usage.source,
		Precision:          usage.precision,
		Notes:              append([]string(nil), usage.notes...),
	}
	trace.InputTokens = usage.projectInputTokens()
	trace.OutputTokens = usage.projectOutputTokens()
	trace.GeneratedOutputTokens = usage.projectGeneratedOutputTokens()
}

func usageForStreamFailure(collector usageCollector, note string) tokenUsage {
	usage := collector.Usage().withNote(note)
	if usage.source == "" {
		usage.precision = "unavailable"
		return usage
	}
	if usage.hasData() {
		usage.precision = "partial"
		return usage
	}
	usage.precision = "unavailable"
	return usage
}

func markDownstreamWriteError(clientReq *http.Request, trace *RequestTrace, collector usageCollector, attemptTrace *TraceAttempt, err error) error {
	if trace != nil {
		applyUsageToTrace(trace, usageForStreamFailure(collector, "downstream write failed before usage finalized"))
	}
	if isDownstreamClientDisconnect(clientReq, err) {
		cancelErr := clientCanceledError(err)
		markAttemptClientCanceled(attemptTrace, TraceResultDownstreamCanceled, cancelErr)
		return cancelErr
	}
	if attemptTrace != nil {
		attemptTrace.Result = "downstream_write_error"
		attemptTrace.Error = err.Error()
	}
	return err
}

func requestContext(req *http.Request) context.Context {
	if req == nil {
		return context.Background()
	}
	return req.Context()
}

func clientRequestCanceled(req *http.Request) bool {
	return req != nil && req.Context().Err() != nil
}

func clientRequestCancelError(req *http.Request) error {
	if req != nil && req.Context().Err() != nil {
		return clientCanceledError(req.Context().Err())
	}
	return errClientCanceled
}

func clientCanceledError(err error) error {
	if err == nil {
		return errClientCanceled
	}
	if errors.Is(err, errClientCanceled) {
		return err
	}
	return fmt.Errorf("%w: %v", errClientCanceled, err)
}

func isUpstreamCanceledByClient(req *http.Request, err error) bool {
	if errors.Is(err, errClientCanceled) {
		return true
	}
	if !clientRequestCanceled(req) {
		return false
	}
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(message, "context canceled") || strings.Contains(message, "request canceled")
}

func isDownstreamClientDisconnect(req *http.Request, err error) bool {
	if errors.Is(err, errClientCanceled) {
		return true
	}
	if clientRequestCanceled(req) {
		return true
	}
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, token := range []string{"broken pipe", "connection reset by peer", "client disconnected", "use of closed network connection", "connection aborted", "forcibly closed", "wsasend", "http2: stream closed", "stream closed"} {
		if strings.Contains(message, token) {
			return true
		}
	}
	return false
}

func markAttemptClientCanceled(attemptTrace *TraceAttempt, result string, err error) {
	if attemptTrace == nil {
		return
	}
	attemptTrace.Success = false
	attemptTrace.Retryable = false
	attemptTrace.Result = result
	attemptTrace.Error = errorString(err)
	if attemptTrace.Error == "" {
		attemptTrace.Error = errClientCanceled.Error()
	}
}

func traceAttemptIsClientCanceled(attempt TraceAttempt) bool {
	result := strings.ToLower(strings.TrimSpace(attempt.Result))
	return result == TraceResultClientCanceled || result == TraceResultDownstreamCanceled
}

var errClientCanceled = errors.New("client canceled")
var errFirstByteTimeout = errors.New("first byte timeout")
var errStreamIdleTimeout = errors.New("stream idle timeout")

func readFirstChunk(r io.Reader, timeout time.Duration) ([]byte, error) {
	return readFirstChunkWithContext(context.Background(), r, timeout)
}

func readFirstChunkWithContext(ctx context.Context, r io.Reader, timeout time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		buf []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 16<<10)
		n, err := r.Read(buf)
		if n > 0 {
			buf = buf[:n]
		} else {
			buf = nil
		}
		ch <- result{buf: buf, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.buf, res.err
	case <-ctx.Done():
		return nil, clientCanceledError(ctx.Err())
	case <-timer.C:
		return nil, errFirstByteTimeout
	}
}

func readChunkWithTimeout(r io.Reader, buf []byte, timeout time.Duration) (int, error) {
	return readChunkWithContext(context.Background(), r, buf, timeout)
}

func readChunkWithContext(ctx context.Context, r io.Reader, buf []byte, timeout time.Duration) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := r.Read(buf)
		ch <- result{n: n, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-ctx.Done():
		return 0, clientCanceledError(ctx.Err())
	case <-timer.C:
		return 0, errStreamIdleTimeout
	}
}

func captureRetryableFailure(resp *http.Response) *upstreamFailure {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	return &upstreamFailure{
		status: resp.StatusCode,
		header: cloneHeaderSubset(resp.Header, "Content-Type", "Retry-After"),
		body:   body,
	}
}

func cloneHeaderSubset(src http.Header, names ...string) http.Header {
	dst := make(http.Header)
	for _, name := range names {
		ck := http.CanonicalHeaderKey(name)
		for _, v := range src.Values(ck) {
			dst.Add(ck, v)
		}
	}
	return dst
}

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openAIErrorEnvelope{
		Error: openAIError{
			Message: message,
			Type:    errorTypeForStatus(status),
			Code:    code,
		},
	})
}

func writeAnthropicError(w http.ResponseWriter, status int, code, message string) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(anthropicErrorEnvelope{
		Type: "error",
		Error: anthropicError{
			Type:    anthropicErrorTypeForStatus(status, code),
			Message: message,
		},
	})
}

func protocolErrorWriterFor(protocol string) protocolErrorWriter {
	normalized := config.NormalizeProviderProtocol(protocol)
	if normalized == config.ProtocolAnthropicMessages {
		return writeAnthropicError
	}
	return writeOpenAIError
}

func anthropicErrorTypeForStatus(status int, code string) string {
	switch {
	case code == "invalid_api_key":
		return "authentication_error"
	case status == http.StatusRequestTimeout:
		return "request_timeout_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}

func errorTypeForStatus(status int) string {
	if status >= 500 {
		return "server_error"
	}
	return "invalid_request_error"
}

func requestReadError(err error) (int, string) {
	var netErr net.Error
	switch {
	case errors.As(err, &netErr) && netErr.Timeout():
		return http.StatusRequestTimeout, "request body read timeout"
	case strings.Contains(strings.ToLower(err.Error()), "timeout"):
		return http.StatusRequestTimeout, "request body read timeout"
	default:
		return http.StatusBadRequest, "read body: " + err.Error()
	}
}

func timeoutDuration(value int, fallback int) time.Duration {
	if value <= 0 {
		value = fallback
	}
	return time.Duration(value) * time.Millisecond
}

func nonNegativeDuration(value int) time.Duration {
	if value < 0 {
		value = 0
	}
	return time.Duration(value) * time.Millisecond
}

func positiveDurationMs(value time.Duration) int64 {
	ms := value.Milliseconds()
	if ms <= 0 {
		return 1
	}
	return ms
}

func minDuration(a time.Duration, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

func normalizeAliasName(model string) string {
	prefix := config.AppName + "/"
	if strings.HasPrefix(model, prefix) {
		trimmed := strings.TrimPrefix(model, prefix)
		if trimmed != "" {
			return trimmed
		}
	}
	return model
}

// writeDebugHeaders sets the X-OCSWITCH-* debug headers before WriteHeader.
func (s *Server) writeDebugHeaders(w http.ResponseWriter, alias, provider, remoteModel string, attempt, failoverCount int) {
	h := w.Header()
	h.Set("X-OCSWITCH-Alias", alias)
	h.Set("X-OCSWITCH-Provider", provider)
	h.Set("X-OCSWITCH-Remote-Model", remoteModel)
	h.Set("X-OCSWITCH-Attempt", fmt.Sprintf("%d", attempt))
	h.Set("X-OCSWITCH-Failover-Count", fmt.Sprintf("%d", failoverCount))
}

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyForwardHeaders(dst, src http.Header) {
	connectionHeaders := connectionDeclaredHeaders(src)
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHopHeaders[ck] || connectionHeaders[ck] {
			continue
		}
		switch ck {
		case "Authorization", "X-Api-Key", "Host", "Content-Length", "Transfer-Encoding", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Via":
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}

func connectionDeclaredHeaders(src http.Header) map[string]bool {
	declared := map[string]bool{}
	for _, raw := range src.Values("Connection") {
		for _, part := range strings.Split(raw, ",") {
			name := http.CanonicalHeaderKey(strings.TrimSpace(part))
			if name != "" {
				declared[name] = true
			}
		}
	}
	return declared
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHopHeaders[ck] {
			continue
		}
		if ck == "Content-Length" {
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneJSONValue(v)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// finishLocalRequestTrace records a local (pre-upstream) failure on the request
// trace so StatusCode/ErrorCode match the HTTP response written to the client.
func finishLocalRequestTrace(trace *RequestTrace, status int, code, msg string) {
	if trace == nil {
		return
	}
	trace.StatusCode = status
	trace.ErrorCode = code
	trace.Error = msg
	trace.Success = false
}

func classifyFailureReason(attempt TraceAttempt, retryable bool) routing.FailureReason {
	if traceAttemptIsClientCanceled(attempt) {
		return routing.FailureClientCanceled
	}
	if attempt.Skipped {
		if strings.Contains(strings.ToLower(attempt.Error), "unavailable") {
			return routing.FailureProviderDisabled
		}
		return routing.FailureUnknown
	}
	if retryable {
		switch {
		case attempt.StatusCode == http.StatusTooManyRequests:
			return routing.FailureRateLimited
		case attempt.StatusCode >= 500:
			return routing.FailureUpstream5xx
		case attempt.StatusCode >= 400:
			return routing.FailureUpstream4xx
		case strings.Contains(attempt.Result, "timeout"):
			return routing.FailureTimeout
		case attempt.Result == "empty_response":
			return routing.FailureEmptyResponse
		case attempt.Result == "stream_error":
			return routing.FailureStreamBroken
		default:
			return routing.FailureTransport
		}
	}
	if attempt.StatusCode >= 400 && attempt.StatusCode < 500 {
		return routing.FailureUpstream4xx
	}
	if strings.Contains(attempt.Result, "timeout") {
		return routing.FailureTimeout
	}
	if attempt.Result == "stream_error" || attempt.Result == "downstream_write_error" || strings.Contains(attempt.Result, "stream") {
		return routing.FailureStreamBroken
	}
	return routing.FailureUnknown
}
