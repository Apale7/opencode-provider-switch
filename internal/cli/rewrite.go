package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func newRewriteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rewrite",
		Short: "Manage outbound request config rewrite rules",
		Long: `Rewrite commands manage outbound request config changes stored in
local ocswitch config.

Rules match the incoming alias and optional selected target providers after alias
routing. New rules use --op with RFC 9535 JSONPath paths and op-layer mutation
semantics. Existing set/delete config is legacy, skipped at runtime, and must be
migrated to ops.`,
		Example: `  ocswitch rewrite add --name gpt-fast --alias gpt-5.5-fast --op set:$.serviceTier=priority
  ocswitch rewrite add --name no-store --alias gpt-5.5 --provider provider-a --override --op set:$.store=false
  ocswitch rewrite add --name strip-tier --alias gpt-5.5-fast --override --op delete:$.serviceTier
  ocswitch rewrite add --name include --alias gpt-5.5 --override --op append:$.include="reasoning.encrypted_content"
  ocswitch rewrite list`,
	}
	c.AddCommand(newRewriteAddCmd(), newRewriteListCmd(), newRewriteEnableCmd(), newRewriteDisableCmd(), newRewriteRemoveCmd())
	return c
}

func newRewriteAddCmd() *cobra.Command {
	var name, alias string
	var disabled, override bool
	var providers, opItems, setItems, deleteItems []string
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
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			enabled := !disabled
			if existing := cfg.FindRequestRewriteRule(ruleName); existing != nil && !cmd.Flags().Changed("disabled") {
				enabled = existing.Enabled
			}
			rule := config.RequestRewriteRule{
				Name:      ruleName,
				Alias:     strings.TrimSpace(alias),
				Providers: providers,
				Enabled:   enabled,
				Override:  override,
				Ops:       ops,
			}
			cfg.UpsertRequestRewriteRule(rule)
			if errs := cfg.Validate(); len(errs) > 0 {
				return errs[0]
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			state := "enabled"
			if !enabled {
				state = "disabled"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "saved rewrite rule %q [%s]\n", ruleName, state)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "rule name (required)")
	cmd.Flags().StringVar(&alias, "alias", "", "match incoming alias (required)")
	cmd.Flags().StringArrayVar(&providers, "provider", nil, "match selected target provider (repeatable; omit for all providers on alias)")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save rule disabled")
	cmd.Flags().BoolVar(&override, "override", false, "allow replacing or deleting existing request fields")
	cmd.Flags().StringArrayVar(&opItems, "op", nil, "rewrite op (repeatable): set:$.path=JSON, delete:$.path, append:$.array=JSON, insert:$.array:INDEX=JSON")
	cmd.Flags().StringArrayVar(&setItems, "set", nil, "legacy inactive syntax; use --op set:$.path=VALUE")
	cmd.Flags().StringArrayVar(&deleteItems, "delete", nil, "legacy inactive syntax; use --op delete:$.path with --override")
	return cmd
}

func newRewriteListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List request rewrite rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			rules := cfg.RequestRewriteRulesSnapshot()
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
				if config.RequestRewriteRuleUsesLegacySyntax(rule) {
					legacy = " legacy=skipped"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%s] scope=%s override=%v ops=%s%s\n", rule.Name, state, scope, rule.Override, strings.Join(formatRewriteOps(rule.Ops), ","), legacy)
				for _, warning := range config.RequestRewriteRuleWarnings(rule) {
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
	return &cobra.Command{
		Use:   use + " <name>",
		Args:  cobra.ExactArgs(1),
		Short: rewriteStateShort(use) + " a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if !cfg.SetRequestRewriteRuleEnabled(args[0], enabled) {
				return fmt.Errorf("rewrite rule %q not found", args[0])
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s rewrite rule %q\n", action, args[0])
			return nil
		},
	}
}

func rewriteStateShort(use string) string {
	if use == "enable" {
		return "Enable"
	}
	return "Disable"
}

func newRewriteRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Args:  cobra.ExactArgs(1),
		Short: "Remove a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadCfg()
			if err != nil {
				return err
			}
			if !cfg.RemoveRequestRewriteRule(args[0]) {
				return fmt.Errorf("rewrite rule %q not found", args[0])
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed rewrite rule %q\n", args[0])
			return nil
		},
	}
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

func rewriteRuleScopeLabel(rule config.RequestRewriteRule) string {
	parts := []string{}
	if rule.Alias != "" {
		parts = append(parts, "alias="+rule.Alias)
	}
	if len(rule.Providers) > 0 {
		parts = append(parts, "providers="+strings.Join(rule.Providers, ","))
	}
	if len(parts) == 0 {
		return "(invalid)"
	}
	return strings.Join(parts, ",")
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
