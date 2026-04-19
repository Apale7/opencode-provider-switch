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
)

var firstByteTimeout = 15 * time.Second
var requestReadTimeout = 30 * time.Second
var streamIdleTimeout = 60 * time.Second

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
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

// Server is the local ocswitch HTTP proxy.
type Server struct {
	cfg    *config.Config
	client *http.Client
	logger *log.Logger
}

// New constructs a Server from cfg.
func New(cfg *config.Config) *Server {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: firstByteTimeout,
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
	}
}

// ListenAndServe starts the HTTP listener until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/responses", s.handleResponses)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       requestReadTimeout,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
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

// handleResponses is the main alias→failover proxy entry.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	reqID := atomic.AddUint64(&reqCounter, 1)
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if !s.authorize(r) {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "unauthorized")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 50<<20))
	if err != nil {
		status, msg := requestReadError(err)
		writeOpenAIError(w, status, "invalid_request_error", msg)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid json: "+err.Error())
		return
	}
	aliasName, _ := payload["model"].(string)
	if aliasName == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "missing model field")
		return
	}
	rawModel := aliasName
	aliasName = normalizeAliasName(aliasName)
	s.logger.Printf("req=%d incoming model=%q alias=%q stream=%v", reqID, rawModel, aliasName, payload["stream"])
	alias := s.cfg.FindAlias(aliasName)
	if alias == nil {
		s.logger.Printf("req=%d alias lookup failed for model=%q alias=%q", reqID, rawModel, aliasName)
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("alias %q not found", aliasName))
		return
	}
	if !alias.Enabled {
		s.logger.Printf("req=%d alias=%q disabled", reqID, aliasName)
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("alias %q is disabled", aliasName))
		return
	}
	targets := s.cfg.AvailableTargets(*alias)
	if len(targets) == 0 {
		s.logger.Printf("req=%d alias=%q has no available targets", reqID, aliasName)
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("alias %q has no available targets", aliasName))
		return
	}

	failoverCount := 0
	var lastRetryable *upstreamFailure
	for attempt, t := range targets {
		p := s.cfg.FindProvider(t.Provider)
		if p == nil || !p.IsEnabled() {
			s.logger.Printf("req=%d alias=%s attempt=%d target provider %q unavailable, skipping", reqID, aliasName, attempt+1, t.Provider)
			failoverCount++
			continue
		}
		s.logger.Printf("req=%d alias=%s attempt=%d provider=%s remote_model=%s failovers=%d", reqID, aliasName, attempt+1, p.ID, t.Model, failoverCount)
		cloned := cloneMap(payload)
		cloned["model"] = t.Model
		newBody, err := json.Marshal(cloned)
		if err != nil {
			s.logger.Printf("req=%d marshal error: %v", reqID, err)
			writeOpenAIError(w, http.StatusInternalServerError, "server_error", "marshal error")
			return
		}

		ok, retryable, upstreamErr, failure := s.tryOnce(r.Context(), w, r, p, t, newBody, aliasName, attempt+1, failoverCount)
		if ok {
			return
		}
		if !retryable {
			s.logger.Printf("req=%d alias=%s attempt=%d final failure: %v", reqID, aliasName, attempt+1, upstreamErr)
			return
		}
		if failure != nil {
			lastRetryable = failure
		}
		s.logger.Printf("req=%d alias=%s attempt=%d retryable: %v", reqID, aliasName, attempt+1, upstreamErr)
		failoverCount++
	}

	if lastRetryable != nil {
		copyResponseHeaders(w.Header(), lastRetryable.header)
		w.WriteHeader(lastRetryable.status)
		if len(lastRetryable.body) > 0 {
			_, _ = w.Write(lastRetryable.body)
		}
		return
	}

	writeOpenAIError(w, http.StatusBadGateway, "server_error", fmt.Sprintf("all upstream targets failed for alias %q", aliasName))
}

// tryOnce proxies one attempt. Returns (ok, retryable, err, failure).
// ok=true means successful response fully/partially written to client.
// retryable=true means failure happened before any bytes flushed downstream.
func (s *Server) tryOnce(
	ctx context.Context,
	w http.ResponseWriter,
	clientReq *http.Request,
	provider *config.Provider,
	target config.Target,
	body []byte,
	aliasName string,
	attempt int,
	failoverCount int,
) (ok bool, retryable bool, err error, failure *upstreamFailure) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	upstreamURL := strings.TrimRight(provider.BaseURL, "/") + "/responses"
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return false, false, fmt.Errorf("build request: %w", err), nil
	}
	copyForwardHeaders(upReq.Header, clientReq.Header)
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", clientReq.Header.Get("Accept"))
	if provider.APIKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	for k, v := range provider.Headers {
		upReq.Header.Set(k, v)
	}
	upReq.ContentLength = int64(len(body))

	startedAt := time.Now()
	resp, err := s.client.Do(upReq)
	if err != nil {
		return false, true, fmt.Errorf("upstream dial/transport: %w", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		failure = captureRetryableFailure(resp)
		return false, true, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(failure.body), 200)), failure
	}
	if resp.StatusCode >= 400 {
		s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
		s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return true, false, fmt.Errorf("upstream %d", resp.StatusCode), nil
	}

	remaining := firstByteTimeout - time.Since(startedAt)
	if remaining <= 0 {
		return false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout), nil
	}
	firstChunk, firstErr := readFirstChunk(resp.Body, remaining)
	if firstErr != nil {
		if errors.Is(firstErr, errFirstByteTimeout) {
			return false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout), nil
		}
		if errors.Is(firstErr, io.EOF) {
			if len(firstChunk) == 0 {
				return false, true, fmt.Errorf("upstream closed before first byte"), nil
			}
		} else {
			return false, true, fmt.Errorf("upstream first read: %w", firstErr), nil
		}
	}

	isEventStream := false
	if mediaType, _, parseErr := mime.ParseMediaType(resp.Header.Get("Content-Type")); parseErr == nil {
		isEventStream = mediaType == "text/event-stream"
	}

	s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
	s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if len(firstChunk) > 0 {
		if _, werr := w.Write(firstChunk); werr != nil {
			return true, false, werr, nil
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
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, false, werr, nil
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return true, false, nil, nil
			}
			s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream body read failed after response start: %v", aliasName, attempt, provider.ID, target.Model, rerr)
			return true, false, rerr, nil
		}
	}
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
