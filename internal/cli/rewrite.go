package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func newRewriteCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "rewrite",
		Short: "Manage outbound request config rewrite rules",
		Long: `Rewrite commands manage top-level outbound request config changes stored in
local ocswitch config.

Rules match the incoming alias and/or the resolved upstream model after alias
routing. By default --set only fills missing request fields so caller-supplied
values win. With --override, --set replaces existing request fields and --delete
removes top-level fields before forwarding upstream.`,
		Example: `  ocswitch rewrite add --name gpt-fast --alias gpt-5.5-fast --set serviceTier=priority
  ocswitch rewrite add --name no-store --model gpt-5.5 --override --set store=false
  ocswitch rewrite add --name strip-tier --alias gpt-5.5-fast --override --delete serviceTier
  ocswitch rewrite list`,
	}
	c.AddCommand(newRewriteAddCmd(), newRewriteListCmd(), newRewriteEnableCmd(), newRewriteDisableCmd(), newRewriteRemoveCmd())
	return c
}

func newRewriteAddCmd() *cobra.Command {
	var name, alias, model string
	var disabled, override bool
	var setItems, deleteItems []string
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create or update a request rewrite rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			ruleName := strings.TrimSpace(name)
			if ruleName == "" {
				return fmt.Errorf("--name is required")
			}
			set, err := parseRewriteSetItems(setItems)
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
				Name:     ruleName,
				Alias:    strings.TrimSpace(alias),
				Model:    strings.TrimSpace(model),
				Enabled:  enabled,
				Override: override,
				Set:      set,
				Delete:   deleteItems,
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
	cmd.Flags().StringVar(&alias, "alias", "", "match incoming alias")
	cmd.Flags().StringVar(&model, "model", "", "match resolved upstream model")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "save rule disabled")
	cmd.Flags().BoolVar(&override, "override", false, "allow replacing or deleting existing request fields")
	cmd.Flags().StringArrayVar(&setItems, "set", nil, "top-level request field KEY=VALUE (repeatable; VALUE may be JSON)")
	cmd.Flags().StringArrayVar(&deleteItems, "delete", nil, "top-level request field to remove when --override is set (repeatable)")
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
				fmt.Fprintf(cmd.OutOrStdout(), "%s  [%s] scope=%s override=%v set=%s delete=%s\n", rule.Name, state, scope, rule.Override, strings.Join(sortedRewriteSetKeys(rule.Set), ","), strings.Join(rule.Delete, ","))
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

func parseRewriteSetItems(items []string) (map[string]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(items))
	for _, item := range items {
		key, rawValue, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --set %q (want KEY=VALUE)", item)
		}
		out[key] = parseRewriteValue(strings.TrimSpace(rawValue))
	}
	return out, nil
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
	if rule.Model != "" {
		parts = append(parts, "model="+rule.Model)
	}
	if len(parts) == 0 {
		return "(invalid)"
	}
	return strings.Join(parts, ",")
}

func sortedRewriteSetKeys(set map[string]any) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
