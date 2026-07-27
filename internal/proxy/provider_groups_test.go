package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/opencode"
)

func TestProviderGroups_AliasDualGroupOrder(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seenKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seenKeys = append(seenKeys, key)
		n := len(seenKeys)
		mu.Unlock()
		// Ordinary rate limits do not advance the key; the next alias target is tried.
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-dual","output":[]}`))
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, "alias_dual_group_order.json", upstream.URL+"/v1")
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/dual-order")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	mu.Lock()
	got := slices.Clone(seenKeys)
	mu.Unlock()
	premiumPool := []string{"sk-fake-premium-primary", "sk-fake-premium-backup"}
	standardPool := []string{"sk-fake-standard-primary", "sk-fake-standard-backup"}
	if len(got) != 2 {
		t.Fatalf("upstream attempts = %d, want 2 (premium then standard): %#v", len(got), got)
	}
	assertGroupKeyPhase(t, got[:1], premiumPool, "premium")
	assertGroupKeyPhase(t, got[1:], standardPool, "standard")
	if got := rr.Header().Get("X-OCSWITCH-Provider"); got != "vendor-a" {
		t.Fatalf("X-OCSWITCH-Provider = %q, want vendor-a", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Group"); got != "standard" {
		t.Fatalf("X-OCSWITCH-Group = %q, want standard", got)
	}
	if got := rr.Header().Get("X-OCSWITCH-Remote-Model"); got != "model-shared" {
		t.Fatalf("X-OCSWITCH-Remote-Model = %q, want model-shared", got)
	}
	traces, err := srv.traces.QueryAll(context.Background(), TraceQuery{})
	if err != nil {
		t.Fatalf("QueryAll() error = %v", err)
	}
	if len(traces) != 1 || traces[0].FinalGroup != "standard" || len(traces[0].Attempts) != 2 || traces[0].Attempts[0].Group != "premium" || traces[0].Attempts[1].Group != "standard" {
		t.Fatalf("group trace = %#v", traces)
	}
}

func TestProviderGroups_SameModelAliasTargetsOnly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seenKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seenKeys = append(seenKeys, key)
		n := len(seenKeys)
		mu.Unlock()
		// Ordinary rate limits keep the first beta key; alpha is tried next.
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-same","output":[]}`))
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, "same_model_alias_targets_only.json", upstream.URL+"/v1")
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/shared-only-targets")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	mu.Lock()
	got := slices.Clone(seenKeys)
	mu.Unlock()
	betaPool := []string{"sk-fake-beta-1", "sk-fake-beta-2"}
	alphaPool := []string{"sk-fake-alpha-1", "sk-fake-alpha-2"}
	if len(got) != 2 {
		t.Fatalf("upstream attempts = %d, want 2 (beta then alpha): %#v", len(got), got)
	}
	// Candidate order is beta then alpha; only listed targets.
	assertGroupKeyPhase(t, got[:1], betaPool, "beta")
	assertGroupKeyPhase(t, got[1:], alphaPool, "alpha")
	for _, key := range got {
		if strings.Contains(key, "gamma") {
			t.Fatalf("unlisted sibling gamma key used: %#v", got)
		}
	}
}

// assertGroupKeyPhase checks that attempts stay inside one group's key pool.
// Every attempted key must belong to the target group's configured pool.
func assertGroupKeyPhase(t *testing.T, got []string, pool []string, groupName string) {
	t.Helper()
	allowed := map[string]bool{}
	for _, key := range pool {
		allowed[key] = true
	}
	seen := map[string]int{}
	for _, key := range got {
		if !allowed[key] {
			t.Fatalf("%s phase key %q not in pool %#v; attempts=%#v", groupName, key, pool, got)
		}
		seen[key]++
	}
	if len(got) == len(pool) {
		for _, key := range pool {
			if seen[key] != 1 {
				t.Fatalf("%s phase must try each pool key once; got %#v pool %#v", groupName, got, pool)
			}
		}
	}
}

func TestProviderGroups_ProtocolAuthHeaders(t *testing.T) {
	t.Parallel()

	var openaiAuth, openaiShared string
	var anthropicKey, anthropicVersion, anthropicShared string
	var openaiPath, anthropicPath string
	var openaiHits, anthropicHits atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/responses"):
			openaiHits.Add(1)
			openaiPath = r.URL.Path
			openaiAuth = r.Header.Get("Authorization")
			openaiShared = r.Header.Get("X-Shared-Client")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp-openai","output":[]}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			anthropicHits.Add(1)
			anthropicPath = r.URL.Path
			anthropicKey = r.Header.Get("X-Api-Key")
			anthropicVersion = r.Header.Get("Anthropic-Version")
			anthropicShared = r.Header.Get("X-Shared-Client")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"msg-anthropic","type":"message","role":"assistant","content":[]}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, "protocol_auth_headers.json", upstream.URL+"/v1")
	srv := New(cfg)

	rrOpenAI := postOpenAIResponses(t, srv, "ocswitch/auth-openai")
	if rrOpenAI.Code != http.StatusOK {
		t.Fatalf("openai status = %d body=%s", rrOpenAI.Code, rrOpenAI.Body.String())
	}
	if openaiHits.Load() != 1 {
		t.Fatalf("openai hits = %d, want 1", openaiHits.Load())
	}
	if openaiAuth != "Bearer sk-fake-openai-group" {
		t.Fatalf("openai Authorization = %q", openaiAuth)
	}
	if openaiShared != "multi-proto-shared" {
		t.Fatalf("openai shared header = %q", openaiShared)
	}
	if !strings.HasSuffix(openaiPath, "/responses") {
		t.Fatalf("openai path = %q, want .../responses", openaiPath)
	}

	rrAnthropic := postAnthropicMessages(t, srv, "ocswitch/auth-anthropic")
	if rrAnthropic.Code != http.StatusOK {
		t.Fatalf("anthropic status = %d body=%s", rrAnthropic.Code, rrAnthropic.Body.String())
	}
	if anthropicHits.Load() != 1 {
		t.Fatalf("anthropic hits = %d, want 1", anthropicHits.Load())
	}
	if anthropicKey != "sk-fake-anthropic-group" {
		t.Fatalf("anthropic X-Api-Key = %q", anthropicKey)
	}
	if anthropicVersion != "2023-06-01" {
		t.Fatalf("Anthropic-Version = %q", anthropicVersion)
	}
	if anthropicShared != "multi-proto-shared" {
		t.Fatalf("anthropic shared header = %q", anthropicShared)
	}
	if !strings.HasSuffix(anthropicPath, "/messages") {
		t.Fatalf("anthropic path = %q, want .../messages", anthropicPath)
	}
}

func TestProviderGroups_KeyPoolRetryIsolation(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seenKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seenKeys = append(seenKeys, key)
		n := len(seenKeys)
		mu.Unlock()
		// Non-quota failures use both base URLs with the same first key, then pool-b.
		if n <= 2 {
			status := http.StatusTooManyRequests
			switch n {
			case 1:
				status = http.StatusUnauthorized
			case 2:
				status = http.StatusTooManyRequests
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"message":"fail-%d"}}`, n)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-keys","output":[]}`))
	}))
	defer upstream.Close()

	// Dual base URL: primary + backup rewritten to same upstream for isolation assertions.
	cfg := loadProviderGroupFixtureWithBaseURLs(t, "group_key_pool_retry_isolation.json", []string{
		upstream.URL + "/v1",
		upstream.URL + "/v1-backup",
	})
	// 401/429 are retryable by default; also treat 5xx as retryable for this fixture.
	cfg.Server.FailoverStatusCodes = []int{401, 402, 403, 429, 500, 502, 503}
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/key-pool-iso")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	mu.Lock()
	got := slices.Clone(seenKeys)
	mu.Unlock()
	if len(got) < 3 {
		t.Fatalf("upstream attempts = %d, want at least 3: %#v", len(got), got)
	}
	// Both base URLs must reuse pool-a's first configured key.
	for i := 0; i < 2; i++ {
		if got[i] != "sk-fake-pool-a-1" {
			t.Fatalf("attempt %d key = %q, want pool-a-1; full=%#v", i+1, got[i], got)
		}
	}
	// Next target starts pool-b and must not re-enter pool-a.
	if !strings.HasPrefix(got[2], "sk-fake-pool-b-") {
		t.Fatalf("attempt 3 key = %q, want pool-b; full=%#v", got[2], got)
	}
	for _, key := range got[2:] {
		if strings.HasPrefix(key, "sk-fake-pool-a-") {
			t.Fatalf("pool-a key leaked after failover: %#v", got)
		}
	}
}

func TestProviderGroups_SiblingGroupNotListed(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seenKeys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		seenKeys = append(seenKeys, key)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, "sibling_group_not_listed.json", upstream.URL+"/v1")
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/no-sibling")
	if rr.Code != http.StatusTooManyRequests && rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	mu.Lock()
	got := slices.Clone(seenKeys)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("expected listed group keys to be attempted")
	}
	for _, key := range got {
		if strings.Contains(key, "unlisted") {
			t.Fatalf("unlisted sibling group attempted: %#v", got)
		}
		if !strings.HasPrefix(key, "sk-fake-listed-") {
			t.Fatalf("unexpected key %q; full=%#v", key, got)
		}
	}
}

func TestProviderGroups_MissingGroupNoFallback(t *testing.T) {
	t.Parallel()
	assertProviderGroupNoUpstream(t, "missing_group_no_fallback.json", "ocswitch/missing-group", "missing-group", "no_available_target", `alias "missing-group" has no available targets`)
}

func TestProviderGroups_DisabledGroupNoFallback(t *testing.T) {
	t.Parallel()
	assertProviderGroupNoUpstream(t, "disabled_group_no_fallback.json", "ocswitch/disabled-group", "disabled-group", "no_available_target", `alias "disabled-group" has no available targets`)
}

func TestProviderGroups_ProtocolMismatchNoFallback(t *testing.T) {
	t.Parallel()
	assertProviderGroupNoUpstream(t, "protocol_mismatch_no_fallback.json", "ocswitch/proto-mismatch", "proto-mismatch", "no_available_target", `alias "proto-mismatch" has no available targets`)
}

func TestProviderGroups_ModelNotFoundNoDirect(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatal("direct candidate generation from group models is forbidden")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, "model_not_found_no_direct.json", upstream.URL+"/v1")
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/not-an-alias")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if hitCount.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hitCount.Load())
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "not-an-alias" not found`)
	assertLocalTrace(t, srv, "not-an-alias", http.StatusNotFound, "alias_missing", `alias "not-an-alias" not found`)
}

func TestProviderAPIKeyOptionsGroupIsolation(t *testing.T) {
	t.Parallel()

	groupA := &config.ProviderGroup{
		ID:      "pool-a",
		APIKey:  "sk-a-1",
		APIKeys: []string{"sk-a-2"},
	}
	groupB := &config.ProviderGroup{
		ID:      "pool-b",
		APIKey:  "sk-b-1",
		APIKeys: []string{"sk-b-2", "sk-b-3"},
	}

	keysA := providerAPIKeyOptions(groupA)
	keysB := providerAPIKeyOptions(groupB)
	if len(keysA) != 2 || keysA[0].Value != "sk-a-1" || keysA[1].Value != "sk-a-2" {
		t.Fatalf("group A keys = %#v", keysA)
	}
	if len(keysB) != 3 || keysB[0].Value != "sk-b-1" {
		t.Fatalf("group B keys = %#v", keysB)
	}
	for _, item := range keysA {
		if strings.HasPrefix(item.Value, "sk-b-") {
			t.Fatalf("group A leaked group B key: %#v", keysA)
		}
	}
	if len(providerAPIKeyOptions(nil)) != 1 || providerAPIKeyOptions(nil)[0].Value != "" {
		t.Fatalf("nil group should yield empty key option")
	}
}

func TestIsQuotaExhaustedResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"five hour", `{"error":{"message":"5h limit reached"}}`, true},
		{"five hour long", `{"error":{"message":"5-hour limit reached"}}`, true},
		{"weekly", `{"error":{"message":"weekly limit reached"}}`, true},
		{"structured quota", `{"error":{"code":"insufficient_quota"}}`, true},
		{"billing hard limit", `{"error":{"code":"billing_hard_limit_reached"}}`, true},
		{"credit balance", `{"error":{"message":"credit balance is too low"}}`, true},
		{"insufficient balance", `{"error":{"message":"insufficient balance"}}`, true},
		{"chinese balance", `{"error":{"message":"余额不足"}}`, true},
		{"invalid key", `{"error":{"code":"invalid_api_key"}}`, false},
		{"plain rate limit", `{"error":{"message":"rate limit"}}`, false},
		{"server error", `{"error":{"message":"internal failure"}}`, false},
		{"transport text", `transport error`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isQuotaExhaustedResponse([]byte(tt.body)); got != tt.want {
				t.Fatalf("isQuotaExhaustedResponse(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestProviderGroupQuotaExhaustionOnHTTP400AdvancesKey(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if len(seen) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-first", APIKeys: []string{"sk-second"}}},
		}},
		Aliases: []config.Alias{{Alias: "quota-400", Enabled: true, Targets: []config.Target{{Provider: "p1", Group: config.DefaultGroupID, Model: "up-1", Enabled: true}}}},
	}
	rr := postOpenAIResponses(t, New(cfg), "ocswitch/quota-400")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !slices.Equal(seen, []string{"Bearer sk-first", "Bearer sk-second"}) {
		t.Fatalf("seen auth headers = %#v", seen)
	}
}

func TestProviderGroupInvalidAPIKeyHTTP400DoesNotAdvanceKey(t *testing.T) {
	var seen []string
	const responseBody = `{"error":{"code":"invalid_api_key"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	cfg := &config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID: "p1", BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-first", APIKeys: []string{"sk-second"}}},
		}},
		Aliases: []config.Alias{{Alias: "invalid-key-400", Enabled: true, Targets: []config.Target{{Provider: "p1", Group: config.DefaultGroupID, Model: "up-1", Enabled: true}}}},
	}
	rr := postOpenAIResponses(t, New(cfg), "ocswitch/invalid-key-400")
	if rr.Code != http.StatusBadRequest || rr.Body.String() != responseBody {
		t.Fatalf("response = (%d, %q), want (%d, %q)", rr.Code, rr.Body.String(), http.StatusBadRequest, responseBody)
	}
	if !slices.Equal(seen, []string{"Bearer sk-first"}) {
		t.Fatalf("seen auth headers = %#v", seen)
	}
}

func TestResolveTargetGroupID_EmptyFailsClosed(t *testing.T) {
	t.Parallel()

	if got := resolveTargetGroupID(""); got != "" {
		t.Fatalf("resolveTargetGroupID(\"\") = %q, want empty (no default fallback)", got)
	}
	if got := resolveTargetGroupID("   "); got != "" {
		t.Fatalf("resolveTargetGroupID(whitespace) = %q, want empty", got)
	}
	if got := resolveTargetGroupID("premium"); got != "premium" {
		t.Fatalf("resolveTargetGroupID(premium) = %q, want premium", got)
	}
	if got := resolveTargetGroupID("  standard  "); got != "standard" {
		t.Fatalf("resolveTargetGroupID(padded) = %q, want standard", got)
	}
	if got := resolveTargetGroupID(config.DefaultGroupID); got != config.DefaultGroupID {
		t.Fatalf("resolveTargetGroupID(default) = %q, want %q", got, config.DefaultGroupID)
	}
}

func TestProviderBaseURLLatencyCache_IsolatesGroups(t *testing.T) {
	t.Parallel()

	cache := newProviderBaseURLLatencyCache(time.Minute)
	baseURL := "https://shared.example/v1"
	cache.put("vendor-a", "premium", baseURL, &opencode.ProviderBaseURLProbe{
		BaseURL: baseURL, Reachable: true, LatencyMs: 10,
	})
	cache.put("vendor-a", "standard", baseURL, &opencode.ProviderBaseURLProbe{
		BaseURL: baseURL, Reachable: true, LatencyMs: 50,
	})

	premium, okPremium := cache.get("vendor-a", "premium", baseURL)
	if !okPremium || premium.latencyMs != 10 {
		t.Fatalf("premium sample = %+v ok=%v, want latency 10", premium, okPremium)
	}
	standard, okStandard := cache.get("vendor-a", "standard", baseURL)
	if !okStandard || standard.latencyMs != 50 {
		t.Fatalf("standard sample = %+v ok=%v, want latency 50", standard, okStandard)
	}
	if _, ok := cache.get("vendor-a", "other", baseURL); ok {
		t.Fatal("unrelated group must not share premium/standard probe cache")
	}
	if _, ok := cache.get("vendor-b", "premium", baseURL); ok {
		t.Fatal("different provider must not share probe cache")
	}
	// Key shape must include group: same provider+baseURL with different groups stay distinct.
	if premium.latencyMs == standard.latencyMs {
		t.Fatal("group-scoped cache entries must not collapse to a single shared sample")
	}
}

func TestProviderGroups_EmptyGroupDoesNotCallUpstream(t *testing.T) {
	t.Parallel()

	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatal("empty target group must not call upstream")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Intentionally use New (not newLegacyTestServer) so empty Group is not
	// rewritten to default — runtime must fail closed without probing.
	cfg := &config.Config{
		Server: config.Server{APIKey: config.DefaultLocalAPIKey},
		Providers: []config.Provider{{
			ID:      "vendor-a",
			BaseURL: upstream.URL + "/v1",
			Groups: []config.ProviderGroup{{
				ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses,
				APIKey: "sk-fake-default", Models: []string{"model-a"},
			}},
		}},
		Aliases: []config.Alias{{
			Alias: "empty-group", Protocol: config.ProtocolOpenAIResponses, Enabled: true,
			Targets: []config.Target{{
				Provider: "vendor-a", Group: "", Model: "model-a", Enabled: true,
			}},
		}},
	}
	if got := resolveTargetGroupID(cfg.Aliases[0].Targets[0].Group); got != "" {
		t.Fatalf("precondition: resolveTargetGroupID empty = %q, want empty", got)
	}

	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, "ocswitch/empty-group")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if hitCount.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hitCount.Load())
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", `alias "empty-group" has no available targets`)
	assertLocalTrace(t, srv, "empty-group", http.StatusNotFound, "no_available_target", `alias "empty-group" has no available targets`)
}

func assertProviderGroupNoUpstream(t *testing.T, fixture, model, alias, wantCode, wantError string) {
	t.Helper()
	var hitCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitCount.Add(1)
		t.Fatalf("fixture %s must not call upstream", fixture)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cfg := loadProviderGroupFixture(t, fixture, upstream.URL+"/v1")
	srv := New(cfg)
	rr := postOpenAIResponses(t, srv, model)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if hitCount.Load() != 0 {
		t.Fatalf("upstream hits = %d, want 0", hitCount.Load())
	}
	assertOpenAIError(t, rr.Body.Bytes(), "model_not_found", "invalid_request_error", wantError)
	assertLocalTrace(t, srv, alias, http.StatusNotFound, wantCode, wantError)
}

func postOpenAIResponses(t *testing.T, srv *Server, model string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"stream":false}`, model)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.handleResponses(rr, req)
	return rr
}

func postAnthropicMessages(t *testing.T, srv *Server, model string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"max_tokens":16,"messages":[{"role":"user","content":"hi"}],"stream":false}`, model)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+config.DefaultLocalAPIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	rr := httptest.NewRecorder()
	srv.handleMessages(rr, req)
	return rr
}

func loadProviderGroupFixture(t *testing.T, name, baseURL string) *config.Config {
	t.Helper()
	return loadProviderGroupFixtureWithBaseURLs(t, name, []string{baseURL})
}

func loadProviderGroupFixtureWithBaseURLs(t *testing.T, name string, baseURLs []string) *config.Config {
	t.Helper()
	path := filepath.Join("testdata", "provider_groups", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	cfg, err := config.LoadFromBytes(path, raw)
	if err != nil {
		t.Fatalf("load fixture %s: %v", path, err)
	}
	if len(baseURLs) == 0 {
		t.Fatal("baseURLs required")
	}
	for i := range cfg.Providers {
		cfg.Providers[i].BaseURL = baseURLs[0]
		if len(baseURLs) > 1 {
			cfg.Providers[i].BaseURLs = append([]string(nil), baseURLs[1:]...)
		} else {
			cfg.Providers[i].BaseURLs = nil
		}
	}
	// Ensure fixture round-trip retained groups (sanity for schema v2 fixtures).
	var check map[string]any
	if err := json.Unmarshal(raw, &check); err != nil {
		t.Fatalf("unmarshal fixture meta: %v", err)
	}
	if len(cfg.Providers) == 0 || len(cfg.Providers[0].Groups) == 0 {
		t.Fatalf("fixture %s loaded without groups", name)
	}
	return cfg
}
