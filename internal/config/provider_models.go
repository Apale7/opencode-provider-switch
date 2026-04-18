package config

import (
	"slices"
	"sort"
	"strings"
)

func NormalizeProviderModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(model)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return slices.Clip(out)
}
