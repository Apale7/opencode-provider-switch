package opencode

import (
	"math"
	"testing"
)

func TestLookupModelPricingKnownModel(t *testing.T) {
	pricing, ok := LookupModelPricing("gpt-4o")
	if !ok {
		t.Fatal("expected known pricing for gpt-4o")
	}

	assertPricingField(t, pricing, "input", 0.0025)
	assertPricingField(t, pricing, "output", 0.01)
	assertPricingField(t, pricing, "cacheRead", 0.00125)
	assertPricingField(t, pricing, "cacheWrite", 0)
	if pricing.Currency != "USD" {
		t.Fatalf("ModelPricing.Currency = %q, want USD", pricing.Currency)
	}
	if pricing.Source != ModelCapabilityProbeSourceKnownDB {
		t.Fatalf("ModelPricing.Source = %q, want %q", pricing.Source, ModelCapabilityProbeSourceKnownDB)
	}
}

func TestLookupModelPricingUnknownModel(t *testing.T) {
	if pricing, ok := LookupModelPricing("definitely-not-a-known-model"); ok {
		t.Fatalf("expected unknown model to return false, got true with pricing %#v", pricing)
	}
}

func TestLookupModelPricingFieldsAreFiniteFloats(t *testing.T) {
	pricing, ok := LookupModelPricing("claude-3-5-haiku-20241022")
	if !ok {
		t.Fatal("expected known pricing for claude-3-5-haiku-20241022")
	}

	for _, fieldName := range []string{"input", "output", "cacheRead", "cacheWrite"} {
		value := pricingField(pricing, fieldName)
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			t.Fatalf("ModelPricing.%s = %v, want finite non-negative float", fieldName, value)
		}
	}
}

func assertPricingField(t *testing.T, pricing ModelPricing, fieldName string, want float64) {
	t.Helper()

	if got := pricingField(pricing, fieldName); got != want {
		t.Fatalf("ModelPricing.%s = %v, want %v", fieldName, got, want)
	}
}

func pricingField(pricing ModelPricing, fieldName string) float64 {
	switch fieldName {
	case "input":
		return pricing.InputPer1K
	case "output":
		return pricing.OutputPer1K
	case "cacheRead":
		return pricing.CacheReadPer1K
	case "cacheWrite":
		return pricing.CacheWritePer1K
	default:
		return math.NaN()
	}
}
