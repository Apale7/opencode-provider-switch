package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type ModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ProviderBaseURLProbe struct {
	BaseURL    string `json:"baseUrl"`
	LatencyMs  int64  `json:"latencyMs"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProviderGroupModelsInput is the precise (provider, group) contract for model
// discovery and Base URL ping. Callers must supply one Group's protocol and keys
// together with the Provider's shared BaseURLs and headers.
//
// Callers must supply the exact GroupID and must never select the first group or
// a same-protocol sibling as a fallback.
type ProviderGroupModelsInput struct {
	ProviderID string
	GroupID    string
	Protocol   string
	BaseURLs   []string
	APIKeys    []string
	Headers    map[string]string
}

// NormalizeProviderGroupModelsInput trims fields without inventing identity.
func NormalizeProviderGroupModelsInput(in ProviderGroupModelsInput) ProviderGroupModelsInput {
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.GroupID = strings.TrimSpace(in.GroupID)
	in.Protocol = strings.TrimSpace(in.Protocol)
	urls := make([]string, 0, len(in.BaseURLs))
	for _, item := range in.BaseURLs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	in.BaseURLs = urls
	in.APIKeys = append([]string(nil), in.APIKeys...)
	if len(in.Headers) > 0 {
		headers := make(map[string]string, len(in.Headers))
		for k, v := range in.Headers {
			headers[k] = v
		}
		in.Headers = headers
	}
	return in
}

// ProbeTarget projects the group-scoped input into a capability probe target.
func (in ProviderGroupModelsInput) ProbeTarget() ProviderModelProbeTarget {
	normalized := NormalizeProviderGroupModelsInput(in)
	return ProviderModelProbeTarget{
		ProviderID: normalized.ProviderID,
		GroupID:    normalized.GroupID,
		Protocol:   normalized.Protocol,
		BaseURLs:   append([]string(nil), normalized.BaseURLs...),
		APIKeys:    append([]string(nil), normalized.APIKeys...),
		Headers:    normalized.Headers,
	}
}

// FetchProviderGroupModels discovers models for one exact provider group.
// Only the supplied group API keys are tried; sibling group keys are never used.
func FetchProviderGroupModels(ctx context.Context, in ProviderGroupModelsInput) ([]string, *ProviderBaseURLProbe, error) {
	normalized := NormalizeProviderGroupModelsInput(in)
	if normalized.GroupID == "" {
		return nil, nil, fmt.Errorf("provider %q group id is required", normalized.ProviderID)
	}
	if normalized.Protocol == "" {
		return nil, nil, fmt.Errorf("provider %q group %q protocol is required", normalized.ProviderID, normalized.GroupID)
	}
	if len(normalized.BaseURLs) == 0 {
		return nil, nil, fmt.Errorf("provider %q group %q missing base_url", normalized.ProviderID, normalized.GroupID)
	}
	return FetchProviderModelsWithAuthFallbackCtx(ctx, normalized.Protocol, normalized.BaseURLs, normalized.APIKeys, normalized.Headers)
}

// ProbeProviderGroupBaseURL pings one Base URL using one exact group's protocol and keys.
func ProbeProviderGroupBaseURL(ctx context.Context, in ProviderGroupModelsInput, baseURL string) (*ProviderBaseURLProbe, error) {
	normalized := NormalizeProviderGroupModelsInput(in)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("baseUrl is required")
	}
	if normalized.Protocol == "" {
		return nil, fmt.Errorf("provider %q group %q protocol is required", normalized.ProviderID, normalized.GroupID)
	}
	return ProbeProviderBaseURLWithAuthFallback(ctx, normalized.Protocol, baseURL, normalized.APIKeys, normalized.Headers)
}

func FetchProviderModels(protocol, baseURL, apiKey string, headers map[string]string) ([]string, error) {
	models, _, err := FetchProviderModelsDetailed(context.Background(), protocol, baseURL, apiKey, headers)
	return models, err
}

func FetchProviderModelsDetailed(ctx context.Context, protocol, baseURL, apiKey string, headers map[string]string) ([]string, *ProviderBaseURLProbe, error) {
	startedAt := time.Now()
	req, err := newProviderModelsRequest(protocol, baseURL, apiKey, headers)
	if err != nil {
		return nil, &ProviderBaseURLProbe{BaseURL: strings.TrimSpace(baseURL), LatencyMs: time.Since(startedAt).Milliseconds(), Error: err.Error()}, err
	}
	resp, body, err := DoJSON(ctx, req, TransportOptions{MaxRetries: 0})
	probe := &ProviderBaseURLProbe{BaseURL: strings.TrimSpace(baseURL), LatencyMs: time.Since(startedAt).Milliseconds()}
	if err != nil {
		probe.Error = err.Error()
		if modelsErr, ok := err.(*ProviderModelsError); ok {
			probe.StatusCode = modelsErr.StatusCode
		}
		return nil, probe, err
	}
	defer resp.Body.Close()
	probe.Reachable = true
	probe.StatusCode = resp.StatusCode
	var payload ModelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		decodeErr := fmt.Errorf("decode %s: %w", req.URL.String(), err)
		probe.Error = decodeErr.Error()
		return nil, probe, decodeErr
	}
	models := make([]string, 0, len(payload.Data))
	seen := map[string]bool{}
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		models = append(models, id)
	}
	sort.Strings(models)
	return models, probe, nil
}

func FetchProviderModelsWithFallback(protocol string, baseURLs []string, apiKey string, headers map[string]string) ([]string, *ProviderBaseURLProbe, error) {
	return FetchProviderModelsWithAuthFallback(protocol, baseURLs, []string{apiKey}, headers)
}

func FetchProviderModelsWithAuthFallback(protocol string, baseURLs []string, apiKeys []string, headers map[string]string) ([]string, *ProviderBaseURLProbe, error) {
	return FetchProviderModelsWithAuthFallbackCtx(context.Background(), protocol, baseURLs, apiKeys, headers)
}

func FetchProviderModelsWithAuthFallbackCtx(ctx context.Context, protocol string, baseURLs []string, apiKeys []string, headers map[string]string) ([]string, *ProviderBaseURLProbe, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	urls := make([]string, 0, len(baseURLs))
	for _, item := range baseURLs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	if len(urls) == 0 {
		return nil, nil, fmt.Errorf("missing base_url")
	}
	keys := normalizeAPIKeys(apiKeys)
	var lastProbe *ProviderBaseURLProbe
	var lastErr error
	var reachableProbe *ProviderBaseURLProbe
	for _, baseURL := range urls {
		for _, apiKey := range keys {
			models, probe, err := FetchProviderModelsDetailed(ctx, protocol, baseURL, apiKey, headers)
			if err == nil {
				if len(models) > 0 {
					return models, probe, nil
				}
				if reachableProbe == nil {
					reachableProbe = probe
				}
				continue
			}
			lastProbe = probe
			lastErr = err
		}
	}
	if reachableProbe != nil {
		return []string{}, reachableProbe, nil
	}
	return nil, lastProbe, lastErr
}

func normalizeAPIKeys(apiKeys []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(apiKeys))
	for _, apiKey := range apiKeys {
		trimmed := strings.TrimSpace(apiKey)
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func ProbeProviderBaseURL(ctx context.Context, protocol, baseURL, apiKey string, headers map[string]string) (*ProviderBaseURLProbe, error) {
	_, probe, err := FetchProviderModelsDetailed(ctx, protocol, baseURL, apiKey, headers)
	return probe, err
}

func ProbeProviderBaseURLWithAuthFallback(ctx context.Context, protocol, baseURL string, apiKeys []string, headers map[string]string) (*ProviderBaseURLProbe, error) {
	var lastProbe *ProviderBaseURLProbe
	var lastErr error
	for _, apiKey := range normalizeAPIKeys(apiKeys) {
		_, probe, err := FetchProviderModelsDetailed(ctx, protocol, baseURL, apiKey, headers)
		if err == nil || (probe != nil && probe.Reachable) {
			return probe, err
		}
		lastProbe = probe
		lastErr = err
	}
	return lastProbe, lastErr
}
