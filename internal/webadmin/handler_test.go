package webadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appcore "github.com/Apale7/opencode-provider-switch/internal/app"
)

func TestProviderPingRouteRunsThroughService(t *testing.T) {
	t.Parallel()

	service := &pingSpyService{Service: appcore.NewService(filepath.Join(t.TempDir(), "config.json"))}
	h, err := NewHandler(Options{
		Version:    "test",
		Shell:      "server",
		Service:    service,
		ServerMode: true,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/providers/ping", strings.NewReader(`{"id":"demo","protocol":"openai-responses","baseUrl":"https://upstream.example/v1","apiKey":"sk-test","headers":{"X-Test":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	h.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	if !service.pingCalled {
		t.Fatal("PingProviderBaseURL was not called")
	}
	if service.pingInput.BaseURL != "https://upstream.example/v1" || service.pingInput.Protocol != "openai-responses" {
		t.Fatalf("ping input = %#v", service.pingInput)
	}
	if service.pingInput.APIKey != "sk-test" || service.pingInput.Headers["X-Test"] != "1" {
		t.Fatalf("ping auth input = %#v", service.pingInput)
	}

	var payload struct {
		Data appcore.ProviderPingResult `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.Data.Reachable || payload.Data.LatencyMs != 12 {
		t.Fatalf("payload = %#v", payload.Data)
	}
}

func TestRewriteRuleRoutesRunThroughService(t *testing.T) {
	t.Parallel()

	service := &pingSpyService{Service: appcore.NewService(filepath.Join(t.TempDir(), "config.json"))}
	h, err := NewHandler(Options{
		Version:    "test",
		Shell:      "server",
		Service:    service,
		ServerMode: true,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	post := func(path string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := httptest.NewRecorder()
		h.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("POST %s status = %d, want %d body=%s", path, resp.Code, http.StatusOK, resp.Body.String())
		}
		return resp
	}

	post("/api/rewrite-rules", `{"name":"fast","alias":"chat","enabled":true,"ops":[{"op":"set","path":"$.store","value":false}]}`)
	post("/api/rewrite-rules", `{"name":"strip","alias":"chat","providers":["p1"],"enabled":true,"override":true,"ops":[{"op":"delete","path":"$.store"}]}`)
	post("/api/rewrite-rules/state", `{"name":"fast","enabled":false}`)
	post("/api/rewrite-rules/reorder", `{"names":["strip","fast"]}`)

	req := httptest.NewRequest(http.MethodGet, "/api/rewrite-rules", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/rewrite-rules status = %d body=%s", resp.Code, resp.Body.String())
	}
	var listPayload struct {
		Data []appcore.RequestRewriteRuleView `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("json.Unmarshal(list) error = %v", err)
	}
	if len(listPayload.Data) != 2 || listPayload.Data[0].Name != "strip" || listPayload.Data[1].Enabled {
		t.Fatalf("list payload = %#v", listPayload.Data)
	}

	deleteResp := post("/api/rewrite-rules/delete", `{"name":"strip"}`)
	var deletePayload struct {
		Data appcore.RequestRewriteRuleRemoveResult `json:"data"`
	}
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &deletePayload); err != nil {
		t.Fatalf("json.Unmarshal(delete) error = %v", err)
	}
	if !deletePayload.Data.OK {
		t.Fatalf("delete payload = %#v", deletePayload.Data)
	}
}

type pingSpyService struct {
	*appcore.Service
	pingCalled bool
	pingInput  appcore.ProviderPingInput
}

func (s *pingSpyService) PingProviderBaseURL(ctx context.Context, in appcore.ProviderPingInput) (appcore.ProviderPingResult, error) {
	_ = ctx
	s.pingCalled = true
	s.pingInput = in
	return appcore.ProviderPingResult{
		ID:         in.ID,
		BaseURL:    in.BaseURL,
		LatencyMs:  12,
		Reachable:  true,
		StatusCode: http.StatusOK,
	}, nil
}

func (s *pingSpyService) StartProxy(ctx context.Context) (appcore.ProxyStatusView, error) {
	if err := s.Service.StartProxy(ctx); err != nil {
		return appcore.ProxyStatusView{}, err
	}
	return s.Service.GetProxyStatus(ctx)
}

func (s *pingSpyService) StopProxy(ctx context.Context) (appcore.ProxyStatusView, error) {
	if err := s.Service.StopProxy(ctx); err != nil {
		return appcore.ProxyStatusView{}, err
	}
	return s.Service.GetProxyStatus(ctx)
}

func TestLifecycleRoutesAndRevisionConflictMapping(t *testing.T) {
	t.Parallel()

	service := &pingSpyService{Service: appcore.NewService(filepath.Join(t.TempDir(), "config.json"))}
	h, err := NewHandler(Options{
		Version:    "test",
		Shell:      "server",
		Service:    service,
		ServerMode: true,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	revReq := httptest.NewRequest(http.MethodGet, "/api/config/revision", nil)
	revResp := httptest.NewRecorder()
	h.ServeHTTP(revResp, revReq)
	if revResp.Code != http.StatusOK {
		t.Fatalf("revision status = %d body=%s", revResp.Code, revResp.Body.String())
	}
	var revPayload struct {
		OK   bool `json:"ok"`
		Data struct {
			Revision appcore.ConfigRevision `json:"revision"`
		} `json:"data"`
		Outcome struct {
			Code string `json:"code"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(revResp.Body.Bytes(), &revPayload); err != nil {
		t.Fatalf("unmarshal revision: %v", err)
	}
	if !revPayload.OK || revPayload.Outcome.Code != "ok" || revPayload.Data.Revision == "" {
		t.Fatalf("revision payload = %#v", revPayload)
	}

	// Stale revision must map to HTTP 409 + stable code.
	body := `{"revision":"stale-revision","operation":{"kind":"provider.remove","payload":{"providerId":"missing"}},"selections":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/lifecycle/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict {
		t.Fatalf("preview stale status = %d body=%s", resp.Code, resp.Body.String())
	}
	var conflict struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Outcome struct {
			Code   string         `json:"code"`
			Params map[string]any `json:"params"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &conflict); err != nil {
		t.Fatalf("unmarshal conflict: %v", err)
	}
	if conflict.OK || conflict.Error != "revision_conflict" || conflict.Outcome.Code != "revision_conflict" {
		t.Fatalf("conflict payload = %#v", conflict)
	}
}

func (s *pingSpyService) SyncOpenCode(ctx context.Context, in appcore.SyncInput) (appcore.SyncResult, error) {
	return s.Service.ApplyOpenCodeSync(ctx, in)
}
