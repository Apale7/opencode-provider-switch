package config

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const (
	RequestRewriteOpSet    = "set"
	RequestRewriteOpDelete = "delete"
	RequestRewriteOpAppend = "append"
	RequestRewriteOpInsert = "insert"
)

type RequestRewriteOperation struct {
	Op       string `json:"op"`
	Path     string `json:"path"`
	Value    any    `json:"-"`
	ValueSet bool   `json:"-"`
	Index    *int   `json:"index,omitempty"`
}

func (op RequestRewriteOperation) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"op":   op.Op,
		"path": op.Path,
	}
	if op.ValueSet {
		out["value"] = op.Value
	}
	if op.Index != nil {
		out["index"] = *op.Index
	}
	return json.Marshal(out)
}

func (op *RequestRewriteOperation) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if value, ok := raw["op"]; ok {
		if err := json.Unmarshal(value, &op.Op); err != nil {
			return fmt.Errorf("op: %w", err)
		}
	}
	if value, ok := raw["path"]; ok {
		if err := json.Unmarshal(value, &op.Path); err != nil {
			return fmt.Errorf("path: %w", err)
		}
	}
	if value, ok := raw["value"]; ok {
		if err := json.Unmarshal(value, &op.Value); err != nil {
			return fmt.Errorf("value: %w", err)
		}
		op.ValueSet = true
	}
	if value, ok := raw["index"]; ok {
		var index int
		if err := json.Unmarshal(value, &index); err != nil {
			return fmt.Errorf("index: %w", err)
		}
		op.Index = &index
	}
	return nil
}

type rewritePathSegment struct {
	Name  string
	Index *int
}

func RequestRewriteRuleUsesLegacySyntax(rule RequestRewriteRule) bool {
	return len(rule.Set) > 0 || len(rule.Delete) > 0
}

func RequestRewriteRuleWarnings(rule RequestRewriteRule) []string {
	if RequestRewriteRuleUsesLegacySyntax(rule) {
		return []string{RequestRewriteLegacyWarning(rule)}
	}
	return nil
}

func RequestRewriteLegacyWarning(rule RequestRewriteRule) string {
	return fmt.Sprintf("request rewrite rule %q uses legacy set/delete syntax and will be skipped; migrate it to ops with RFC 9535 JSONPath paths", strings.TrimSpace(rule.Name))
}

func ApplyRequestRewriteRules(payload map[string]any, aliasName, provider, model string, rules []RequestRewriteRule) {
	if payload == nil || len(rules) == 0 {
		return
	}
	aliasName = strings.TrimSpace(aliasName)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	for _, rule := range rules {
		rule = normalizeRequestRewriteRule(rule)
		if !rule.Enabled || RequestRewriteRuleUsesLegacySyntax(rule) || !requestRewriteRuleMatches(rule, aliasName, provider) {
			continue
		}
		for _, op := range rule.Ops {
			applyRequestRewriteOperation(payload, rule.Override, op)
		}
	}
}

func applyRequestRewriteOperation(payload map[string]any, override bool, op RequestRewriteOperation) {
	segments, err := parseRequestRewritePath(op.Path)
	if err != nil || len(segments) == 0 {
		return
	}
	switch op.Op {
	case RequestRewriteOpSet:
		if op.ValueSet {
			setRequestRewriteValue(payload, segments, op.Value, override)
		}
	case RequestRewriteOpDelete:
		if override {
			deleteRequestRewriteValue(payload, segments)
		}
	case RequestRewriteOpAppend:
		if override && op.ValueSet {
			appendRequestRewriteValue(payload, segments, op.Value)
		}
	case RequestRewriteOpInsert:
		if override && op.Index != nil && op.ValueSet {
			insertRequestRewriteValue(payload, segments, *op.Index, op.Value)
		}
	}
}

func setRequestRewriteValue(payload map[string]any, segments []rewritePathSegment, value any, override bool) {
	parent, last, ok := resolveRewriteParent(payload, segments, true)
	if !ok {
		return
	}
	if last.Index != nil {
		array, ok := parent.([]any)
		if !ok || *last.Index < 0 || *last.Index >= len(array) {
			return
		}
		if override {
			array[*last.Index] = cloneJSONValue(value)
		}
		return
	}
	object, ok := parent.(map[string]any)
	if !ok {
		return
	}
	if override {
		object[last.Name] = cloneJSONValue(value)
		return
	}
	if _, exists := object[last.Name]; !exists {
		object[last.Name] = cloneJSONValue(value)
	}
}

func deleteRequestRewriteValue(payload map[string]any, segments []rewritePathSegment) {
	parent, last, ok := resolveRewriteParent(payload, segments, false)
	if !ok {
		return
	}
	if last.Index != nil {
		array, ok := parent.([]any)
		if !ok || *last.Index < 0 || *last.Index >= len(array) {
			return
		}
		array = append(array[:*last.Index], array[*last.Index+1:]...)
		assignRewriteValue(payload, segments[:len(segments)-1], array)
		return
	}
	if object, ok := parent.(map[string]any); ok {
		delete(object, last.Name)
	}
}

func appendRequestRewriteValue(payload map[string]any, segments []rewritePathSegment, value any) {
	container, ok := resolveRewriteContainer(payload, segments)
	if !ok {
		return
	}
	array, ok := container.([]any)
	if !ok {
		return
	}
	array = append(array, cloneJSONValue(value))
	assignRewriteValue(payload, segments, array)
}

func insertRequestRewriteValue(payload map[string]any, segments []rewritePathSegment, index int, value any) {
	container, ok := resolveRewriteContainer(payload, segments)
	if !ok {
		return
	}
	array, ok := container.([]any)
	if !ok || index < 0 || index > len(array) {
		return
	}
	array = append(array, nil)
	copy(array[index+1:], array[index:])
	array[index] = cloneJSONValue(value)
	assignRewriteValue(payload, segments, array)
}

func resolveRewriteParent(payload map[string]any, segments []rewritePathSegment, createObjects bool) (any, rewritePathSegment, bool) {
	if len(segments) == 0 {
		return nil, rewritePathSegment{}, false
	}
	current := any(payload)
	for _, segment := range segments[:len(segments)-1] {
		next, ok := resolveRewriteChild(current, segment)
		if !ok && createObjects && segment.Index == nil {
			object, isObject := current.(map[string]any)
			if !isObject {
				return nil, rewritePathSegment{}, false
			}
			next = map[string]any{}
			object[segment.Name] = next
			ok = true
		}
		if !ok {
			return nil, rewritePathSegment{}, false
		}
		current = next
	}
	return current, segments[len(segments)-1], true
}

func resolveRewriteContainer(payload map[string]any, segments []rewritePathSegment) (any, bool) {
	current := any(payload)
	for _, segment := range segments {
		next, ok := resolveRewriteChild(current, segment)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func resolveRewriteChild(current any, segment rewritePathSegment) (any, bool) {
	if segment.Index != nil {
		array, ok := current.([]any)
		if !ok || *segment.Index < 0 || *segment.Index >= len(array) {
			return nil, false
		}
		return array[*segment.Index], true
	}
	object, ok := current.(map[string]any)
	if !ok {
		return nil, false
	}
	next, ok := object[segment.Name]
	return next, ok
}

func assignRewriteValue(payload map[string]any, segments []rewritePathSegment, value any) {
	if len(segments) == 0 {
		return
	}
	parent, last, ok := resolveRewriteParent(payload, segments, false)
	if !ok {
		return
	}
	if last.Index != nil {
		array, ok := parent.([]any)
		if !ok || *last.Index < 0 || *last.Index >= len(array) {
			return
		}
		array[*last.Index] = value
		return
	}
	if object, ok := parent.(map[string]any); ok {
		object[last.Name] = value
	}
}

func validateRequestRewriteOperations(rule RequestRewriteRule) []error {
	errs := []error{}
	for index, op := range rule.Ops {
		if err := validateRequestRewriteOperation(rule, index, op); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func validateRequestRewriteOperation(rule RequestRewriteRule, index int, op RequestRewriteOperation) error {
	label := fmt.Sprintf("request rewrite rule %q op %d", rule.Name, index+1)
	switch op.Op {
	case RequestRewriteOpSet, RequestRewriteOpDelete, RequestRewriteOpAppend, RequestRewriteOpInsert:
	default:
		return fmt.Errorf("%s has invalid op %q", label, op.Op)
	}
	if _, err := parseRequestRewritePath(op.Path); err != nil {
		return fmt.Errorf("%s has invalid path: %w", label, err)
	}
	if op.Op == RequestRewriteOpDelete || op.Op == RequestRewriteOpAppend || op.Op == RequestRewriteOpInsert {
		if !rule.Override {
			return fmt.Errorf("%s %s requires override", label, op.Op)
		}
	}
	if op.Op == RequestRewriteOpInsert {
		if op.Index == nil {
			return fmt.Errorf("%s insert requires index", label)
		}
		if *op.Index < 0 {
			return fmt.Errorf("%s insert index must be >= 0", label)
		}
	} else if op.Index != nil {
		return fmt.Errorf("%s index is only valid for insert", label)
	}
	if op.Op == RequestRewriteOpSet || op.Op == RequestRewriteOpAppend || op.Op == RequestRewriteOpInsert {
		if !op.ValueSet {
			return fmt.Errorf("%s %s requires value", label, op.Op)
		}
	}
	if op.Op == RequestRewriteOpDelete && op.ValueSet {
		return fmt.Errorf("%s delete must not include value", label)
	}
	return nil
}

func parseRequestRewritePath(path string) ([]rewritePathSegment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if path == "$" {
		return nil, fmt.Errorf("root-only path is not a mutation target")
	}
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("path must start with $")
	}
	segments := []rewritePathSegment{}
	for i := 1; i < len(path); {
		switch path[i] {
		case '.':
			if i+1 < len(path) && path[i+1] == '.' {
				return nil, fmt.Errorf("recursive descent is not supported")
			}
			name, next, err := parseDotName(path, i+1)
			if err != nil {
				return nil, err
			}
			segments = append(segments, rewritePathSegment{Name: name})
			i = next
		case '[':
			segment, next, err := parseBracketSegment(path, i)
			if err != nil {
				return nil, err
			}
			segments = append(segments, segment)
			i = next
		default:
			return nil, fmt.Errorf("unexpected character %q", path[i])
		}
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("path must select a child")
	}
	return segments, nil
}

func parseDotName(path string, start int) (string, int, error) {
	if start >= len(path) || !isJSONPathNameStart(rune(path[start])) {
		return "", 0, fmt.Errorf("invalid dot-name selector")
	}
	end := start + 1
	for end < len(path) && isJSONPathNameChar(rune(path[end])) {
		end++
	}
	return path[start:end], end, nil
}

func parseBracketSegment(path string, start int) (rewritePathSegment, int, error) {
	contentStart := start + 1
	for contentStart < len(path) && unicode.IsSpace(rune(path[contentStart])) {
		contentStart++
	}
	if contentStart >= len(path) {
		return rewritePathSegment{}, 0, fmt.Errorf("empty bracket selector")
	}
	if path[contentStart] == '\'' || path[contentStart] == '"' {
		quote := path[contentStart]
		contentEnd := contentStart + 1
		for contentEnd < len(path) {
			if path[contentEnd] == '\\' {
				contentEnd += 2
				continue
			}
			if path[contentEnd] == quote {
				break
			}
			contentEnd++
		}
		if contentEnd >= len(path) {
			return rewritePathSegment{}, 0, fmt.Errorf("unterminated quoted name selector")
		}
		end := contentEnd + 1
		for end < len(path) && unicode.IsSpace(rune(path[end])) {
			end++
		}
		if end >= len(path) || path[end] != ']' {
			return rewritePathSegment{}, 0, fmt.Errorf("invalid quoted name selector")
		}
		name, err := parseQuotedPathName(path[contentStart : contentEnd+1])
		if err != nil {
			return rewritePathSegment{}, 0, err
		}
		return rewritePathSegment{Name: name}, end + 1, nil
	}
	end := strings.IndexByte(path[contentStart:], ']')
	if end < 0 {
		return rewritePathSegment{}, 0, fmt.Errorf("unterminated bracket selector")
	}
	end += contentStart
	content := strings.TrimSpace(path[contentStart:end])
	if content == "" {
		return rewritePathSegment{}, 0, fmt.Errorf("empty bracket selector")
	}
	if strings.ContainsAny(content, "*?:,") {
		return rewritePathSegment{}, 0, fmt.Errorf("wildcard, filter, slice, and union selectors are not supported")
	}
	index, err := strconv.Atoi(content)
	if err != nil {
		return rewritePathSegment{}, 0, fmt.Errorf("bracket selector must be a quoted name or non-negative array index")
	}
	if index < 0 {
		return rewritePathSegment{}, 0, fmt.Errorf("negative array indexes are not supported")
	}
	return rewritePathSegment{Index: &index}, end + 1, nil
}

func parseQuotedPathName(content string) (string, error) {
	if len(content) < 2 || content[0] != content[len(content)-1] || (content[0] != '\'' && content[0] != '"') {
		return "", fmt.Errorf("invalid quoted name selector")
	}
	inner := content[1 : len(content)-1]
	value := strings.Builder{}
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' {
			value.WriteByte(inner[i])
			continue
		}
		i++
		if i >= len(inner) {
			return "", fmt.Errorf("invalid quoted name selector")
		}
		switch inner[i] {
		case '\\', '/', '\'', '"':
			value.WriteByte(inner[i])
		case 'b':
			value.WriteByte('\b')
		case 'f':
			value.WriteByte('\f')
		case 'n':
			value.WriteByte('\n')
		case 'r':
			value.WriteByte('\r')
		case 't':
			value.WriteByte('\t')
		default:
			return "", fmt.Errorf("invalid quoted name selector")
		}
	}
	if value.Len() == 0 {
		return "", fmt.Errorf("quoted name selector cannot be empty")
	}
	return value.String(), nil
}

func isJSONPathNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isJSONPathNameChar(r rune) bool {
	return isJSONPathNameStart(r) || unicode.IsDigit(r)
}

func normalizeRequestRewriteOperation(op RequestRewriteOperation) RequestRewriteOperation {
	op.Op = strings.ToLower(strings.TrimSpace(op.Op))
	op.Path = strings.TrimSpace(op.Path)
	op.Value = cloneJSONValue(op.Value)
	return op
}

func cloneRequestRewriteOperations(in []RequestRewriteOperation) []RequestRewriteOperation {
	if len(in) == 0 {
		return nil
	}
	out := make([]RequestRewriteOperation, len(in))
	for i := range in {
		out[i] = normalizeRequestRewriteOperation(in[i])
		out[i].ValueSet = in[i].ValueSet
		if in[i].Index != nil {
			index := *in[i].Index
			out[i].Index = &index
		}
	}
	return out
}
