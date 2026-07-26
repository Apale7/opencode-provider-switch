package opencode

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Apale7/opencode-provider-switch/internal/config"
)

const (
	// ModelCapabilityProbeSourceUpstream marks capabilities confirmed by the provider /models endpoint.
	ModelCapabilityProbeSourceUpstream = "upstream"
	// ModelCapabilityProbeSourceKnownDB marks capabilities resolved from the embedded known model database.
	ModelCapabilityProbeSourceKnownDB = "known_db"
	// ModelCapabilityProbeSourceFallback marks capabilities filled from protocol-safe defaults.
	ModelCapabilityProbeSourceFallback = "fallback"

	// SafeDefaultContextLimit is the conservative context limit used when probing cannot determine one.
	SafeDefaultContextLimit = 128000
	// SafeDefaultOutputLimit is the conservative output limit used when probing cannot determine one.
	SafeDefaultOutputLimit = 4096
)

var safeDefaultModalities = []string{"text"}

// knownModelsFS embeds known_models.json generated from Basellm llm-metadata all.json.
// Basellm prices are normalized from USD per 1M tokens to USD per 1K tokens.
//
//go:embed known_models.json
var knownModelsFS embed.FS

// ProviderModelProbeTarget is the minimal provider-group shape needed to probe one model.
// GroupID is required identity and is never inferred.
// BaseURLs/Headers come from the Provider; Protocol/APIKeys come from the Group.
type ProviderModelProbeTarget struct {
	ProviderID string
	GroupID    string
	Protocol   string
	BaseURLs   []string
	APIKeys    []string
	Headers    map[string]string
}

// ModelCapabilityProbe records the best-known capabilities for a model and where they came from.
type ModelCapabilityProbe struct {
	ModelID           string   `json:"modelId"`
	ProviderID        string   `json:"providerId,omitempty"`
	GroupID           string   `json:"groupId,omitempty"`
	Protocol          string   `json:"protocol"`
	ContextLimit      int64    `json:"contextLimit"`
	OutputLimit       int64    `json:"outputLimit"`
	InputModalities   []string `json:"inputModalities"`
	OutputModalities  []string `json:"outputModalities"`
	SupportsReasoning bool     `json:"supportsReasoning"`
	SupportsTools     bool     `json:"supportsTools"`
	SupportsImages    bool     `json:"supportsImages"`
	ProbeSource       string   `json:"probeSource"`
	ProbeError        string   `json:"probeError,omitempty"`
}

type knownModelCapability struct {
	ContextLimit     int64          `json:"contextLimit"`
	OutputLimit      int64          `json:"outputLimit"`
	InputModalities  []string       `json:"inputModalities"`
	OutputModalities []string       `json:"outputModalities"`
	Reasoning        bool           `json:"reasoning"`
	ToolCall         bool           `json:"toolCall"`
	Attachment       bool           `json:"attachment"`
	Cost             knownModelCost `json:"cost"`
}

type knownModelCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

var knownModelCapabilityCache struct {
	sync.Once
	models map[string]knownModelCapability
	err    error
}

// ProbeModelCapability resolves one model's capabilities through upstream, known DB, then protocol defaults.
// Only the supplied group's protocol and API keys are used; sibling group keys are never tried.
func ProbeModelCapability(ctx context.Context, provider ProviderModelProbeTarget, modelID string) ModelCapabilityProbe {
	modelID = strings.TrimSpace(modelID)
	protocol := strings.TrimSpace(provider.Protocol)
	groupID := strings.TrimSpace(provider.GroupID)
	if protocol != "" {
		protocol = config.NormalizeProviderProtocol(protocol)
	}
	probe := ProtocolDefaultModelCapability(protocol)
	probe.ProviderID = strings.TrimSpace(provider.ProviderID)
	probe.GroupID = groupID
	probe.ModelID = modelID
	probe.Protocol = protocol

	if groupID == "" {
		probe.ProbeError = "missing groupID"
		return probe
	}
	if protocol == "" {
		probe.ProbeError = "missing protocol"
		return probe
	}
	if modelID == "" {
		probe.ProbeError = "missing modelID"
		return probe
	}
	if !isSupportedCapabilityProtocol(protocol) {
		return probe
	}

	knownCapability, foundKnown := lookupKnownModelCapability(modelID)
	if foundKnown {
		probe.applyCapability(knownCapability)
	}

	upstreamErr := ""
	// Use only this group's keys — never merge or fall back to sibling groups.
	for _, baseURL := range normalizedProbeBaseURLs(provider.BaseURLs) {
		for _, apiKey := range normalizeAPIKeys(provider.APIKeys) {
			models, _, err := FetchProviderModelsDetailed(ctx, protocol, baseURL, apiKey, provider.Headers)
			if err != nil {
				upstreamErr = err.Error()
				continue
			}
			if modelListContains(models, modelID) {
				probe.ProbeSource = ModelCapabilityProbeSourceUpstream
				probe.ProbeError = ""
				return probe
			}
			upstreamErr = fmt.Sprintf("model %q not found in upstream /models", modelID)
		}
	}

	if foundKnown {
		probe.ProbeSource = ModelCapabilityProbeSourceKnownDB
		probe.ProbeError = ""
		return probe
	}

	if !isSupportedCapabilityProtocol(protocol) {
		probe.ProbeError = fmt.Sprintf("unsupported protocol %q", protocol)
	} else if upstreamErr != "" {
		probe.ProbeError = upstreamErr
	}
	return probe
}

// KnownModelCapability returns a model capability from known_models.json when available.
func KnownModelCapability(modelID string) (ModelCapabilityProbe, bool) {
	capability, ok := lookupKnownModelCapability(modelID)
	if !ok {
		return ModelCapabilityProbe{}, false
	}
	probe := ModelCapabilityProbe{ModelID: strings.TrimSpace(modelID), ProbeSource: ModelCapabilityProbeSourceKnownDB}
	probe.applyCapability(capability)
	return probe, true
}

func lookupKnownModelCapability(modelID string) (knownModelCapability, bool) {
	models, err := loadKnownModelCapabilities()
	if err != nil {
		return knownModelCapability{}, false
	}
	capability, ok := models[strings.TrimSpace(modelID)]
	return capability, ok
}

// ProtocolDefaultModelCapability returns safe defaults for protocols without known model metadata.
func ProtocolDefaultModelCapability(protocol string) ModelCapabilityProbe {
	protocol = config.NormalizeProviderProtocol(strings.TrimSpace(protocol))
	probe := ModelCapabilityProbe{
		Protocol:         protocol,
		ProbeSource:      ModelCapabilityProbeSourceFallback,
		ContextLimit:     SafeDefaultContextLimit,
		OutputLimit:      SafeDefaultOutputLimit,
		InputModalities:  cloneStrings(safeDefaultModalities),
		OutputModalities: cloneStrings(safeDefaultModalities),
	}
	if !isSupportedCapabilityProtocol(protocol) {
		probe.ProbeError = fmt.Sprintf("unsupported protocol %q", protocol)
	}
	return probe
}

// ModelConfigFromCapabilityProbe converts a capability probe into an OpenCode model config object.
func ModelConfigFromCapabilityProbe(probe ModelCapabilityProbe) map[string]any {
	modelID := strings.TrimSpace(probe.ModelID)
	config := map[string]any{
		"name": modelID,
		"limit": map[string]any{
			"context": positiveOrDefault(probe.ContextLimit, SafeDefaultContextLimit),
			"output":  positiveOrDefault(probe.OutputLimit, SafeDefaultOutputLimit),
		},
		"inputModalities":  nonEmptyStringsOrDefault(probe.InputModalities, safeDefaultModalities),
		"outputModalities": nonEmptyStringsOrDefault(probe.OutputModalities, safeDefaultModalities),
		"reasoning":        probe.SupportsReasoning,
		"toolCall":         probe.SupportsTools,
		"attachment":       probe.SupportsImages,
	}
	if modelID == "" {
		delete(config, "name")
	}
	return config
}

// SafeDefaultModelConfig exposes the safe protocol-default OpenCode model config for callers that cannot probe.
func SafeDefaultModelConfig(protocol string, modelID string) map[string]any {
	probe := ProtocolDefaultModelCapability(protocol)
	probe.ModelID = strings.TrimSpace(modelID)
	return ModelConfigFromCapabilityProbe(probe)
}

func (probe *ModelCapabilityProbe) applyCapability(capability knownModelCapability) {
	probe.ContextLimit = positiveOrDefault(capability.ContextLimit, SafeDefaultContextLimit)
	probe.OutputLimit = positiveOrDefault(capability.OutputLimit, SafeDefaultOutputLimit)
	probe.InputModalities = nonEmptyStringsOrDefault(capability.InputModalities, safeDefaultModalities)
	probe.OutputModalities = nonEmptyStringsOrDefault(capability.OutputModalities, safeDefaultModalities)
	probe.SupportsReasoning = capability.Reasoning
	probe.SupportsTools = capability.ToolCall
	probe.SupportsImages = capability.Attachment || containsCapabilityModality(probe.InputModalities, "image")
}

func loadKnownModelCapabilities() (map[string]knownModelCapability, error) {
	knownModelCapabilityCache.Do(func() {
		data, err := readKnownModelsJSON()
		if err != nil {
			knownModelCapabilityCache.models = map[string]knownModelCapability{}
			knownModelCapabilityCache.err = err
			return
		}
		models := map[string]knownModelCapability{}
		if err := json.Unmarshal(data, &models); err != nil {
			knownModelCapabilityCache.models = map[string]knownModelCapability{}
			knownModelCapabilityCache.err = err
			return
		}
		for modelID, capability := range models {
			capability.ContextLimit = positiveOrDefault(capability.ContextLimit, SafeDefaultContextLimit)
			capability.OutputLimit = positiveOrDefault(capability.OutputLimit, SafeDefaultOutputLimit)
			capability.InputModalities = nonEmptyStringsOrDefault(capability.InputModalities, safeDefaultModalities)
			capability.OutputModalities = nonEmptyStringsOrDefault(capability.OutputModalities, safeDefaultModalities)
			models[modelID] = capability
		}
		knownModelCapabilityCache.models = models
	})
	return knownModelCapabilityCache.models, knownModelCapabilityCache.err
}

func readKnownModelsJSON() ([]byte, error) {
	return knownModelsFS.ReadFile("known_models.json")
}

func normalizedProbeBaseURLs(baseURLs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(baseURLs))
	for _, item := range baseURLs {
		baseURL := config.NormalizeProviderBaseURL(item)
		if baseURL == "" || seen[baseURL] {
			continue
		}
		seen[baseURL] = true
		out = append(out, baseURL)
	}
	return out
}

func modelListContains(models []string, modelID string) bool {
	for _, item := range models {
		if item == modelID {
			return true
		}
	}
	return false
}

func isSupportedCapabilityProtocol(protocol string) bool {
	switch config.NormalizeProviderProtocol(protocol) {
	case config.ProtocolOpenAIResponses, config.ProtocolAnthropicMessages, config.ProtocolOpenAICompatible:
		return true
	default:
		return false
	}
}

func positiveOrDefault(value int64, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

func nonEmptyStringsOrDefault(values []string, fallback []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	if len(out) == 0 {
		return cloneStrings(fallback)
	}
	return out
}

func cloneStrings(values []string) []string {
	return append([]string(nil), values...)
}

func containsCapabilityModality(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
