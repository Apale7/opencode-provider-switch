package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultModelsDevURL     = "https://models.dev"
	modelsDevCacheTTL       = 5 * time.Minute
	modelsDevRequestTimeout = 10 * time.Second
)

type ModelsDevPricing struct {
	Input           *float64
	Output          *float64
	CacheRead       *float64
	CacheWrite      *float64
	ContextOver200K *ModelsDevPricingTier
}

type ModelsDevPricingTier struct {
	Input      *float64
	Output     *float64
	CacheRead  *float64
	CacheWrite *float64
}

type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Cost         *modelsDevCost         `json:"cost"`
	Experimental *modelsDevExperimental `json:"experimental"`
}

type modelsDevExperimental struct {
	Modes map[string]modelsDevExperimentalMode `json:"modes"`
}

type modelsDevExperimentalMode struct {
	Cost *modelsDevCost `json:"cost"`
}

type modelsDevCost struct {
	Input           *float64           `json:"input"`
	Output          *float64           `json:"output"`
	CacheRead       *float64           `json:"cache_read"`
	CacheWrite      *float64           `json:"cache_write"`
	ContextOver200K *modelsDevCostTier `json:"context_over_200k"`
}

type modelsDevCostTier struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

var cachedModelsDevPricing struct {
	mu       sync.Mutex
	cacheKey string
	loadedAt time.Time
	catalog  map[string]ModelsDevPricing
}

func LookupModelsDevPricing(ctx context.Context, candidates ...string) (ModelsDevPricing, bool, error) {
	catalog, err := loadModelsDevPricingCatalog(ctx)
	if err != nil || len(catalog) == 0 {
		return ModelsDevPricing{}, false, err
	}
	for _, candidate := range candidates {
		key := normalizeModelsDevLookupKey(candidate)
		if key == "" {
			continue
		}
		pricing, ok := catalog[key]
		if ok {
			return pricing, true, nil
		}
	}
	return ModelsDevPricing{}, false, nil
}

func loadModelsDevPricingCatalog(ctx context.Context) (map[string]ModelsDevPricing, error) {
	cacheKey := modelsDevEnvKey()
	cachedModelsDevPricing.mu.Lock()
	defer cachedModelsDevPricing.mu.Unlock()
	if cachedModelsDevPricing.cacheKey == cacheKey && time.Since(cachedModelsDevPricing.loadedAt) < modelsDevCacheTTL {
		return cloneModelsDevPricingCatalog(cachedModelsDevPricing.catalog), nil
	}
	catalog, err := readModelsDevPricingCatalog(ctx)
	if err != nil {
		return nil, err
	}
	cachedModelsDevPricing.cacheKey = cacheKey
	cachedModelsDevPricing.loadedAt = time.Now()
	cachedModelsDevPricing.catalog = cloneModelsDevPricingCatalog(catalog)
	return cloneModelsDevPricingCatalog(catalog), nil
}

func readModelsDevPricingCatalog(ctx context.Context) (map[string]ModelsDevPricing, error) {
	var data []byte
	if path := strings.TrimSpace(os.Getenv("OPENCODE_MODELS_PATH")); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read models.dev catalog %s: %w", path, err)
		}
		data = body
	} else {
		if envBoolEnabled("OPENCODE_DISABLE_MODELS_FETCH") {
			return nil, nil
		}
		if ctx == nil {
			ctx = context.Background()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevSourceURL()+"/api.json", nil)
		if err != nil {
			return nil, fmt.Errorf("build models.dev request: %w", err)
		}
		_, body, err := DoJSON(ctx, req, TransportOptions{RequestTimeout: modelsDevRequestTimeout, MaxRetries: 0})
		if err != nil {
			return nil, fmt.Errorf("fetch models.dev catalog: %w", err)
		}
		data = body
	}
	if len(data) == 0 {
		return nil, nil
	}
	raw := map[string]modelsDevProvider{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode models.dev catalog: %w", err)
	}
	return buildModelsDevPricingCatalog(raw), nil
}

func buildModelsDevPricingCatalog(raw map[string]modelsDevProvider) map[string]ModelsDevPricing {
	if len(raw) == 0 {
		return nil
	}
	catalog := map[string]ModelsDevPricing{}
	uniqueBare := map[string]ModelsDevPricing{}
	bareCounts := map[string]int{}
	for providerID, provider := range raw {
		for modelID, model := range provider.Models {
			pricing, ok := modelsDevPricingFromCost(model.Cost)
			if !ok {
				pricing = ModelsDevPricing{}
			}
			if ok {
				addModelsDevCatalogEntry(catalog, uniqueBare, bareCounts, providerID, modelID, pricing)
			}
			if model.Experimental != nil {
				for mode, experimental := range model.Experimental.Modes {
					merged, mergedOK := mergeModelsDevPricing(pricing, experimental.Cost)
					if !mergedOK {
						continue
					}
					addModelsDevCatalogEntry(catalog, uniqueBare, bareCounts, providerID, modelID+"-"+mode, merged)
				}
			}
		}
	}
	for key, pricing := range uniqueBare {
		if _, exists := catalog[key]; !exists {
			catalog[key] = pricing
		}
	}
	if len(catalog) == 0 {
		return nil
	}
	return catalog
}

func modelsDevPricingFromCost(cost *modelsDevCost) (ModelsDevPricing, bool) {
	if cost == nil {
		return ModelsDevPricing{}, false
	}
	pricing := ModelsDevPricing{
		Input:      cost.Input,
		Output:     cost.Output,
		CacheRead:  cost.CacheRead,
		CacheWrite: cost.CacheWrite,
	}
	if cost.ContextOver200K != nil {
		pricing.ContextOver200K = &ModelsDevPricingTier{
			Input:      cost.ContextOver200K.Input,
			Output:     cost.ContextOver200K.Output,
			CacheRead:  cost.ContextOver200K.CacheRead,
			CacheWrite: cost.ContextOver200K.CacheWrite,
		}
	}
	if pricing.Input == nil && pricing.Output == nil && pricing.CacheRead == nil && pricing.CacheWrite == nil && pricing.ContextOver200K == nil {
		return ModelsDevPricing{}, false
	}
	return pricing, true
}

func mergeModelsDevPricing(base ModelsDevPricing, override *modelsDevCost) (ModelsDevPricing, bool) {
	if override == nil {
		if base.Input == nil && base.Output == nil && base.CacheRead == nil && base.CacheWrite == nil && base.ContextOver200K == nil {
			return ModelsDevPricing{}, false
		}
		return cloneModelsDevPricing(base), true
	}
	merged := cloneModelsDevPricing(base)
	if override.Input != nil {
		merged.Input = cloneFloat64Ptr(override.Input)
	}
	if override.Output != nil {
		merged.Output = cloneFloat64Ptr(override.Output)
	}
	if override.CacheRead != nil {
		merged.CacheRead = cloneFloat64Ptr(override.CacheRead)
	}
	if override.CacheWrite != nil {
		merged.CacheWrite = cloneFloat64Ptr(override.CacheWrite)
	}
	if override.ContextOver200K != nil {
		merged.ContextOver200K = &ModelsDevPricingTier{
			Input:      cloneFloat64Ptr(override.ContextOver200K.Input),
			Output:     cloneFloat64Ptr(override.ContextOver200K.Output),
			CacheRead:  cloneFloat64Ptr(override.ContextOver200K.CacheRead),
			CacheWrite: cloneFloat64Ptr(override.ContextOver200K.CacheWrite),
		}
	}
	if merged.Input == nil && merged.Output == nil && merged.CacheRead == nil && merged.CacheWrite == nil && merged.ContextOver200K == nil {
		return ModelsDevPricing{}, false
	}
	return merged, true
}

func addModelsDevCatalogEntry(catalog map[string]ModelsDevPricing, uniqueBare map[string]ModelsDevPricing, bareCounts map[string]int, providerID string, modelID string, pricing ModelsDevPricing) {
	compositeKey := normalizeModelsDevLookupKey(providerID + "/" + modelID)
	if compositeKey != "" {
		catalog[compositeKey] = pricing
	}
	bareKey := normalizeModelsDevLookupKey(modelID)
	if bareKey == "" {
		return
	}
	if isOfficialModelsDevProvider(providerID) {
		if _, exists := catalog[bareKey]; !exists {
			catalog[bareKey] = pricing
		}
		return
	}
	bareCounts[bareKey]++
	if bareCounts[bareKey] == 1 {
		uniqueBare[bareKey] = pricing
		return
	}
	delete(uniqueBare, bareKey)
}

func modelsDevSourceURL() string {
	source := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENCODE_MODELS_URL")), "/")
	if source == "" {
		return defaultModelsDevURL
	}
	return source
}

func modelsDevEnvKey() string {
	return strings.Join([]string{
		strings.TrimSpace(os.Getenv("OPENCODE_MODELS_PATH")),
		modelsDevSourceURL(),
		strings.TrimSpace(os.Getenv("OPENCODE_DISABLE_MODELS_FETCH")),
	}, "|")
}

func envBoolEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeModelsDevLookupKey(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

func isOfficialModelsDevProvider(providerID string) bool {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai", "anthropic":
		return true
	default:
		return false
	}
}

func cloneModelsDevPricingCatalog(in map[string]ModelsDevPricing) map[string]ModelsDevPricing {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]ModelsDevPricing, len(in))
	for key, value := range in {
		out[key] = cloneModelsDevPricing(value)
	}
	return out
}

func cloneModelsDevPricing(in ModelsDevPricing) ModelsDevPricing {
	out := ModelsDevPricing{
		Input:      cloneFloat64Ptr(in.Input),
		Output:     cloneFloat64Ptr(in.Output),
		CacheRead:  cloneFloat64Ptr(in.CacheRead),
		CacheWrite: cloneFloat64Ptr(in.CacheWrite),
	}
	if in.ContextOver200K != nil {
		out.ContextOver200K = &ModelsDevPricingTier{
			Input:      cloneFloat64Ptr(in.ContextOver200K.Input),
			Output:     cloneFloat64Ptr(in.ContextOver200K.Output),
			CacheRead:  cloneFloat64Ptr(in.ContextOver200K.CacheRead),
			CacheWrite: cloneFloat64Ptr(in.ContextOver200K.CacheWrite),
		}
	}
	return out
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
