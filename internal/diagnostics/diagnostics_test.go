package diagnostics

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

func TestNormalizeJSONAndSensitiveParams(t *testing.T) {
	issue, err := Normalize(Issue{Code: CodeAliasDisabled, Severity: SeverityInfo, Path: "/config/aliases/0", Source: Source{"alias", "a", "/config/aliases/0"}, Reason: ReasonDisabled, Params: Params{"alias": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(issue)
	if string(raw) == "" || issue.AllowedActions == nil || issue.Params == nil {
		t.Fatalf("issue=%+v json=%s", issue, raw)
	}
	issue.Params["apiKey"] = "secret"
	if _, err := Normalize(issue); err == nil {
		t.Fatal("sensitive param accepted")
	}
	if got := EscapePathToken("a~/b"); got != "a~0~1b" {
		t.Fatalf("escape=%q", got)
	}
}

func TestSortAndDedupeIntersectsActions(t *testing.T) {
	base := Issue{SchemaVersion: 1, Code: CodeAliasDisabled, Severity: SeverityInfo, Path: "/config/aliases/0", Source: Source{"alias", "a", "/config/aliases/0"}, Reason: ReasonDisabled, Params: Params{"alias": "a"}, AllowedActions: []Action{ActionKeep, ActionEnableAlias}}
	other := base
	other.AllowedActions = []Action{ActionEnableAlias}
	out, err := SortAndDedupe([]Issue{base, other})
	if err != nil || len(out) != 1 || !reflect.DeepEqual(out[0].AllowedActions, []Action{ActionEnableAlias}) {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestNormalizeEnforcesRegistryAndCanonicalParams(t *testing.T) {
	base := Issue{SchemaVersion: 1, Code: CodeAliasDisabled, Severity: SeverityInfo, Path: "/config/aliases/0", Source: Source{"alias", "a", "/config/aliases/0"}, Reason: ReasonDisabled, Params: Params{"alias": "a", "occurrencePaths": []string(nil)}}
	normalized, err := Normalize(base)
	if err != nil {
		t.Fatal(err)
	}
	if paths := normalized.Params["occurrencePaths"].([]string); paths == nil || len(paths) != 0 {
		t.Fatalf("typed nil was not normalized: %#v", paths)
	}

	wrongSeverity := base
	wrongSeverity.Severity = SeverityError
	if _, err := Normalize(wrongSeverity); err == nil {
		t.Fatal("invalid code/severity combination accepted")
	}
	unknownAction := base
	unknownAction.AllowedActions = []Action{"execute_anything"}
	if _, err := Normalize(unknownAction); err == nil {
		t.Fatal("unknown action accepted")
	}
	sensitive := base
	sensitive.Params = Params{"alias": "a", "providerApiKey": "secret"}
	if _, err := Normalize(sensitive); err == nil {
		t.Fatal("embedded sensitive param key accepted")
	}
}

func TestNormalizeValidatesV1PathsKindsAndSafeValues(t *testing.T) {
	base := Issue{SchemaVersion: 1, Code: CodeAliasDisabled, Severity: SeverityInfo, Path: "/config/aliases/0", Source: Source{"alias", "a", "/config/aliases/0"}, Reason: ReasonDisabled, Params: Params{"alias": "a"}}
	tests := []struct {
		name   string
		mutate func(*Issue)
	}{
		{"filesystem path", func(issue *Issue) { issue.Path = `C:\\Users\\secret` }},
		{"unknown root", func(issue *Issue) { issue.Path = "/tmp/config" }},
		{"invalid escape", func(issue *Issue) { issue.Path = "/config/a~2b" }},
		{"unknown source kind", func(issue *Issue) { issue.Source.Kind = "database" }},
		{"URL param", func(issue *Issue) { issue.Params["endpoint"] = "https://user:secret@example.com/path?q=secret" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := base
			candidate.Params = Params{"alias": "a"}
			test.mutate(&candidate)
			if _, err := Normalize(candidate); err == nil {
				t.Fatal("invalid V1 issue accepted")
			}
		})
	}
	withEmpty := base
	withEmpty.Params = Params{"alias": "a", "optional": "", "items": []string{"", "b", "a", "b"}}
	normalized, err := Normalize(withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := normalized.Params["optional"]; ok || !reflect.DeepEqual(normalized.Params["items"], []string{"a", "b"}) {
		t.Fatalf("empty/canonical params not normalized: %#v", normalized.Params)
	}
}

func TestCanonicalRegistryIncludesFrozenReasonsActionsAndCodes(t *testing.T) {
	for _, action := range []Action{ActionRetryRuntime, ActionReloadRuntime, ActionRestartRuntime, ActionSelectRoutableAlias, ActionResyncOpenCode, ActionMigrateRewriteRule, ActionClearExternalValue} {
		if !knownAction(action) {
			t.Errorf("frozen action %q missing", action)
		}
	}
	for _, code := range []Code{CodeOpenCodeDefaultUnroutable, CodeOpenCodeContractDrift, CodeRuntimeUnreachable, CodeRuntimeParseError, CodeRewriteRuleLegacy} {
		if _, ok := codeSpecs[code]; !ok {
			t.Errorf("frozen code %q missing", code)
		}
	}
	for _, reason := range []Reason{ReasonInvalid, ReasonLegacy, ReasonDrift} {
		found := false
		for _, spec := range codeSpecs {
			found = found || spec.reason == reason
		}
		if !found {
			t.Errorf("frozen reason %q missing", reason)
		}
	}
}

func TestScanConfigClassifiesReferencesWithoutModelForeignKey(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{{ID: "disabled", Protocol: config.ProtocolOpenAIResponses, Disabled: true}, {ID: "anthropic", Protocol: config.ProtocolAnthropicMessages}}
	cfg.Aliases = []config.Alias{{Alias: "chat", Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{{Provider: "missing", Model: "external", Enabled: true}, {Provider: "disabled", Model: "not-in-catalog", Enabled: true}, {Provider: "anthropic", Model: "x", Enabled: true}}}}
	cfg.RequestRewriteRules = []config.RequestRewriteRule{{Name: "wild", Alias: "unknown", Providers: nil}, {Name: "single", Alias: "chat", Providers: []string{"gone"}}}
	cfg.ProviderPriority = []string{"gone"}
	issues, err := ScanConfig(cfg, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[Code]Issue{}
	for _, issue := range issues {
		codes[issue.Code] = issue
	}
	for _, code := range []Code{CodeAliasTargetProviderMissing, CodeAliasTargetProviderDisabled, CodeAliasTargetProtocolMismatch, CodeRewriteAliasUnresolved, CodeRewriteProviderMissing, CodePriorityProviderMissing, CodeAliasNoAvailableTarget} {
		if _, ok := codes[code]; !ok {
			t.Errorf("missing code %s", code)
		}
	}
	if actions := codes[CodeRewriteProviderMissing].AllowedActions; contains(actions, ActionRemoveSelector) {
		t.Fatal("singleton selector exposes remove_selector")
	}
	if _, ok := codes[CodeAliasTargetModelUnconfirmed]; ok {
		t.Fatal("Target.Model became an implicit foreign key")
	}
}

func TestScanConfigWildcardAndOwnershipActions(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{{ID: "p", Protocol: config.ProtocolOpenAIResponses}}
	cfg.Aliases = []config.Alias{{Alias: "auto", AutoGenerated: true, Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{{Provider: "gone", Model: "m", Enabled: true}}}}
	cfg.RequestRewriteRules = []config.RequestRewriteRule{{Name: "all", Alias: "auto", Providers: []string{}}}
	issues, _ := ScanConfig(cfg, ScanOptions{})
	for _, issue := range issues {
		if issue.Code == CodeRewriteProviderMissing {
			t.Fatal("wildcard emitted missing selector")
		}
		if issue.Code == CodeAliasTargetProviderMissing && (contains(issue.AllowedActions, ActionRemoveTarget) || !contains(issue.AllowedActions, ActionUpgradeAlias)) {
			t.Fatalf("unsafe auto actions: %v", issue.AllowedActions)
		}
	}
}

func TestScanConfigLockedZeroTargetAndAmbiguousProvider(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{
		{ID: "dup", Protocol: config.ProtocolOpenAIResponses},
		{ID: "dup", Protocol: config.ProtocolAnthropicMessages},
	}
	cfg.Aliases = []config.Alias{
		{Alias: "locked", Locked: true, Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{{Provider: "missing", Model: "m", Enabled: true}}},
		{Alias: "empty", Protocol: config.ProtocolOpenAIResponses, Enabled: true},
		{Alias: "ambiguous-provider", Protocol: config.ProtocolOpenAIResponses, Enabled: true, Targets: []config.Target{{Provider: "dup", Model: "m", Enabled: true}}},
	}
	issues, err := ScanConfig(cfg, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	foundEmpty := false
	for _, issue := range issues {
		switch {
		case issue.Code == CodeAliasTargetProviderMissing && issue.Params["alias"] == "locked":
			if contains(issue.AllowedActions, ActionRemoveTarget) || !contains(issue.AllowedActions, ActionUpgradeAlias) {
				t.Fatalf("locked alias exposed unsafe actions: %v", issue.AllowedActions)
			}
		case issue.Code == CodeAliasNoAvailableTarget && issue.Params["alias"] == "empty":
			foundEmpty = true
		case issue.Code == CodeAliasTargetProtocolMismatch && issue.Params["alias"] == "ambiguous-provider":
			t.Fatal("ambiguous provider was treated as a unique provider")
		}
	}
	if !foundEmpty {
		t.Fatal("enabled zero-target alias lacked no-available-target issue")
	}
}

func TestScanConfigAmbiguousIdentitiesDoNotBecomeMissingReferences(t *testing.T) {
	cfg := config.Default()
	cfg.Providers = []config.Provider{{ID: "dup"}, {ID: "dup"}}
	cfg.Aliases = []config.Alias{{Alias: "dup-alias", Enabled: false}, {Alias: "dup-alias", Enabled: false}, {Alias: "consumer", Enabled: true, Targets: []config.Target{{Provider: "dup", Model: "m", Enabled: true}}}}
	cfg.RequestRewriteRules = []config.RequestRewriteRule{{Name: "rule", Alias: "dup-alias", Providers: []string{"dup"}}, {Name: "rule", Alias: "dup-alias", Providers: []string{"dup"}}}
	cfg.ProviderPriority = []string{"dup"}
	issues, err := ScanConfig(cfg, ScanOptions{CatalogStates: map[string]string{"dup": "error"}})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Code]int{}
	for _, issue := range issues {
		counts[issue.Code]++
		switch issue.Code {
		case CodeProviderIdentityAmbiguous, CodeAliasIdentityAmbiguous, CodeAliasTargetIdentityAmbiguous, CodeRewriteIdentityAmbiguous:
			if !strings.HasPrefix(issue.Source.Key, "@index:") {
				t.Fatalf("duplicate identity issue is not index-anchored: %+v", issue)
			}
			if len(issue.AllowedActions) != 0 {
				t.Fatalf("ambiguous occurrence exposed actions: %+v", issue)
			}
		}
	}
	if counts[CodeProviderIdentityAmbiguous] != 2 || counts[CodeAliasIdentityAmbiguous] != 2 {
		t.Fatalf("ambiguous occurrences=%v", counts)
	}
	for _, code := range []Code{CodeAliasTargetProviderMissing, CodeRewriteAliasUnresolved, CodeRewriteProviderMissing, CodePriorityProviderMissing} {
		if counts[code] != 0 {
			t.Errorf("ambiguous identity emitted false missing code %q", code)
		}
	}
	if counts[CodeAliasNoAvailableTarget] == 0 {
		t.Fatal("alias backed only by ambiguous provider was treated as available")
	}
}

func contains(actions []Action, target Action) bool {
	for _, action := range actions {
		if action == target {
			return true
		}
	}
	return false
}
