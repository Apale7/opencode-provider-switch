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

func (s *pingSpyService) SyncOpenCode(ctx context.Context, in appcore.SyncInput) (appcore.SyncResult, error) {
	return s.Service.ApplyOpenCodeSync(ctx, in)
}
