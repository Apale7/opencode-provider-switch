// Package proxy implements the local `/v1/responses` HTTP server that resolves
// olpx aliases and forwards requests to upstream providers with deterministic
// pre-first-byte failover.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anomalyco/opencode-provider-switch/internal/config"
)

var firstByteTimeout = 15 * time.Second

type openAIErrorEnvelope struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type,omitempty"`
	Code    string `json:"code,omitempty"`
}

// Server is the local olpx HTTP proxy.
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
		// no response buffering so streams flow through immediately
		DisableCompression: false,
		ForceAttemptHTTP2:  true,
	}
	return &Server{
		cfg: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   0, // streaming, no overall timeout
		},
		logger: log.New(log.Writer(), "[olpx] ", log.LstdFlags|log.Lmicroseconds),
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
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	s.logger.Printf("listening on http://%s", addr)
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
			"owned_by": "olpx",
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
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "read body: "+err.Error())
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
	for attempt, t := range targets {
		p := s.cfg.FindProvider(t.Provider)
		if p == nil || !p.IsEnabled() {
			s.logger.Printf("req=%d alias=%s attempt=%d target provider %q unavailable, skipping", reqID, aliasName, attempt+1, t.Provider)
			failoverCount++
			continue
		}
		s.logger.Printf("req=%d alias=%s attempt=%d provider=%s remote_model=%s failovers=%d", reqID, aliasName, attempt+1, p.ID, t.Model, failoverCount)
		// rewrite payload.model for this upstream
		cloned := cloneMap(payload)
		cloned["model"] = t.Model
		newBody, err := json.Marshal(cloned)
		if err != nil {
			s.logger.Printf("req=%d marshal error: %v", reqID, err)
			writeOpenAIError(w, http.StatusInternalServerError, "server_error", "marshal error")
			return
		}

		ok, retryable, upstreamErr := s.tryOnce(r.Context(), w, r, p, t, newBody, aliasName, attempt+1, failoverCount)
		if ok {
			return
		}
		if !retryable {
			// final — response was either already committed or unrecoverable
			s.logger.Printf("req=%d alias=%s attempt=%d final failure: %v", reqID, aliasName, attempt+1, upstreamErr)
			return
		}
		s.logger.Printf("req=%d alias=%s attempt=%d retryable: %v", reqID, aliasName, attempt+1, upstreamErr)
		failoverCount++
	}

	// exhausted all targets with retryable failures
	writeOpenAIError(w, http.StatusBadGateway, "server_error", fmt.Sprintf("all upstream targets failed for alias %q", aliasName))
}

// tryOnce proxies one attempt. Returns (ok, retryable, err).
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
) (ok bool, retryable bool, err error) {
	upstreamURL := strings.TrimRight(provider.BaseURL, "/") + "/responses"
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return false, false, fmt.Errorf("build request: %w", err)
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
		return false, true, fmt.Errorf("upstream dial/transport: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 || resp.StatusCode == 429 {
		// drain up to small cap for logging context
		peek, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return false, true, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(peek), 200))
	}
	if resp.StatusCode >= 400 {
		// non-retryable: forward status + body to client
		s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
		s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return true, false, fmt.Errorf("upstream %d", resp.StatusCode)
	}

	remaining := firstByteTimeout - time.Since(startedAt)
	if remaining <= 0 {
		_ = resp.Body.Close()
		return false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout)
	}
	firstChunk, firstErr := readFirstChunk(resp.Body, remaining)
	if firstErr != nil {
		if errors.Is(firstErr, errFirstByteTimeout) {
			_ = resp.Body.Close()
			return false, true, fmt.Errorf("upstream first byte timeout after %s", firstByteTimeout)
		}
		if errors.Is(firstErr, io.EOF) {
			if len(firstChunk) == 0 {
				firstChunk = nil
			}
		} else {
			return false, true, fmt.Errorf("upstream first read: %w", firstErr)
		}
	}

	// 2xx: start streaming pass-through. From this point no failover is allowed.
	s.logger.Printf("alias=%s attempt=%d provider=%s remote_model=%s upstream_status=%d", aliasName, attempt, provider.ID, target.Model, resp.StatusCode)
	s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
	if len(firstChunk) > 0 {
		if _, werr := w.Write(firstChunk); werr != nil {
			return true, false, werr
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	buf := make([]byte, 16<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, false, werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return true, false, nil
			}
			// mid-stream error — already committed, cannot failover
			return true, false, rerr
		}
	}
}

var errFirstByteTimeout = errors.New("first byte timeout")

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

func normalizeAliasName(model string) string {
	const prefix = "olpx/"
	if strings.HasPrefix(model, prefix) {
		trimmed := strings.TrimPrefix(model, prefix)
		if trimmed != "" {
			return trimmed
		}
	}
	return model
}

// writeDebugHeaders sets the X-OLPX-* debug headers before WriteHeader.
func (s *Server) writeDebugHeaders(w http.ResponseWriter, alias, provider, remoteModel string, attempt, failoverCount int) {
	h := w.Header()
	h.Set("X-OLPX-Alias", alias)
	h.Set("X-OLPX-Provider", provider)
	h.Set("X-OLPX-Remote-Model", remoteModel)
	h.Set("X-OLPX-Attempt", fmt.Sprintf("%d", attempt))
	h.Set("X-OLPX-Failover-Count", fmt.Sprintf("%d", failoverCount))
}

// hopByHopHeaders lists headers that must not be forwarded per RFC 7230.
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

// copyForwardHeaders copies safe request headers from client to upstream,
// dropping Authorization (the upstream key replaces it) and hop-by-hop headers.
func copyForwardHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHopHeaders[ck] {
			continue
		}
		switch ck {
		case "Authorization", "X-Api-Key", "Host", "Content-Length":
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}

// copyResponseHeaders copies upstream response headers into client response.
func copyResponseHeaders(dst, src http.Header) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHopHeaders[ck] {
			continue
		}
		if ck == "Content-Length" {
			// We may transform nothing, but streaming responses often omit this.
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}

// cloneMap performs a shallow copy of a top-level map.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// truncate returns s truncated to at most n bytes (best-effort).
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
