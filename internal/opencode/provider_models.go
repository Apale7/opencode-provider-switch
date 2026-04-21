package opencode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type ModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func FetchProviderModels(protocol, baseURL, apiKey string, headers map[string]string) ([]string, error) {
	req, err := newProviderModelsRequest(protocol, baseURL, apiKey, headers)
	if err != nil {
		return nil, err
	}
	resp, body, err := DoJSON(req.Context(), req, TransportOptions{MaxRetries: 0})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload ModelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", req.URL.String(), err)
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
