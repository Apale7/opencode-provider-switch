package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Apale7/opencode-provider-switch/internal/app"
	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/lifecycle"
)

func TestProviderGroupCommandSurface(t *testing.T) {
	group := newProviderGroupCmd()
	want := map[string]bool{"list": false, "create": false, "update": false, "delete": false, "refresh-models": false, "ping": false}
	for _, command := range group.Commands() {
		if _, ok := want[command.Name()]; ok {
			want[command.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("provider group subcommand %q missing", name)
		}
	}
}

func TestProviderGroupUpdateIDChangeFlagsAndExplicitSelections(t *testing.T) {
	cmd := newProviderGroupUpdateCmd()
	for _, name := range []string{"new-id", "yes", "on-protected", "on-rewrite", "rebind", "rebind-provider", "rebind-group", "rebind-model", "replace-provider-group", "dry-run"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("provider group update missing flag %q", name)
		}
	}

	// Required lifecycle choices remain unresolved until the caller selects them.
	plan := app.LifecyclePlanView{
		Choices: []lifecycle.Choice{
			{ID: "c-protected", Code: lifecycle.ReasonProtectedTarget},
			{ID: "c-rewrite", Code: lifecycle.ReasonSingletonRewrite},
		},
	}
	sels := buildGroupRemoveSelections(plan, "vendor", "premium", groupRemoveSelectionOpts{})
	if len(sels) != 0 {
		t.Fatalf("implicit selections = %#v, want none", sels)
	}
	sels = buildGroupRemoveSelections(plan, "vendor", "premium", groupRemoveSelectionOpts{
		OnProtected: lifecycle.OptionRemoveTarget,
		OnRewrite:   lifecycle.OptionKeepDormant,
	})
	byID := map[string]string{}
	for _, s := range sels {
		byID[s.ChoiceID] = s.OptionID
	}
	if byID["c-protected"] != lifecycle.OptionRemoveTarget {
		t.Fatalf("protected default = %q", byID["c-protected"])
	}
	if byID["c-rewrite"] != lifecycle.OptionKeepDormant {
		t.Fatalf("rewrite default = %q", byID["c-rewrite"])
	}
}

func TestApplyExplicitRebindOptsPreservesSlashModel(t *testing.T) {
	t.Parallel()
	opts, err := applyExplicitRebindOpts(groupRemoveSelectionOpts{}, "vendor", "default", "org/model/v2")
	if err != nil {
		t.Fatal(err)
	}
	if opts.RebindModel != "org/model/v2" || opts.OnProtected != lifecycle.OptionRebindTarget {
		t.Fatalf("opts = %#v", opts)
	}
}

func TestBuildGroupRemoveSelectionsNeverInfersChoiceParams(t *testing.T) {
	t.Parallel()
	plan := app.LifecyclePlanView{Choices: []lifecycle.Choice{
		{ID: "protected", Code: lifecycle.ReasonProtectedTarget, Params: map[string]any{"providerId": "vendor", "model": "org/model"}},
		{ID: "rewrite", Code: lifecycle.ReasonSingletonRewrite},
	}}
	selections := buildGroupRemoveSelections(plan, "vendor", "premium", groupRemoveSelectionOpts{
		OnProtected: lifecycle.OptionRebindTarget,
		OnRewrite:   lifecycle.OptionReplaceProviderGroups,
	})
	if len(selections) != 2 {
		t.Fatalf("selections = %#v", selections)
	}
	if selections[0].Params["providerId"] != "" || selections[0].Params["groupId"] != "" || selections[0].Params["model"] != "" {
		t.Fatalf("rebind params were inferred: %#v", selections[0].Params)
	}
	if len(selections[1].Params) != 0 {
		t.Fatalf("replacement params were inferred: %#v", selections[1].Params)
	}
}

func TestPreviewGroupIDChangeWithSelectionsExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium", Models: []string{"m1"}},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}
	prev := configPath
	configPath = path
	t.Cleanup(func() { configPath = prev })

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	_, plan, op, selections, err := previewGroupIDChangeWithSelections(cmd, "vendor", "premium", "gold", groupRemoveSelectionOpts{})
	if err != nil {
		t.Fatalf("previewGroupIDChangeWithSelections: %v", err)
	}
	if op.Kind != lifecycle.OpGroupIDChange {
		t.Fatalf("op kind = %q", op.Kind)
	}
	if !plan.Executable {
		t.Fatalf("plan not executable: blockers=%#v choices=%#v", plan.Blockers, plan.Choices)
	}
	// Current planner is deterministic (no choices); selections may be empty but must be forwarded when present.
	_ = selections
}

func TestProviderGroupUpdateNewIDAppliesViaLifecyclePreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocswitch.json")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg.UpsertProvider(config.Provider{
		ID:      "vendor",
		BaseURL: "https://example.com/v1",
		Groups: []config.ProviderGroup{
			{ID: config.DefaultGroupID, Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-default"},
			{ID: "premium", Name: "Premium", Protocol: config.ProtocolOpenAIResponses, APIKey: "sk-premium", Models: []string{"m1"}},
		},
	})
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}
	prev := configPath
	configPath = path
	t.Cleanup(func() { configPath = prev })

	var stdout bytes.Buffer
	cmd := newProviderGroupUpdateCmd()
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--provider", "vendor", "--group", "premium", "--new-id", "gold", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("provider group update --new-id: %v\nout=%s", err, stdout.String())
	}
	if out := stdout.String(); !strings.Contains(out, `group "gold"`) {
		t.Fatalf("output = %q", out)
	}
	reloaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	p := reloaded.FindProvider("vendor")
	if p == nil || p.FindGroup("premium") != nil || p.FindGroup("gold") == nil {
		t.Fatalf("groups after rename = %#v", p)
	}
}

func TestAliasBindDryRunPreservesExplicitGroup(t *testing.T) {
	cmd := newAliasBindCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--alias", "chat", "--provider", "vendor", "--model", "model", "--group", "premium", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("alias bind dry-run: %v", err)
	}
	if out := stdout.String(); !strings.Contains(out, "vendor/premium/model") {
		t.Fatalf("dry-run output = %q", out)
	}
}

func TestParseProviderGroupSelectorsDefaultsOnlyToDefault(t *testing.T) {
	selectors, err := parseProviderGroupSelectors([]string{"vendor", "vendor/premium"})
	if err != nil {
		t.Fatalf("parseProviderGroupSelectors() error = %v", err)
	}
	if len(selectors) != 2 || selectors[0].Group != "default" || selectors[1].Group != "premium" {
		t.Fatalf("selectors = %#v", selectors)
	}
}
