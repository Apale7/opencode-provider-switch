package cli

import (
	"fmt"
	"sort"
	"strings"
)

func parseProviderModelRef(value string) (provider string, model string, ok bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "", false
	}
	provider, model, ok = strings.Cut(trimmed, "/")
	if !ok {
		return "", "", false
	}
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" || strings.Contains(provider, "/") {
		return "", "", false
	}
	return provider, model, true
}

func availableProviderModelRefs(models []string, providerID string) []string {
	if len(models) == 0 {
		return nil
	}
	refs := make([]string, 0, len(models))
	for _, model := range models {
		refs = append(refs, providerID+"/"+model)
	}
	sort.Strings(refs)
	return refs
}

func validateProviderModelKnown(providerID string, known []string, source string, model string) error {
	if source != "discovered" || len(known) == 0 {
		return nil
	}
	for _, item := range known {
		if item == model {
			return nil
		}
	}
	choices := availableProviderModelRefs(known, providerID)
	return fmt.Errorf("model %q is not in provider %q discovered models; available: %s", model, providerID, strings.Join(choices, ", "))
}
