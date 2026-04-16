// Package proxy implements the local `/v1/responses` HTTP server that resolves
// ops aliases and forwards requests to upstream providers with deterministic
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

// Server is the local ops HTTP proxy.
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
		logger: log.New(log.Writer(), "[ops] ", log.LstdFlags|log.Lmicroseconds),
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
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	data := []map[string]any{}
	for _, a := range s.cfg.Aliases {
		if !a.Enabled {
			continue
		}
		data = append(data, map[string]any{
			"id":       a.Alias,
			"object":   "model",
			"owned_by": "ops",
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 50<<20))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	aliasName, _ := payload["model"].(string)
	if aliasName == "" {
		http.Error(w, "missing model field", http.StatusBadRequest)
		return
	}
	alias := s.cfg.FindAlias(aliasName)
	if alias == nil {
		http.Error(w, fmt.Sprintf("alias %q not found", aliasName), http.StatusNotFound)
		return
	}
	if !alias.Enabled {
		http.Error(w, fmt.Sprintf("alias %q is disabled", aliasName), http.StatusNotFound)
		return
	}
	var targets []config.Target
	for _, t := range alias.Targets {
		if t.Enabled {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		http.Error(w, fmt.Sprintf("alias %q has no enabled targets", aliasName), http.StatusFailedDependency)
		return
	}

	failoverCount := 0
	for attempt, t := range targets {
		p := s.cfg.FindProvider(t.Provider)
		if p == nil {
			s.logger.Printf("req=%d alias=%s attempt=%d target provider %q missing, skipping", reqID, aliasName, attempt+1, t.Provider)
			failoverCount++
			continue
		}
		// rewrite payload.model for this upstream
		cloned := cloneMap(payload)
		cloned["model"] = t.Model
		newBody, err := json.Marshal(cloned)
		if err != nil {
			s.logger.Printf("req=%d marshal error: %v", reqID, err)
			http.Error(w, "marshal error", http.StatusInternalServerError)
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
	http.Error(w, fmt.Sprintf("all upstream targets failed for alias %q", aliasName), http.StatusBadGateway)
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
		s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return true, false, fmt.Errorf("upstream %d", resp.StatusCode)
	}

	// 2xx: start streaming pass-through. From this point no failover is allowed.
	s.writeDebugHeaders(w, aliasName, provider.ID, target.Model, attempt, failoverCount)
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)
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

// writeDebugHeaders sets the X-OPS-* debug headers before WriteHeader.
func (s *Server) writeDebugHeaders(w http.ResponseWriter, alias, provider, remoteModel string, attempt, failoverCount int) {
	h := w.Header()
	h.Set("X-OPS-Alias", alias)
	h.Set("X-OPS-Provider", provider)
	h.Set("X-OPS-Remote-Model", remoteModel)
	h.Set("X-OPS-Attempt", fmt.Sprintf("%d", attempt))
	h.Set("X-OPS-Failover-Count", fmt.Sprintf("%d", failoverCount))
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
