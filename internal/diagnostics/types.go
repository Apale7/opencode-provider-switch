package diagnostics

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const SchemaVersion = 1

type Severity string
type Reason string
type Action string
type EntityKind string
type Code string
type Params map[string]any

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"

	ReasonMissing            Reason = "missing"
	ReasonDisabled           Reason = "disabled"
	ReasonProtocolMismatch   Reason = "protocol_mismatch"
	ReasonCatalogStale       Reason = "catalog_stale"
	ReasonRuntimeUnavailable Reason = "runtime_unavailable"
	ReasonNoAvailableTarget  Reason = "no_available_target"
	ReasonAmbiguous          Reason = "ambiguous"
	ReasonInvalid            Reason = "invalid"
	ReasonLegacy             Reason = "legacy"
	ReasonDrift              Reason = "drift"

	ActionEnableAlias         Action = "enable_alias"
	ActionEnableProvider      Action = "enable_provider"
	ActionEnableGroup         Action = "enable_group"
	ActionEnableTarget        Action = "enable_target"
	ActionRebindTarget        Action = "rebind_target"
	ActionAlignProtocol       Action = "align_protocol"
	ActionRefreshCatalog      Action = "refresh_catalog"
	ActionRetryRuntime        Action = "retry_runtime"
	ActionReloadRuntime       Action = "reload_runtime"
	ActionRestartRuntime      Action = "restart_runtime"
	ActionSelectRoutableAlias Action = "select_routable_alias"
	ActionResyncOpenCode      Action = "resync_opencode"
	ActionMigrateRewriteRule  Action = "migrate_rewrite_rule"
	ActionReplaceSelector     Action = "replace_selector"
	ActionRemoveSelector      Action = "remove_selector"
	ActionDisableRule         Action = "disable_rule"
	ActionRemoveTarget        Action = "remove_target"
	ActionClearExternalValue  Action = "clear_external_value"
	ActionRemovePriorityEntry Action = "remove_priority_entry"
	ActionDeleteRule          Action = "delete_rule"
	ActionDeleteAlias         Action = "delete_alias"
	ActionKeep                Action = "keep"
)

const (
	CodeProviderIdentityAmbiguous       Code = "provider_identity_ambiguous"
	CodeProviderGroupIdentityAmbiguous  Code = "provider_group_identity_ambiguous"
	CodeAliasIdentityAmbiguous          Code = "alias_identity_ambiguous"
	CodeAliasTargetIdentityAmbiguous    Code = "alias_target_identity_ambiguous"
	CodeRewriteIdentityAmbiguous        Code = "rewrite_rule_identity_ambiguous"
	CodeProviderGroupsEmpty             Code = "provider_groups_empty"
	CodeProviderGroupIDEmpty            Code = "provider_group_id_empty"
	CodeProviderGroupProtocolUnknown    Code = "provider_group_protocol_unknown"
	CodeAliasTargetProviderMissing      Code = "alias_target_provider_missing"
	CodeAliasTargetGroupMissing         Code = "alias_target_group_missing"
	CodeAliasDisabled                   Code = "alias_disabled"
	CodeAliasTargetDisabled             Code = "alias_target_disabled"
	CodeAliasTargetProviderDisabled     Code = "alias_target_provider_disabled"
	CodeAliasTargetGroupDisabled        Code = "alias_target_group_disabled"
	CodeAliasTargetProtocolMismatch     Code = "alias_target_protocol_mismatch"
	CodeProviderCatalogStale            Code = "provider_model_catalog_stale"
	CodeAliasTargetModelUnconfirmed     Code = "alias_target_model_unconfirmed"
	CodeAliasNoAvailableTarget          Code = "alias_no_available_target"
	CodeRewriteAliasUnresolved          Code = "rewrite_alias_selector_unresolved"
	CodeRewriteProviderMissing          Code = "rewrite_provider_selector_missing"
	CodeRewriteProviderGroupMissing     Code = "rewrite_provider_group_selector_missing"
	CodePriorityProviderMissing         Code = "provider_priority_entry_missing"
	CodeOpenCodeDefaultUnroutable       Code = "opencode_default_model_unroutable"
	CodeOpenCodeSmallUnroutable         Code = "opencode_small_model_unroutable"
	CodeOpenCodeContractMissing         Code = "opencode_provider_contract_missing"
	CodeOpenCodeContractInvalid         Code = "opencode_provider_contract_invalid"
	CodeOpenCodeContractDrift           Code = "opencode_provider_contract_drift"
	CodeOpenCodeCatalogDrift            Code = "opencode_catalog_drift"
	CodeAliasMissing                    Code = "alias_missing"
	CodeNoAvailableTarget               Code = "no_available_target"
	CodeAliasTargetRuntimeUnavailable   Code = "alias_target_runtime_unavailable"
	CodeRuntimeUnreachable              Code = "runtime_unreachable"
	CodeRuntimeAuthFailed               Code = "runtime_auth_failed"
	CodeRuntimeBadStatus                Code = "runtime_bad_status"
	CodeRuntimeParseError               Code = "runtime_parse_error"
	CodeRuntimeProviderMissing          Code = "runtime_provider_missing"
	CodeRuntimeProviderProtocolMismatch Code = "runtime_provider_protocol_mismatch"
	CodeConfigInvalid                   Code = "config_invalid"
	CodeFileParseError                  Code = "file_parse_error"
	CodeRewriteRuleLegacy               Code = "rewrite_rule_legacy"
)

type Source struct {
	Kind EntityKind `json:"kind"`
	Key  string     `json:"key"`
	Path string     `json:"path"`
}

type Target struct {
	Kind EntityKind `json:"kind"`
	Key  string     `json:"key"`
	Path *string    `json:"path"`
}

type Issue struct {
	SchemaVersion  int      `json:"schemaVersion"`
	Code           Code     `json:"code"`
	Severity       Severity `json:"severity"`
	Path           string   `json:"path"`
	Source         Source   `json:"source"`
	Target         *Target  `json:"target"`
	Reason         Reason   `json:"reason"`
	AllowedActions []Action `json:"allowedActions"`
	Params         Params   `json:"params"`
}

var actionOrder = []Action{
	ActionEnableAlias, ActionEnableProvider, ActionEnableGroup, ActionEnableTarget,
	ActionRebindTarget, ActionAlignProtocol, ActionRefreshCatalog,
	ActionRetryRuntime, ActionReloadRuntime, ActionRestartRuntime,
	ActionSelectRoutableAlias, ActionResyncOpenCode, ActionMigrateRewriteRule,
	ActionReplaceSelector, ActionRemoveSelector, ActionDisableRule,
	ActionRemoveTarget, ActionClearExternalValue, ActionRemovePriorityEntry, ActionDeleteRule,
	ActionDeleteAlias, ActionKeep,
}

type codeSpec struct {
	severity Severity
	reason   Reason
	required []string
}

var codeSpecs = map[Code]codeSpec{
	CodeProviderIdentityAmbiguous:       {SeverityError, ReasonAmbiguous, []string{"providerId", "occurrenceCount", "occurrencePaths"}},
	CodeProviderGroupIdentityAmbiguous:  {SeverityError, ReasonAmbiguous, []string{"providerId", "groupId", "occurrenceCount", "occurrencePaths"}},
	CodeAliasIdentityAmbiguous:          {SeverityError, ReasonAmbiguous, []string{"alias", "occurrenceCount", "occurrencePaths"}},
	CodeAliasTargetIdentityAmbiguous:    {SeverityError, ReasonAmbiguous, []string{"alias", "providerId", "groupId", "model", "occurrenceCount", "occurrencePaths"}},
	CodeRewriteIdentityAmbiguous:        {SeverityError, ReasonAmbiguous, []string{"ruleName", "occurrenceCount", "occurrencePaths"}},
	CodeProviderGroupsEmpty:             {SeverityError, ReasonInvalid, []string{"providerId"}},
	CodeProviderGroupIDEmpty:            {SeverityError, ReasonInvalid, []string{"providerId", "groupIndex"}},
	CodeProviderGroupProtocolUnknown:    {SeverityError, ReasonInvalid, []string{"providerId", "groupId", "groupIndex", "protocol"}},
	CodeAliasTargetProviderMissing:      {SeverityError, ReasonMissing, []string{"alias", "targetIndex", "providerId", "groupId", "model"}},
	CodeAliasTargetGroupMissing:         {SeverityError, ReasonMissing, []string{"alias", "targetIndex", "providerId", "groupId", "model"}},
	CodeAliasDisabled:                   {SeverityInfo, ReasonDisabled, []string{"alias"}},
	CodeAliasTargetDisabled:             {SeverityInfo, ReasonDisabled, []string{"alias", "targetIndex", "providerId", "groupId", "model"}},
	CodeAliasTargetProviderDisabled:     {SeverityWarning, ReasonDisabled, []string{"alias", "targetIndex", "providerId", "groupId", "model"}},
	CodeAliasTargetGroupDisabled:        {SeverityWarning, ReasonDisabled, []string{"alias", "targetIndex", "providerId", "groupId", "model"}},
	CodeAliasTargetProtocolMismatch:     {SeverityError, ReasonProtocolMismatch, []string{"alias", "targetIndex", "providerId", "groupId", "model", "aliasProtocol", "groupProtocol"}},
	CodeProviderCatalogStale:            {SeverityWarning, ReasonCatalogStale, []string{"providerId", "groupId", "catalogState"}},
	CodeAliasTargetModelUnconfirmed:     {SeverityInfo, ReasonCatalogStale, []string{"alias", "targetIndex", "providerId", "groupId", "model", "catalogState"}},
	CodeAliasNoAvailableTarget:          {SeverityWarning, ReasonNoAvailableTarget, []string{"alias", "targetCount", "missingCount", "disabledCount", "protocolMismatchCount", "ambiguousCount"}},
	CodeRewriteAliasUnresolved:          {SeverityInfo, ReasonMissing, []string{"ruleName", "ruleIndex", "alias", "directFallbackPossible"}},
	CodeRewriteProviderMissing:          {SeverityWarning, ReasonMissing, []string{"ruleName", "ruleIndex", "providerId", "selectorIndex", "selectorCount", "wildcardIfEmpty"}},
	CodeRewriteProviderGroupMissing:     {SeverityWarning, ReasonMissing, []string{"ruleName", "ruleIndex", "providerId", "groupId", "selectorIndex", "selectorCount", "wildcardIfEmpty"}},
	CodePriorityProviderMissing:         {SeverityInfo, ReasonMissing, []string{"providerId", "priorityIndex"}},
	CodeOpenCodeDefaultUnroutable:       {SeverityWarning, ReasonMissing, nil},
	CodeOpenCodeSmallUnroutable:         {SeverityWarning, ReasonMissing, nil},
	CodeOpenCodeContractMissing:         {SeverityWarning, ReasonMissing, nil},
	CodeOpenCodeContractInvalid:         {SeverityError, ReasonInvalid, nil},
	CodeOpenCodeContractDrift:           {SeverityError, ReasonDrift, nil},
	CodeOpenCodeCatalogDrift:            {SeverityWarning, ReasonDrift, nil},
	CodeAliasMissing:                    {SeverityError, ReasonMissing, nil},
	CodeNoAvailableTarget:               {SeverityError, ReasonNoAvailableTarget, nil},
	CodeAliasTargetRuntimeUnavailable:   {SeverityWarning, ReasonRuntimeUnavailable, nil},
	CodeRuntimeUnreachable:              {SeverityWarning, ReasonRuntimeUnavailable, nil},
	CodeRuntimeAuthFailed:               {SeverityError, ReasonRuntimeUnavailable, nil},
	CodeRuntimeBadStatus:                {SeverityError, ReasonRuntimeUnavailable, nil},
	CodeRuntimeParseError:               {SeverityError, ReasonInvalid, nil},
	CodeRuntimeProviderMissing:          {SeverityWarning, ReasonMissing, nil},
	CodeRuntimeProviderProtocolMismatch: {SeverityError, ReasonProtocolMismatch, nil},
	CodeConfigInvalid:                   {SeverityError, ReasonInvalid, nil},
	CodeFileParseError:                  {SeverityError, ReasonInvalid, nil},
	CodeRewriteRuleLegacy:               {SeverityWarning, ReasonLegacy, nil},
}

var validEntityKinds = map[EntityKind]bool{
	"config": true, "provider": true, "provider_group": true, "alias": true, "alias_target": true,
	"rewrite_rule": true, "priority_entry": true, "model_catalog": true,
	"model_symbol": true, "external_config_field": true, "runtime": true,
	"request": true,
}

var validCatalogStates = map[string]bool{
	"not_observed": true, "skipped": true, "error": true, "empty": true,
	"fingerprint_mismatch": true, "incomplete": true, "untrusted_source": true,
}

func Normalize(issue Issue) (Issue, error) {
	if issue.SchemaVersion == 0 {
		issue.SchemaVersion = SchemaVersion
	}
	if issue.AllowedActions == nil {
		issue.AllowedActions = []Action{}
	}
	if issue.Params == nil {
		issue.Params = Params{}
	}
	params, err := normalizeParams(issue.Params)
	if err != nil {
		return Issue{}, err
	}
	issue.Params = params
	if err := Validate(issue); err != nil {
		return Issue{}, err
	}
	issue.AllowedActions = orderedActions(issue.AllowedActions)
	return issue, nil
}

func Validate(issue Issue) error {
	if issue.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported diagnostic schema version %d", issue.SchemaVersion)
	}
	spec, ok := codeSpecs[issue.Code]
	if !ok {
		return fmt.Errorf("unknown diagnostic code %q", issue.Code)
	}
	if issue.Severity != spec.severity || issue.Reason != spec.reason {
		return fmt.Errorf("diagnostic %q requires severity %q and reason %q", issue.Code, spec.severity, spec.reason)
	}
	if !validLogicalPath(issue.Path) {
		return fmt.Errorf("invalid diagnostic path %q", issue.Path)
	}
	if !validEntityKinds[issue.Source.Kind] || issue.Source.Key == "" || !validLogicalPath(issue.Source.Path) {
		return fmt.Errorf("diagnostic source is incomplete")
	}
	if issue.Target != nil {
		if !validEntityKinds[issue.Target.Kind] || issue.Target.Key == "" {
			return fmt.Errorf("diagnostic target is incomplete")
		}
		if issue.Target.Path != nil && !validLogicalPath(*issue.Target.Path) {
			return fmt.Errorf("invalid diagnostic target path %q", *issue.Target.Path)
		}
	}
	for _, action := range issue.AllowedActions {
		if !knownAction(action) {
			return fmt.Errorf("unknown diagnostic action %q", action)
		}
	}
	for key, value := range issue.Params {
		if isSensitiveKey(key) {
			return fmt.Errorf("sensitive diagnostic param %q", key)
		}
		if !validParam(value) {
			return fmt.Errorf("invalid diagnostic param %q", key)
		}
		if err := validateParamValue(value); err != nil {
			return fmt.Errorf("invalid diagnostic param %q: %w", key, err)
		}
	}
	for _, key := range spec.required {
		if _, ok := issue.Params[key]; !ok {
			return fmt.Errorf("diagnostic %q requires param %q", issue.Code, key)
		}
	}
	if state, ok := issue.Params["catalogState"].(string); ok && !validCatalogStates[state] {
		return fmt.Errorf("invalid catalogState %q", state)
	}
	return nil
}

func validLogicalPath(path string) bool {
	validRoot := path == "/config" || strings.HasPrefix(path, "/config/") ||
		path == "/opencode/file" || strings.HasPrefix(path, "/opencode/file/") ||
		path == "/opencode/runtime" || strings.HasPrefix(path, "/opencode/runtime/") ||
		path == "/runtime" || strings.HasPrefix(path, "/runtime/")
	if !validRoot || strings.Contains(path, "\\") {
		return false
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '~' && (i+1 >= len(path) || (path[i+1] != '0' && path[i+1] != '1')) {
			return false
		}
	}
	return true
}

func EscapePathToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func SortAndDedupe(input []Issue) ([]Issue, error) {
	byKey := map[string]Issue{}
	for _, raw := range input {
		issue, err := Normalize(raw)
		if err != nil {
			return nil, err
		}
		key := issueKey(issue)
		if existing, ok := byKey[key]; ok {
			existing.AllowedActions = intersectActions(existing.AllowedActions, issue.AllowedActions)
			byKey[key] = existing
		} else {
			byKey[key] = issue
		}
	}
	out := make([]Issue, 0, len(byKey))
	for _, issue := range byKey {
		out = append(out, issue)
	}
	sort.Slice(out, func(i, j int) bool { return sortKey(out[i]) < sortKey(out[j]) })
	return out, nil
}

func issueKey(issue Issue) string {
	copy := issue
	copy.AllowedActions = nil
	raw, _ := json.Marshal(copy)
	return string(raw)
}

func sortKey(issue Issue) string {
	weight := map[Severity]string{SeverityError: "0", SeverityWarning: "1", SeverityInfo: "2"}[issue.Severity]
	target := "~"
	if issue.Target != nil {
		target = string(issue.Target.Kind) + "\x00" + issue.Target.Key
	}
	params, _ := json.Marshal(issue.Params)
	return strings.Join([]string{weight, string(issue.Code), issue.Path, string(issue.Source.Kind), issue.Source.Key, target, string(issue.Reason), string(params)}, "\x00")
}

func orderedActions(actions []Action) []Action {
	set := map[Action]bool{}
	for _, action := range actions {
		set[action] = true
	}
	out := make([]Action, 0, len(set))
	for _, action := range actionOrder {
		if set[action] {
			out = append(out, action)
			delete(set, action)
		}
	}
	return out
}

func knownAction(action Action) bool {
	for _, known := range actionOrder {
		if action == known {
			return true
		}
	}
	return false
}

func intersectActions(left, right []Action) []Action {
	rightSet := map[Action]bool{}
	for _, action := range right {
		rightSet[action] = true
	}
	var out []Action
	for _, action := range left {
		if rightSet[action] {
			out = append(out, action)
		}
	}
	return orderedActions(out)
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.NewReplacer("_", "", "-", "", ".", "").Replace(normalized)
	for _, bad := range []string{"apikey", "token", "secret", "authorization", "cookie", "headers", "body", "raw", "message", "error", "url", "revision", "digest"} {
		if strings.Contains(normalized, bad) {
			return true
		}
	}
	return false
}

func validParam(value any) bool {
	switch value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64,
		[]string, []bool, []int, []int8, []int16, []int32, []int64, []uint, []uint8, []uint16, []uint32, []uint64:
		return true
	default:
		return false
	}
}

func validateParamValue(value any) error {
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Slice {
		for i := 0; i < rv.Len(); i++ {
			if err := validateParamValue(rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	parsed, _ := url.Parse(text)
	firstSegment := text
	if slash := strings.IndexByte(firstSegment, '/'); slash >= 0 {
		firstSegment = firstSegment[:slash]
	}
	looksLikeBaseURL := strings.Contains(text, "://") || strings.HasPrefix(text, "//") ||
		(strings.Contains(text, "/") && strings.Contains(firstSegment, "."))
	if looksLikeBaseURL || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasPrefix(text, `\\`) || filepath.VolumeName(text) != "" {
		return fmt.Errorf("URL or absolute path is forbidden")
	}
	if strings.HasPrefix(text, "/") && !validLogicalPath(text) {
		return fmt.Errorf("absolute path is forbidden")
	}
	return nil
}

func normalizeParams(input Params) (Params, error) {
	out := make(Params, len(input))
	for key, value := range input {
		if isSensitiveKey(key) {
			return nil, fmt.Errorf("sensitive diagnostic param %q", key)
		}
		if !validParam(value) {
			return nil, fmt.Errorf("invalid diagnostic param %q", key)
		}
		if text, ok := value.(string); ok && text == "" {
			continue
		}
		rv := reflect.ValueOf(value)
		if rv.Kind() != reflect.Slice {
			out[key] = value
			continue
		}
		if rv.IsNil() {
			rv = reflect.MakeSlice(rv.Type(), 0, 0)
		}
		items := make([]reflect.Value, rv.Len())
		items = items[:0]
		for i := 0; i < rv.Len(); i++ {
			item := rv.Index(i)
			if item.Kind() == reflect.String && item.String() == "" {
				continue
			}
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i].Interface()) < fmt.Sprint(items[j].Interface()) })
		normalized := reflect.MakeSlice(rv.Type(), 0, len(items))
		var previous string
		for i, item := range items {
			current := fmt.Sprint(item.Interface())
			if i > 0 && current == previous {
				continue
			}
			normalized = reflect.Append(normalized, item)
			previous = current
		}
		out[key] = normalized.Interface()
	}
	return out, nil
}
