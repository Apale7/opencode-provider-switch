package webadmin

import (
	"context"
	"encoding/json"
	"fmt"
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
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
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

func TestProviderGroupRoutesRequireAuth(t *testing.T) {
	t.Parallel()

	service := newGroupSpyService(t)
	h, err := NewHandler(Options{
		Version:    "test",
		Shell:      "server",
		Service:    service,
		ServerMode: true,
		Auth: func(w http.ResponseWriter, r *http.Request) bool {
			if r.Header.Get("Authorization") != "Bearer secret" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}` + "\n"))
				return false
			}
			return true
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/providers/vendor-a/groups", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.listCalled {
		t.Fatal("list must not run without auth")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/providers/vendor-a/groups", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp = httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("auth status = %d body=%s", resp.Code, resp.Body.String())
	}
	if !service.listCalled {
		t.Fatal("list must run with valid auth")
	}
}

func TestProviderGroupSixRoutesAndPathIdentity(t *testing.T) {
	t.Parallel()

	service := newGroupSpyService(t)
	h := mustNewGroupHandler(t, service)

	// 1) list
	resp := serveJSON(t, h, http.MethodGet, "/api/admin/providers/path-provider/groups", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.listProviderID != "path-provider" {
		t.Fatalf("list providerID = %q", service.listProviderID)
	}
	assertNoPlaintextSecrets(t, resp.Body.Bytes())

	// 2) create — body providerId must not override path
	resp = serveJSON(t, h, http.MethodPost, "/api/admin/providers/path-provider/groups", `{
		"providerId":"body-provider",
		"group":{
			"id":"premium",
			"name":"Premium",
			"protocol":"openai",
			"apiKeysChanged":true,
			"apiKeys":["sk-fake-primary-aaaa","sk-fake-backup-bbbb"]
		}
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.createIn.ProviderID != "path-provider" {
		t.Fatalf("create path identity = %#v", service.createIn)
	}
	if service.createIn.Group.ID != "premium" || !service.createIn.Group.APIKeysChanged {
		t.Fatalf("create group = %#v", service.createIn.Group)
	}
	assertNoPlaintextSecrets(t, resp.Body.Bytes())
	var createPayload struct {
		Data appcore.ProviderGroupView `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("create unmarshal: %v", err)
	}
	if createPayload.Data.APIKeyCount != 2 || len(createPayload.Data.APIKeysMasked) != 2 {
		t.Fatalf("create view = %#v", createPayload.Data)
	}
	if strings.Contains(resp.Body.String(), "sk-fake-primary-aaaa") {
		t.Fatalf("create response leaked plaintext: %s", resp.Body.String())
	}

	// 3) update — path groupID is old id; body Group.ID may rename
	resp = serveJSON(t, h, http.MethodPut, "/api/admin/providers/path-provider/groups/premium", `{
		"providerId":"body-provider",
		"groupId":"body-group",
		"group":{"id":"premium-renamed","name":"Renamed","protocol":"openai","apiKeysChanged":false},
		"selections":[{"choiceId":"rename-refs","optionId":"rebind_target","params":{"provider":"path-provider","group":"premium-renamed","model":"m1"}}]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.updateIn.ProviderID != "path-provider" || service.updateIn.GroupID != "premium" {
		t.Fatalf("update path identity = %#v", service.updateIn)
	}
	if service.updateIn.Group.ID != "premium-renamed" {
		t.Fatalf("update new id = %#v", service.updateIn.Group)
	}
	if len(service.updateIn.Selections) != 1 || service.updateIn.Selections[0].ChoiceID != "rename-refs" {
		t.Fatalf("update selections = %#v", service.updateIn.Selections)
	}
	assertNoPlaintextSecrets(t, resp.Body.Bytes())

	// 4) delete — optional lifecycle body, path identity wins
	resp = serveJSON(t, h, http.MethodDelete, "/api/admin/providers/path-provider/groups/premium", `{
		"providerId":"body-provider",
		"groupId":"body-group",
		"selections":[{"choiceId":"protected-target","optionId":"drop_target"}]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.deleteIn.ProviderID != "path-provider" || service.deleteIn.GroupID != "premium" {
		t.Fatalf("delete path identity = %#v", service.deleteIn)
	}
	if len(service.deleteIn.Selections) != 1 || service.deleteIn.Selections[0].OptionID != "drop_target" {
		t.Fatalf("delete selections = %#v", service.deleteIn.Selections)
	}

	// empty delete body is valid
	service.deleteIn = appcore.ProviderGroupDeleteInput{}
	resp = serveJSON(t, h, http.MethodDelete, "/api/admin/providers/path-provider/groups/free", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("delete empty body status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.deleteIn.ProviderID != "path-provider" || service.deleteIn.GroupID != "free" {
		t.Fatalf("delete empty body identity = %#v", service.deleteIn)
	}

	// 5) refresh-models
	resp = serveJSON(t, h, http.MethodPost, "/api/admin/providers/path-provider/groups/premium/refresh-models", `{
		"providerId":"body-provider","groupId":"body-group"
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.refreshIn.ProviderID != "path-provider" || service.refreshIn.GroupID != "premium" {
		t.Fatalf("refresh path identity = %#v", service.refreshIn)
	}

	// 6) ping
	resp = serveJSON(t, h, http.MethodPost, "/api/admin/providers/path-provider/groups/premium/ping", `{
		"providerId":"body-provider","groupId":"body-group","baseUrl":"https://upstream.example/v1","protocol":"openai"
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("ping status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.pingGroupIn.ProviderID != "path-provider" || service.pingGroupIn.GroupID != "premium" {
		t.Fatalf("ping path identity = %#v", service.pingGroupIn)
	}
	if service.pingGroupIn.BaseURL != "https://upstream.example/v1" {
		t.Fatalf("ping base url = %#v", service.pingGroupIn)
	}
}

func TestProviderGroupRouteErrorsAndNoAPIKeysPaths(t *testing.T) {
	t.Parallel()

	service := newGroupSpyService(t)
	service.listErr = fmt.Errorf("provider %q not found", "missing")
	service.createErr = fmt.Errorf("provider %q not found", "missing")
	service.updateErr = fmt.Errorf("provider %q group %q not found", "p", "g")
	h := mustNewGroupHandler(t, service)

	// Service not-found → existing envelope (legacy 400 + message).
	resp := serveJSON(t, h, http.MethodGet, "/api/admin/providers/missing/groups", "")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("list missing status = %d body=%s", resp.Code, resp.Body.String())
	}

	// Invalid JSON → 400 invalid_request
	resp = serveJSON(t, h, http.MethodPost, "/api/admin/providers/p1/groups", `{not-json`)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("invalid json status = %d body=%s", resp.Code, resp.Body.String())
	}
	var bad struct {
		Error   string `json:"error"`
		Outcome struct {
			Code string `json:"code"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &bad); err != nil {
		t.Fatalf("unmarshal bad json: %v", err)
	}
	if bad.Error != "invalid_request" || bad.Outcome.Code != "invalid_request" {
		t.Fatalf("invalid json envelope = %#v", bad)
	}

	// Wrong method on groups collection → 405
	resp = serveJSON(t, h, http.MethodPut, "/api/admin/providers/p1/groups", `{}`)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method status = %d body=%s", resp.Code, resp.Body.String())
	}

	// Frozen isolation: never register client proxy api-keys under /api/ admin mux.
	forbiddenAPI := []string{
		"/api/admin/api-keys",
		"/api/admin/providers/p1/api-keys",
		"/api/admin/providers/p1/groups/g1/api-keys",
	}
	for _, path := range forbiddenAPI {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s status = %d, want 404 (must not register api-keys routes) body=%s",
					method, path, rec.Code, rec.Body.String())
			}
		}
	}
	// Bare /api-keys is not under /api/ prefix routing; ensure it is not a JSON admin API.
	req := httptest.NewRequest(http.MethodGet, "/api-keys", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	ct := rec.Header().Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var env struct {
			OK   bool `json:"ok"`
			Data any  `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err == nil && env.OK {
			t.Fatalf("/api-keys must not be a successful admin JSON route, body=%s", rec.Body.String())
		}
	}
}

func TestAliasTargetBodyKeepsGroupField(t *testing.T) {
	t.Parallel()

	service := newGroupSpyService(t)
	h := mustNewGroupHandler(t, service)

	resp := serveJSON(t, h, http.MethodPost, "/api/aliases/bind", `{
		"alias":"chat","provider":"vendor-a","group":"premium","model":"m1","disabled":false
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("bind status = %d body=%s", resp.Code, resp.Body.String())
	}
	if service.bindIn.Alias != "chat" || service.bindIn.Provider != "vendor-a" ||
		service.bindIn.Group != "premium" || service.bindIn.Model != "m1" {
		t.Fatalf("bind input lost Group field: %#v", service.bindIn)
	}

	resp = serveJSON(t, h, http.MethodPost, "/api/aliases/reorder-targets", `{
		"alias":"chat",
		"targets":[
			{"provider":"vendor-a","group":"premium","model":"m1"},
			{"provider":"vendor-a","group":"default","model":"m1"}
		]
	}`)
	if resp.Code != http.StatusOK {
		t.Fatalf("reorder status = %d body=%s", resp.Code, resp.Body.String())
	}
	if len(service.reorderIn.Targets) != 2 || service.reorderIn.Targets[0].Group != "premium" ||
		service.reorderIn.Targets[1].Group != "default" {
		t.Fatalf("reorder targets lost Group: %#v", service.reorderIn)
	}
}

func mustNewGroupHandler(t *testing.T, service Service) http.Handler {
	t.Helper()
	h, err := NewHandler(Options{
		Version:    "test",
		Shell:      "server",
		Service:    service,
		ServerMode: true,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return h
}

func serveJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

func assertNoPlaintextSecrets(t *testing.T, raw []byte) {
	t.Helper()
	// Never echo known plaintext fixture secrets.
	if strings.Contains(string(raw), "sk-fake-primary-aaaa") || strings.Contains(string(raw), "sk-fake-backup-bbbb") {
		t.Fatalf("response leaked plaintext secret: %s", string(raw))
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("response is not JSON: %v body=%s", err, string(raw))
	}
	assertJSONTreeNoPlaintextKeys(t, envelope)
}

func assertJSONTreeNoPlaintextKeys(t *testing.T, v any) {
	t.Helper()
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			switch k {
			case "apiKey", "apiKeys", "api_key", "api_keys", "key", "secret", "plaintext":
				t.Fatalf("response JSON tree contains plaintext key field %q", k)
			}
			assertJSONTreeNoPlaintextKeys(t, child)
		}
	case []any:
		for _, child := range node {
			assertJSONTreeNoPlaintextKeys(t, child)
		}
	}
}

type groupSpyService struct {
	*pingSpyService

	listCalled     bool
	listProviderID string
	listErr        error

	createIn  appcore.ProviderGroupCreateInput
	createErr error

	updateIn  appcore.ProviderGroupUpdateInput
	updateErr error

	deleteIn  appcore.ProviderGroupDeleteInput
	deleteErr error

	refreshIn  appcore.ProviderGroupRefreshModelsInput
	refreshErr error

	pingGroupIn  appcore.ProviderGroupPingInput
	pingGroupErr error

	bindIn    appcore.AliasTargetInput
	reorderIn appcore.AliasTargetReorderInput
}

func newGroupSpyService(t *testing.T) *groupSpyService {
	t.Helper()
	return &groupSpyService{
		pingSpyService: &pingSpyService{Service: appcore.NewService(filepath.Join(t.TempDir(), "config.json"))},
	}
}

func (s *groupSpyService) ListProviderGroups(ctx context.Context, providerID string) ([]appcore.ProviderGroupView, error) {
	_ = ctx
	s.listCalled = true
	s.listProviderID = providerID
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []appcore.ProviderGroupView{{
		ID:            "default",
		Name:          "Default",
		Protocol:      "openai",
		APIKeyCount:   1,
		APIKeysMasked: []string{"sk-f…aaaa"},
		Models:        []string{"m1"},
	}}, nil
}

func (s *groupSpyService) CreateProviderGroup(ctx context.Context, in appcore.ProviderGroupCreateInput) (appcore.ProviderGroupView, error) {
	_ = ctx
	s.createIn = in
	if s.createErr != nil {
		return appcore.ProviderGroupView{}, s.createErr
	}
	masked := make([]string, 0, len(in.Group.APIKeys))
	for range in.Group.APIKeys {
		masked = append(masked, "sk-f…xxxx")
	}
	return appcore.ProviderGroupView{
		ID:            in.Group.ID,
		Name:          in.Group.Name,
		Protocol:      in.Group.Protocol,
		APIKeyCount:   len(in.Group.APIKeys),
		APIKeysMasked: masked,
		Models:        append([]string(nil), in.Group.Models...),
		Disabled:      in.Group.Disabled,
	}, nil
}

func (s *groupSpyService) UpdateProviderGroup(ctx context.Context, in appcore.ProviderGroupUpdateInput) (appcore.ProviderGroupView, error) {
	_ = ctx
	s.updateIn = in
	if s.updateErr != nil {
		return appcore.ProviderGroupView{}, s.updateErr
	}
	id := in.Group.ID
	if id == "" {
		id = in.GroupID
	}
	return appcore.ProviderGroupView{
		ID:            id,
		Name:          in.Group.Name,
		Protocol:      in.Group.Protocol,
		APIKeyCount:   0,
		APIKeysMasked: nil,
		Models:        append([]string(nil), in.Group.Models...),
		Disabled:      in.Group.Disabled,
	}, nil
}

func (s *groupSpyService) DeleteProviderGroup(ctx context.Context, in appcore.ProviderGroupDeleteInput) error {
	_ = ctx
	s.deleteIn = in
	return s.deleteErr
}

func (s *groupSpyService) RefreshProviderGroupModels(ctx context.Context, in appcore.ProviderGroupRefreshModelsInput) (appcore.ProviderSaveResult, error) {
	_ = ctx
	s.refreshIn = in
	if s.refreshErr != nil {
		return appcore.ProviderSaveResult{}, s.refreshErr
	}
	return appcore.ProviderSaveResult{
		Provider: appcore.ProviderView{ID: in.ProviderID, Groups: []appcore.ProviderGroupView{{ID: in.GroupID, Protocol: "openai"}}},
	}, nil
}

func (s *groupSpyService) PingProviderGroupBaseURL(ctx context.Context, in appcore.ProviderGroupPingInput) (appcore.ProviderPingResult, error) {
	_ = ctx
	s.pingGroupIn = in
	if s.pingGroupErr != nil {
		return appcore.ProviderPingResult{}, s.pingGroupErr
	}
	return appcore.ProviderPingResult{
		ID:         in.ProviderID,
		BaseURL:    in.BaseURL,
		LatencyMs:  9,
		Reachable:  true,
		StatusCode: http.StatusOK,
	}, nil
}

func (s *groupSpyService) BindAliasTarget(ctx context.Context, in appcore.AliasTargetInput) (appcore.AliasView, error) {
	_ = ctx
	s.bindIn = in
	return appcore.AliasView{
		Alias: in.Alias,
		Targets: []appcore.AliasTargetView{{
			Provider: in.Provider,
			Group:    in.Group,
			Model:    in.Model,
			Enabled:  !in.Disabled,
		}},
	}, nil
}

func (s *groupSpyService) ReorderAliasTargets(ctx context.Context, in appcore.AliasTargetReorderInput) (appcore.AliasView, error) {
	_ = ctx
	s.reorderIn = in
	targets := make([]appcore.AliasTargetView, 0, len(in.Targets))
	for _, ref := range in.Targets {
		targets = append(targets, appcore.AliasTargetView{
			Provider: ref.Provider,
			Group:    ref.Group,
			Model:    ref.Model,
		})
	}
	return appcore.AliasView{Alias: in.Alias, Targets: targets}, nil
}
