package opencode

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

type ModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func FetchProviderModels(protocol, baseURL, apiKey string, headers map[string]string) ([]string, error) {
	protocol = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
	url := strings.TrimRight(strings.TrimSpace(baseURL), "/") + config.ProtocolUpstreamModelsPath(protocol)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	config.ApplyProtocolAuthHeaders(req.Header, protocol, apiKey)
	config.ApplyProtocolDefaultHeaders(req.Header, protocol)
	for key, value := range headers {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body struct {
			Error any `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != nil {
			return nil, fmt.Errorf("request %s: unexpected status %s: %v", url, resp.Status, body.Error)
		}
		return nil, fmt.Errorf("request %s: unexpected status %s", url, resp.Status)
	}
	var payload ModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", url, err)
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
	return models, nil
}
