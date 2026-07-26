package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func newRewriteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rewrite",
		Short: "Manage outbound request config rewrite rules",
		Long: `Rewrite commands manage outbound request config changes stored in
local ocswitch config via Service/ConfigStore.

Rules match the incoming alias and optional selected target providers after alias
routing. New rules use --op with RFC 9535 JSONPath paths and op-layer mutation
semantics. Existing set/delete config is legacy, skipped at runtime, and must be
migrated to ops.`,
		Example: `  ocswitch rewrite add --name gpt-fast --alias gpt-5.5-fast --op set:$.service_tier=priority
  ocswitch rewrite add --name no-store --alias gpt-5.5 --provider provider-a --override --op set:$.store=false
  ocswitch rewrite add --name strip-tier --alias gpt-5.5-fast --override --op delete:$.service_tier
  ocswitch rewrite add --name include --alias gpt-5.5 --override --op append:$.include="reasoning.encrypted_content"
  ocswitch rewrite list`,
	}
	c.AddCommand(newRewriteAddCmd(), newRewriteListCmd(), newRewriteEnableCmd(), newRewriteDisableCmd(), newRewriteRemoveCmd())
	return c
}

func newRewriteAddCmd() *cobra.Command {
	var name, alias string
	var disabled, override bool
	var providers, providerGroups, opItems, setItems, deleteItems []string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create or update a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleName := strings.TrimSpace(name)
			if ruleName == "" {
				return fmt.Errorf("--name is required")
			}
			if len(setItems) > 0 || len(deleteItems) > 0 {
				return fmt.Errorf("--set/--delete are legacy and no longer create active rules; use --op instead")
			}
			ops, err := parseRewriteOpItems(opItems)
			if err != nil {
				return err
			}
			selectors, err := parseProviderGroupSelectors(providerGroups)
			if err != nil {
				return err
			}
			enabled := !disabled
			if !cmd.Flags().Changed("disabled") {
				// Preserve existing enabled state when updating without --disabled.
				rules, listErr := appService().ListRequestRewriteRules(cmd.Context())
				if listErr == nil {
					for _, rule := range rules {
						if rule.Name == ruleName {
							enabled = rule.Enabled
							break
						}
					}
				}
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would upsert rewrite rule %q via Service/ConfigStore\n", ruleName)
				return nil
			}
			in := app.RequestRewriteRuleInput{
				Name:     ruleName,
				Alias:    strings.TrimSpace(alias),
				Enabled:  enabled,
				Override: override,
				Ops:      ops,
			}
			if cmd.Flags().Changed("provider-group") {
				// Explicit selector path (including empty) — maps only declared pairs.
				in.ProviderGroups = selectors
			} else if len(providers) > 0 {
				// Legacy CLI flag is explicitly scoped to each provider's default group.
				in.ProviderGroups = make([]app.ProviderGroupSelectorInput, 0, len(providers))
				for _, provider := range providers {
					provider = strings.TrimSpace(provider)
					if provider != "" {
						in.ProviderGroups = append(in.ProviderGroups, app.ProviderGroupSelectorInput{Provider: provider, Group: config.DefaultGroupID})
					}
				}
			}
			view, err := appService().UpsertRequestRewriteRule(cmd.Context(), in)
			if err != nil {
				return err
			}
			state := "enabled"
			if !view.Enabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved rewrite rule %q [%s]\n", view.Name, state)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "rule name (required)")
	cmd.Flags().StringVar(&alias, "alias", "", "match incoming alias (required)")
	cmd.Flags().StringArrayVar(&providers, "provider", nil, "match selected target provider (repeatable; omit for all providers on alias)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save rule disabled")
	cmd.Flags().BoolVar(&override, "override", false, "allow replacing or deleting existing request fields")
	cmd.Flags().StringArrayVar(&opItems, "op", nil, "rewrite op (repeatable): set:$.path=JSON, delete:$.path, append:$.array=JSON, insert:$.array:INDEX=JSON")
	cmd.Flags().StringArrayVar(&providerGroups, "provider-group", nil, "match selected target provider/group (repeatable; omit for all groups on alias)")
	cmd.Flags().StringArrayVar(&setItems, "set", nil, "legacy inactive syntax; use --op set:$.path=VALUE")
	cmd.Flags().StringArrayVar(&deleteItems, "delete", nil, "legacy inactive syntax; use --op delete:$.path with --override")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func newRewriteListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List request rewrite rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			rules, err := appService().ListRequestRewriteRules(cmd.Context())
			if err != nil {
				return err
			}
			if len(rules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no rewrite rules)")
				return nil
			}
			for _, rule := range rules {
				state := "enabled"
				if !rule.Enabled {
					state = "disabled"
				}
				scope := rewriteRuleScopeLabel(rule)
				legacy := ""
				if rule.Legacy {
					legacy = " legacy=skipped"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%s] scope=%s override=%v ops=%s%s\n", rule.Name, state, scope, rule.Override, strings.Join(formatRewriteOps(rule.Ops), ","), legacy)
				for _, warning := range rule.Warnings {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
				}
			}
			return nil
		},
	}
}

func newRewriteEnableCmd() *cobra.Command {
	return newRewriteStateCmd("enable", true)
}

func newRewriteDisableCmd() *cobra.Command {
	return newRewriteStateCmd("disable", false)
}

func newRewriteStateCmd(use string, enabled bool) *cobra.Command {
	action := "enabled"
	if !enabled {
		action = "disabled"
	}
	var dryRun bool
	cmd := &cobra.Command{
		Use:   use + " <name>",
		Args:  cobra.ExactArgs(1),
		Short: rewriteStateShort(use) + " a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would set rewrite rule %q enabled=%v via Service/ConfigStore\n", name, enabled)
				return nil
			}
			if _, err := appService().SetRequestRewriteRuleEnabled(cmd.Context(), app.RequestRewriteRuleStateInput{
				Name:    name,
				Enabled: enabled,
			}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s rewrite rule %q\n", action, name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func rewriteStateShort(use string) string {
	if use == "enable" {
		return "Enable"
	}
	return "Disable"
}

func newRewriteRemoveCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Remove a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "dry-run: would remove rewrite rule %q via Service/ConfigStore\n", name)
				return nil
			}
			if _, err := appService().RemoveRequestRewriteRule(cmd.Context(), app.RequestRewriteRuleRemoveInput{Name: name}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed rewrite rule %q\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print action without persisting")
	return cmd
}

func parseRewriteOpItems(items []string) ([]config.RequestRewriteOperation, error) {
	if len(items) == 0 {
		return nil, nil
	}
	ops := make([]config.RequestRewriteOperation, 0, len(items))
	for _, item := range items {
		op, err := parseRewriteOpItem(item)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func parseRewriteOpItem(item string) (config.RequestRewriteOperation, error) {
	name, rest, ok := strings.Cut(item, ":")
	if !ok {
		return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want op:$.path or op:$.path=VALUE)", item)
	}
	opName := strings.ToLower(strings.TrimSpace(name))
	rest = strings.TrimSpace(rest)
	switch opName {
	case config.RequestRewriteOpSet, config.RequestRewriteOpAppend:
		path, rawValue, ok := splitRewriteOpValue(rest)
		if !ok || strings.TrimSpace(path) == "" {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want %s:$.path=VALUE)", item, opName)
		}
		return config.RequestRewriteOperation{Op: opName, Path: strings.TrimSpace(path), Value: parseRewriteValue(strings.TrimSpace(rawValue)), ValueSet: true}, nil
	case config.RequestRewriteOpDelete:
		if strings.Contains(rest, "=") || rest == "" {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want delete:$.path)", item)
		}
		return config.RequestRewriteOperation{Op: opName, Path: rest}, nil
	case config.RequestRewriteOpInsert:
		left, rawValue, ok := splitRewriteOpValue(rest)
		if !ok {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want insert:$.array:INDEX=VALUE)", item)
		}
		left = strings.TrimSpace(left)
		indexSeparator := lastRewriteSyntaxSeparator(left, ':')
		if indexSeparator < 0 {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want insert:$.array:INDEX=VALUE)", item)
		}
		path := strings.TrimSpace(left[:indexSeparator])
		rawIndex := strings.TrimSpace(left[indexSeparator+1:])
		if path == "" || rawIndex == "" {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (want insert:$.array:INDEX=VALUE)", item)
		}
		index, err := strconv.Atoi(rawIndex)
		if err != nil || index < 0 {
			return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (insert index must be >= 0)", item)
		}
		return config.RequestRewriteOperation{Op: opName, Path: path, Index: &index, Value: parseRewriteValue(strings.TrimSpace(rawValue)), ValueSet: true}, nil
	default:
		return config.RequestRewriteOperation{}, fmt.Errorf("invalid --op %q (unknown op %q)", item, opName)
	}
}

func splitRewriteOpValue(input string) (string, string, bool) {
	separator := firstRewriteSyntaxSeparator(input, '=')
	if separator < 0 {
		return "", "", false
	}
	return input[:separator], input[separator+1:], true
}

func firstRewriteSyntaxSeparator(input string, separator byte) int {
	for index := 0; index < len(input); index++ {
		switch input[index] {
		case '\'', '"':
			index = scanRewriteQuotedSpan(input, index)
		case separator:
			return index
		}
	}
	return -1
}

func lastRewriteSyntaxSeparator(input string, separator byte) int {
	match := -1
	for index := 0; index < len(input); index++ {
		switch input[index] {
		case '\'', '"':
			index = scanRewriteQuotedSpan(input, index)
		case separator:
			match = index
		}
	}
	return match
}

func scanRewriteQuotedSpan(input string, start int) int {
	quote := input[start]
	for index := start + 1; index < len(input); index++ {
		if input[index] == '\\' {
			index++
			continue
		}
		if input[index] == quote {
			return index
		}
	}
	return len(input)
}

func parseRewriteValue(value string) any {
	if value == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		return decoded
	}
	return value
}

func rewriteRuleScopeLabel(rule app.RequestRewriteRuleView) string {
	parts := []string{}
	if rule.Alias != "" {
		parts = append(parts, "alias="+rule.Alias)
	}
	if len(rule.ProviderGroups) > 0 {
		labels := make([]string, 0, len(rule.ProviderGroups))
		for _, sel := range rule.ProviderGroups {
			group := strings.TrimSpace(sel.Group)
			if group == "" {
				group = config.DefaultGroupID
			}
			labels = append(labels, sel.Provider+"/"+group)
		}
		parts = append(parts, "providerGroups="+strings.Join(labels, ","))
	} else if rule.ProviderGroups != nil {
		parts = append(parts, "providerGroups=*")
	}
	if len(parts) == 0 {
		return "(invalid)"
	}
	return strings.Join(parts, ",")
}

// parseProviderGroupSelectors parses --provider-group values as "provider" or "provider/group".
// Bare provider maps only to the default group (never siblings).
func parseProviderGroupSelectors(items []string) ([]app.ProviderGroupSelectorInput, error) {
	if items == nil {
		return nil, nil
	}
	out := make([]app.ProviderGroupSelectorInput, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		provider, group, ok := strings.Cut(item, "/")
		provider = strings.TrimSpace(provider)
		if provider == "" {
			return nil, fmt.Errorf("invalid --provider-group %q (want provider or provider/group)", item)
		}
		if !ok || strings.TrimSpace(group) == "" {
			group = config.DefaultGroupID
		} else {
			group = strings.TrimSpace(group)
		}
		key := provider + "\x00" + group
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, app.ProviderGroupSelectorInput{Provider: provider, Group: group})
	}
	// Preserve explicit empty as non-nil empty (wildcard).
	if len(items) == 0 {
		return []app.ProviderGroupSelectorInput{}, nil
	}
	return out, nil
}

func formatRewriteOps(ops []config.RequestRewriteOperation) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		if op.Index != nil {
			out = append(out, fmt.Sprintf("%s:%s:%d", op.Op, op.Path, *op.Index))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", op.Op, op.Path))
	}
	sort.Strings(out)
	return out
}
