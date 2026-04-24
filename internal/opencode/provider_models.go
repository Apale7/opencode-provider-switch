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
	BaseURL     string `json:"baseUrl"`
	LatencyMs   int64  `json:"latencyMs"`
	Reachable   bool   `json:"reachable"`
	StatusCode  int    `json:"statusCode,omitempty"`
	Error       string `json:"error,omitempty"`
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
	urls := make([]string, 0, len(baseURLs))
	for _, item := range baseURLs {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			urls = append(urls, trimmed)
		}
	}
	if len(urls) == 0 {
		return nil, nil, fmt.Errorf("missing base_url")
	}
	var lastProbe *ProviderBaseURLProbe
	var lastErr error
	var reachableProbe *ProviderBaseURLProbe
	for _, baseURL := range urls {
		models, probe, err := FetchProviderModelsDetailed(context.Background(), protocol, baseURL, apiKey, headers)
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
	if reachableProbe != nil {
		return []string{}, reachableProbe, nil
	}
	return nil, lastProbe, lastErr
}

func ProbeProviderBaseURL(ctx context.Context, protocol, baseURL, apiKey string, headers map[string]string) (*ProviderBaseURLProbe, error) {
	_, probe, err := FetchProviderModelsDetailed(ctx, protocol, baseURL, apiKey, headers)
	return probe, err
}
