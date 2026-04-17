package cli

import (
	"fmt"
	"sort"
	"strings"
)

func validateSyncedModelSelection(value string, aliases []string, flagName string) error {
	const prefix = "ocswitch/"
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("%s must use the ocswitch/<alias> form", flagName)
	}
	alias := strings.TrimPrefix(value, prefix)
	if alias == "" {
		return fmt.Errorf("%s must use the ocswitch/<alias> form", flagName)
	}
	for _, name := range aliases {
		if name == alias {
			return nil
		}
	}
	sorted := append([]string(nil), aliases...)
	sort.Strings(sorted)
	if len(sorted) == 0 {
		return fmt.Errorf("%s requires at least one routable alias; run ocswitch alias list or doctor first", flagName)
	}
	choices := make([]string, 0, len(sorted))
	for _, name := range sorted {
		choices = append(choices, prefix+name)
	}
	return fmt.Errorf("%s %q is not a routable alias; available: %s", flagName, value, strings.Join(choices, ", "))
}
