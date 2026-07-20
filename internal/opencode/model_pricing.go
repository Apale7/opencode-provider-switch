package opencode

import "strings"

// ModelPricing records known model pricing in USD per 1K tokens.
type ModelPricing struct {
	ModelID         string
	InputPer1K      float64
	OutputPer1K     float64
	CacheReadPer1K  float64
	CacheWritePer1K float64
	Currency        string
	Source          string
}

// LookupModelPricing returns known model pricing from the embedded known model database.
func LookupModelPricing(modelID string) (ModelPricing, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ModelPricing{}, false
	}

	capability, ok := lookupKnownModelCapability(modelID)
	if !ok {
		return ModelPricing{}, false
	}

	return ModelPricing{
		ModelID:         modelID,
		InputPer1K:      capability.Cost.Input,
		OutputPer1K:     capability.Cost.Output,
		CacheReadPer1K:  capability.Cost.CacheRead,
		CacheWritePer1K: capability.Cost.CacheWrite,
		Currency:        "USD",
		Source:          ModelCapabilityProbeSourceKnownDB,
	}, true
}
