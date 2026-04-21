package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
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

// Server is the local ocswitch HTTP proxy.
type Server struct {
	cfg    *config.Config
	client *http.Client
	logger *log.Logger
	traces RequestTraceStore
	store  routing.StateStore
	policy routing.Strategy
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
	policy := routing.MustBuild(cfg.Server.Routing, routing.Dependencies{Store: store})
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
	return &Server{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   0,
		},
		logger: log.New(log.Writer(), "[ocswitch] ", log.LstdFlags|log.Lmicroseconds),
		traces: traces,
		store:  store,
		policy: policy,
	}
}

// ListenAndServe starts the HTTP listener until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	return s.ListenAndServeWithReady(ctx, nil)
}

// ListenAndServeWithReady starts the HTTP listener until ctx is cancelled and
// reports whether the listening socket was bound successfully.
func (s *Server) ListenAndServeWithReady(ctx context.Context, ready chan<- error) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	mux := http.NewServeMux()
	mux.HandleFunc(config.ProtocolLocalRequestPath(config.ProtocolOpenAIResponses), s.handleResponses)
	mux.HandleFunc(config.ProtocolLocalRequestPath(config.ProtocolAnthropicMessages), s.handleMessages)
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
		ReadTimeout:       timeoutDuration(s.cfg.Server.RequestReadTimeoutMs, config.DefaultRequestReadTimeoutMs),
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
	if !s.authorize(r) {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "unauthorized")
		return
	}
	data := []map[string]any{}
	for _, aliasName := range s.cfg.AvailableAliasNames() {
		data = append(data, map[string]any{
			"id":       aliasName,
			"object":   "model",
			"owned_by": config.AppName,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}

// authorize enforces the static local api key when one is configured.
func (s *Server) authorize(r *http.Request) bool {
	expected := s.cfg.Server.APIKey
	if expected == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ") == expected
	}
	if k := r.Header.Get("X-Api-Key"); k != "" {
		return k == expected
	}
	return false
}

var reqCounter uint64

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolRequest(config.ProtocolOpenAIResponses, w, r)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.handleProtocolRequest(config.ProtocolAnthropicMessages, w, r)
}

// handleProtocolRequest is the main alias→failover proxy entry.
func (s *Server) handleProtocolRequest(protocol string, w http.ResponseWriter, r *http.Request) {
	protocol = config.NormalizeProviderProtocol(protocol)
	writeProtocolError := protocolErrorWriterFor(protocol)
	reqID := atomic.AddUint64(&reqCounter, 1)
	startedAt := time.Now()
	if r.Method != http.MethodPost {
		writeProtocolError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.authorize(r) {
		writeProtocolError(w, http.StatusUnauthorized, "invalid_api_key", "unauthorized")
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
		if err := s.traces.Add(context.Background(), trace); err != nil {
			s.logger.Printf("req=%d trace persist failed: %v", reqID, err)
		}
	}()
	s.logger.Printf("req=%d incoming model=%q alias=%q stream=%v", reqID, rawModel, aliasName, payload["stream"])
	alias := s.cfg.FindAlias(aliasName)
	if alias == nil {
		s.logger.Printf("req=%d alias lookup failed for model=%q alias=%q", reqID, rawModel, aliasName)
		trace.Error = fmt.Sprintf("alias %q not found", aliasName)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("alias %q not found", aliasName))
		return
	}
	if !config.ProtocolsMatch(alias.Protocol, protocol) {
		trace.Error = fmt.Sprintf("alias %q does not support protocol %q", aliasName, protocol)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("alias %q does not support protocol %q", aliasName, protocol))
		return
	}
	if !alias.Enabled {
		s.logger.Printf("req=%d alias=%q disabled", reqID, aliasName)
		trace.Error = fmt.Sprintf("alias %q is disabled", aliasName)
		writeProtocolError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("alias %q is disabled", aliasName))
		return
	}
	targets := s.cfg.AvailableTargets(*alias)
	if len(targets) == 0 {
		s.logger.Printf("req=%d alias=%q has no available targets", reqID, aliasName)
		trace.Error = fmt.Sprintf("alias %q has no available targets", aliasName)
		writeProtocolError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("alias %q has no available targets", aliasName))
		return
	}

	failoverCount := 0
	var lastRetryable *upstreamFailure
	candidates := make([]routing.Candidate, 0, len(targets))
	for index, t := range targets {
		provider := s.cfg.FindProvider(t.Provider)
		baseURL := ""
		if provider != nil {
			baseURL = provider.BaseURL
		}
		candidates = append(candidates, routing.Candidate{Index: index, ProviderID: t.Provider, Provider: t.Provider, Protocol: protocol, Model: t.Model, BaseURL: baseURL})
	}
	session := s.policy.NewSession(routing.SessionInput{Now: startedAt, RequestID: reqID, Protocol: protocol, Alias: aliasName, Candidates: candidates})
	attempt := 0
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
			attemptTrace.Skipped = true
			attemptTrace.Result = "skipped"
			attemptTrace.Error = decision.SkipReason
			attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
			trace.Attempts = append(trace.Attempts, attemptTrace)
			session.Report(routing.AttemptFeedback{Candidate: decision.Candidate, StartedAt: attemptTrace.StartedAt, FinishedAt: time.Now(), Duration: time.Since(attemptTrace.StartedAt), Retryable: true, Outcome: routing.OutcomeSkipped, FailureReason: routing.FailureStrategySkipped})
			failoverCount++
			continue
		}
		p := s.cfg.FindProvider(t.Provider)
		if p == nil || !p.IsEnabled() || !config.ProtocolsMatch(protocol, p.Protocol) {
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
		s.logger.Printf("req=%d alias=%s attempt=%d provider=%s remote_model=%s failovers=%d", reqID, aliasName, attempt, p.ID, t.Model, failoverCount)
		cloned := cloneMap(payload)
		cloned["model"] = t.Model
		upstreamURL := strings.TrimRight(p.BaseURL, "/") + config.ProtocolUpstreamRequestPath(protocol)
		attemptTrace.URL = upstreamURL
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

		handled, success, retryable, upstreamErr, failure := s.tryOnce(r.Context(), protocol, w, r, p, t, newBody, aliasName, attempt, failoverCount, &attemptTrace, &trace)
		attemptTrace.DurationMs = time.Since(attemptTrace.StartedAt).Milliseconds()
		trace.Attempts = append(trace.Attempts, attemptTrace)
		trace.FinalProvider = p.ID
		trace.FinalModel = t.Model
		trace.FinalURL = upstreamURL
		trace.StatusCode = attemptTrace.StatusCode
		if trace.FirstByteMs == 0 {
			trace.FirstByteMs = attemptTrace.FirstByteMs
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
		if success {
			feedback.Outcome = routing.OutcomeSuccess
		} else if retryable {
			feedback.Outcome = routing.OutcomeRetryableFail
		} else if handled && attemptTrace.FirstByteMs > 0 {
			feedback.Outcome = routing.OutcomePostCommitFail
		} else {
			feedback.Outcome = routing.OutcomeTerminalFail
		}
		session.Report(feedback)
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

// tryOnce proxies one attempt. Returns (handled, success, retryable, err, failure).
// handled=true means a downstream response has already been started or completed.
// retryable=true means failure happened before any bytes flushed downstream.
func (s *Server) tryOnce(
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
	firstByteTimeout := timeoutDuration(s.cfg.Server.FirstByteTimeoutMs, config.DefaultFirstByteTimeoutMs)
	resp, err := s.client.Do(upReq)
	if err != nil {
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

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
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
	if remaining <= 0 {
		return false, false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout), nil
	}
	firstChunk, firstErr := readFirstChunk(resp.Body, remaining)
	if firstErr != nil {
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

	isEventStream := false
	streamIdleTimeout := timeoutDuration(s.cfg.Server.StreamIdleTimeoutMs, config.DefaultStreamIdleTimeoutMs)
	if mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); parseErr == nil {
		isEventStream = mediaType == "text/event-stream"
	}
	usageCollector := newUsageCollector(protocol, resp.Header.Get("Content-Type"))

	s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
	s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if len(firstChunk) > 0 {
		usageCollector.Add(firstChunk)
		if _, werr := w.Write(firstChunk); werr != nil {
			if trace != nil {
				applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "downstream write failed before usage finalized"))
			}
			if attemptTrace != nil {
				attemptTrace.Result = "downstream_write_error"
				attemptTrace.Error = werr.Error()
			}
			return true, false, false, werr, nil
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
		if isEventStream {
			n, rerr = resp.Body.Read(buf)
		} else {
			n, rerr = readChunkWithTimeout(resp.Body, buf, streamIdleTimeout)
		}
		if n > 0 {
			usageCollector.Add(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				if trace != nil {
					applyUsageToTrace(trace, usageForStreamFailure(usageCollector, "downstream write failed before usage finalized"))
				}
				if attemptTrace != nil {
					attemptTrace.Result = "downstream_write_error"
					attemptTrace.Error = werr.Error()
				}
				return true, false, false, werr, nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
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

var errFirstByteTimeout = errors.New("first byte timeout")
var errStreamIdleTimeout = errors.New("stream idle timeout")

func readFirstChunk(r io.Reader, timeout time.Duration) ([]byte, error) {
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
	select {
	case res := <-ch:
		return res.buf, res.err
	case <-time.After(timeout):
		return nil, errFirstByteTimeout
	}
}

func readChunkWithTimeout(r io.Reader, buf []byte, timeout time.Duration) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := r.Read(buf)
		ch <- result{n: n, err: err}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
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
	if config.NormalizeProviderProtocol(protocol) == config.ProtocolAnthropicMessages {
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
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func classifyFailureReason(attempt TraceAttempt, retryable bool) routing.FailureReason {
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
	if attempt.Result == "stream_error" || attempt.Result == "downstream_write_error" {
		return routing.FailureStreamBroken
	}
	return routing.FailureUnknown
}
