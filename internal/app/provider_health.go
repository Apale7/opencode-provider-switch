package app

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Apale7/opencode-provider-switch/internal/config"
	"github.com/Apale7/opencode-provider-switch/internal/proxy"
)

const providerHealthLowSampleThreshold = 5

type ProviderHealthInput struct {
	Aliases     []string `json:"aliases,omitempty"`
	Providers   []string `json:"providers,omitempty"`
	StartedFrom string   `json:"startedFrom,omitempty"`
	StartedTo   string   `json:"startedTo,omitempty"`
}

type ProviderHealthResult struct {
	Summary            ProviderHealthSummary `json:"summary"`
	Providers          []ProviderHealthView  `json:"providers"`
	AvailableAliases   []string              `json:"availableAliases,omitempty"`
	AvailableProviders []string              `json:"availableProviders,omitempty"`
	Warnings           []string              `json:"warnings,omitempty"`
}

type ProviderHealthSummary struct {
	RequestCount       int     `json:"requestCount"`
	AttemptCount       int     `json:"attemptCount"`
	Success            int     `json:"success"`
	Failed             int     `json:"failed"`
	Failover           int     `json:"failover"`
	RetryableFailures  int     `json:"retryableFailures"`
	RateLimited        int     `json:"rateLimited"`
	Upstream5xx        int     `json:"upstream5xx"`
	Timeouts           int     `json:"timeouts"`
	TransportErrors    int     `json:"transportErrors"`
	StreamErrors       int     `json:"streamErrors"`
	InputTokens        int64   `json:"inputTokens"`
	OutputTokens       int64   `json:"outputTokens"`
	TotalTokens        int64   `json:"totalTokens"`
	CacheReadTokens    int64   `json:"cacheReadTokens"`
	CacheHitRate       float64 `json:"cacheHitRate"`
	FirstByteP50Ms     int64   `json:"firstByteP50Ms,omitempty"`
	FirstByteP95Ms     int64   `json:"firstByteP95Ms,omitempty"`
	DurationP50Ms      int64   `json:"durationP50Ms,omitempty"`
	DurationP95Ms      int64   `json:"durationP95Ms,omitempty"`
	SampledProviders   int     `json:"sampledProviders"`
	LowSampleProviders int     `json:"lowSampleProviders"`
}

type ProviderHealthView struct {
	Provider             string                `json:"provider"`
	Name                 string                `json:"name,omitempty"`
	Protocol             string                `json:"protocol,omitempty"`
	Role                 string                `json:"role"`
	Configured           bool                  `json:"configured"`
	Disabled             bool                  `json:"disabled,omitempty"`
	SampleLevel          string                `json:"sampleLevel"`
	RequestCount         int                   `json:"requestCount"`
	AttemptCount         int                   `json:"attemptCount"`
	PrimaryAttempts      int                   `json:"primaryAttempts"`
	BackupAttempts       int                   `json:"backupAttempts"`
	Success              int                   `json:"success"`
	FinalSuccess         int                   `json:"finalSuccess"`
	TerminalFailures     int                   `json:"terminalFailures"`
	RetryableFailures    int                   `json:"retryableFailures"`
	Skipped              int                   `json:"skipped"`
	RateLimited          int                   `json:"rateLimited"`
	Upstream5xx          int                   `json:"upstream5xx"`
	Upstream4xx          int                   `json:"upstream4xx"`
	Timeouts             int                   `json:"timeouts"`
	TransportErrors      int                   `json:"transportErrors"`
	StreamErrors         int                   `json:"streamErrors"`
	EmptyResponses       int                   `json:"emptyResponses"`
	OtherFailures        int                   `json:"otherFailures"`
	FailoverInvolved     int                   `json:"failoverInvolved"`
	InputTokens          int64                 `json:"inputTokens"`
	OutputTokens         int64                 `json:"outputTokens"`
	TotalTokens          int64                 `json:"totalTokens"`
	CacheReadTokens      int64                 `json:"cacheReadTokens"`
	CacheHitRate         float64               `json:"cacheHitRate"`
	FirstByteP50Ms       int64                 `json:"firstByteP50Ms,omitempty"`
	FirstByteP95Ms       int64                 `json:"firstByteP95Ms,omitempty"`
	DurationP50Ms        int64                 `json:"durationP50Ms,omitempty"`
	DurationP95Ms        int64                 `json:"durationP95Ms,omitempty"`
	ObservedSuccessRate  float64               `json:"observedSuccessRate"`
	RetryableFailureRate float64               `json:"retryableFailureRate"`
	Aliases              []ProviderHealthAlias `json:"aliases,omitempty"`
}

type ProviderHealthAlias struct {
	Alias       string `json:"alias"`
	Model       string `json:"model,omitempty"`
	Role        string `json:"role"`
	TargetIndex int    `json:"targetIndex"`
	Attempts    int    `json:"attempts"`
	Success     int    `json:"success"`
}

type providerHealthAccum struct {
	view           ProviderHealthView
	requestIDs     map[uint64]bool
	failoverIDs    map[uint64]bool
	firstByteMs    []int64
	durationMs     []int64
	aliasStats     map[string]*ProviderHealthAlias
	roleHasPrimary bool
	roleHasBackup  bool
}

type providerTargetInfo struct {
	alias string
	model string
	index int
}

func (s *Service) QueryProviderHealth(ctx context.Context, in ProviderHealthInput) (ProviderHealthResult, error) {
	startedFrom, err := parseOptionalTimestamp(in.StartedFrom)
	if err != nil {
		return ProviderHealthResult{}, fmt.Errorf("parse startedFrom: %w", err)
	}
	startedTo, err := parseOptionalTimestamp(in.StartedTo)
	if err != nil {
		return ProviderHealthResult{}, fmt.Errorf("parse startedTo: %w", err)
	}
	cfg, err := s.loadConfig()
	if err != nil {
		return ProviderHealthResult{}, err
	}
	traces, err := s.traces.QueryAll(ctx, proxy.TraceQuery{
		Aliases:     in.Aliases,
		StartedFrom: startedFrom,
		StartedTo:   startedTo,
	})
	if err != nil {
		return ProviderHealthResult{}, err
	}
	providerFilter := normalizeStringSet(in.Providers)
	aliases := cfg.AvailableAliasNames()
	sort.Strings(aliases)
	providers := providerIDs(cfg)
	accums := initializeProviderHealthAccums(cfg)
	targets := providerTargetLookup(cfg)
	for _, trace := range traces {
		aggregateTraceHealth(accums, targets, trace)
	}
	items := providerHealthViews(accums, providerFilter)
	summary := summarizeProviderHealth(items, traces, accums, providerFilter)
	warnings := []string{
		"Observed from routed traffic only. Backup providers can have low sample counts because aliases try earlier targets first.",
	}
	return ProviderHealthResult{
		Summary:            summary,
		Providers:          items,
		AvailableAliases:   aliases,
		AvailableProviders: providers,
		Warnings:           warnings,
	}, nil
}

func initializeProviderHealthAccums(cfg *config.Config) map[string]*providerHealthAccum {
	out := map[string]*providerHealthAccum{}
	for _, provider := range cfg.Providers {
		role := providerConfiguredRole(cfg, provider.ID)
		out[provider.ID] = &providerHealthAccum{
			view: ProviderHealthView{
				Provider:    provider.ID,
				Name:        provider.Name,
				Protocol:    config.NormalizeProviderProtocol(provider.Protocol),
				Role:        role,
				Configured:  true,
				Disabled:    provider.Disabled,
				SampleLevel: "none",
			},
			requestIDs:  map[uint64]bool{},
			failoverIDs: map[uint64]bool{},
			aliasStats:  map[string]*ProviderHealthAlias{},
		}
	}
	return out
}

func providerConfiguredRole(cfg *config.Config, providerID string) string {
	hasPrimary := false
	hasBackup := false
	for _, alias := range cfg.Aliases {
		for index, target := range alias.Targets {
			if target.Provider != providerID {
				continue
			}
			if index == 0 {
				hasPrimary = true
			} else {
				hasBackup = true
			}
		}
	}
	switch {
	case hasPrimary && hasBackup:
		return "mixed"
	case hasPrimary:
		return "primary"
	case hasBackup:
		return "backup"
	default:
		return "unbound"
	}
}

func providerTargetLookup(cfg *config.Config) map[string]providerTargetInfo {
	out := map[string]providerTargetInfo{}
	for _, alias := range cfg.Aliases {
		for index, target := range alias.Targets {
			key := providerTargetKey(alias.Alias, target.Provider, target.Model)
			out[key] = providerTargetInfo{alias: alias.Alias, model: target.Model, index: index}
		}
	}
	return out
}

func aggregateTraceHealth(accums map[string]*providerHealthAccum, targets map[string]providerTargetInfo, trace proxy.RequestTrace) {
	seenProviders := map[string]bool{}
	traceFailover := trace.Failover || len(trace.Attempts) > 1 || trace.AttemptCount > 1
	for _, attempt := range trace.Attempts {
		providerID := strings.TrimSpace(attempt.Provider)
		if providerID == "" {
			continue
		}
		accum := ensureProviderHealthAccum(accums, providerID)
		accum.view.AttemptCount++
		seenProviders[providerID] = true
		if traceFailover {
			accum.failoverIDs[trace.ID] = true
		}
		info, hasTarget := targets[providerTargetKey(trace.Alias, providerID, attempt.Model)]
		role := "unknown"
		if hasTarget {
			role = roleFromTargetIndex(info.index)
			aliasKey := fmt.Sprintf("%s\x00%d\x00%s", info.alias, info.index, info.model)
			aliasStat := accum.aliasStats[aliasKey]
			if aliasStat == nil {
				aliasStat = &ProviderHealthAlias{Alias: info.alias, Model: info.model, Role: role, TargetIndex: info.index}
				accum.aliasStats[aliasKey] = aliasStat
			}
			aliasStat.Attempts++
			if attempt.Success {
				aliasStat.Success++
			}
		}
		if role == "primary" || (!hasTarget && attempt.Attempt == 1) {
			accum.view.PrimaryAttempts++
			accum.roleHasPrimary = true
		} else if role == "backup" || (!hasTarget && attempt.Attempt > 1) {
			accum.view.BackupAttempts++
			accum.roleHasBackup = true
		}
		if attempt.DurationMs > 0 {
			accum.durationMs = append(accum.durationMs, attempt.DurationMs)
		}
		if attempt.FirstByteMs > 0 {
			accum.firstByteMs = append(accum.firstByteMs, attempt.FirstByteMs)
		}
		if attempt.Success {
			accum.view.Success++
			continue
		}
		if attempt.Skipped {
			accum.view.Skipped++
			continue
		}
		if attempt.Retryable {
			accum.view.RetryableFailures++
		} else {
			accum.view.TerminalFailures++
		}
		switch providerHealthFailureKind(attempt) {
		case "rate_limited":
			accum.view.RateLimited++
		case "upstream_5xx":
			accum.view.Upstream5xx++
		case "upstream_4xx":
			accum.view.Upstream4xx++
		case "timeout":
			accum.view.Timeouts++
		case "transport_error":
			accum.view.TransportErrors++
		case "stream_error":
			accum.view.StreamErrors++
		case "empty_response":
			accum.view.EmptyResponses++
		default:
			accum.view.OtherFailures++
		}
	}
	for providerID := range seenProviders {
		accums[providerID].requestIDs[trace.ID] = true
	}
	if trace.FinalProvider != "" {
		if accum := accums[trace.FinalProvider]; accum != nil && trace.Success {
			if len(trace.Attempts) == 0 && !seenProviders[trace.FinalProvider] {
				accum.view.AttemptCount++
				accum.view.Success++
				accum.view.PrimaryAttempts++
				accum.roleHasPrimary = true
				accum.requestIDs[trace.ID] = true
				if trace.FirstByteMs > 0 {
					accum.firstByteMs = append(accum.firstByteMs, trace.FirstByteMs)
				}
				if trace.DurationMs > 0 {
					accum.durationMs = append(accum.durationMs, trace.DurationMs)
				}
			}
			accum.view.FinalSuccess++
			accum.view.InputTokens += trace.InputTokens
			accum.view.OutputTokens += trace.OutputTokens
			accum.view.TotalTokens += trace.InputTokens + trace.OutputTokens
			if trace.Usage.CacheReadTokens != nil {
				accum.view.CacheReadTokens += *trace.Usage.CacheReadTokens
			}
		}
	}
}

func ensureProviderHealthAccum(accums map[string]*providerHealthAccum, providerID string) *providerHealthAccum {
	if accum := accums[providerID]; accum != nil {
		return accum
	}
	accum := &providerHealthAccum{
		view: ProviderHealthView{
			Provider:    providerID,
			Role:        "unknown",
			SampleLevel: "none",
		},
		requestIDs:  map[uint64]bool{},
		failoverIDs: map[uint64]bool{},
		aliasStats:  map[string]*ProviderHealthAlias{},
	}
	accums[providerID] = accum
	return accum
}

func providerHealthViews(accums map[string]*providerHealthAccum, providerFilter map[string]bool) []ProviderHealthView {
	items := make([]ProviderHealthView, 0, len(accums))
	for providerID, accum := range accums {
		if len(providerFilter) > 0 && !providerFilter[providerID] {
			continue
		}
		view := accum.view
		view.RequestCount = len(accum.requestIDs)
		view.FailoverInvolved = len(accum.failoverIDs)
		view.FirstByteP50Ms = percentileInt64(accum.firstByteMs, 50)
		view.FirstByteP95Ms = percentileInt64(accum.firstByteMs, 95)
		view.DurationP50Ms = percentileInt64(accum.durationMs, 50)
		view.DurationP95Ms = percentileInt64(accum.durationMs, 95)
		view.ObservedSuccessRate = ratio(view.Success, view.AttemptCount)
		view.RetryableFailureRate = ratio(view.RetryableFailures, view.AttemptCount)
		view.CacheHitRate = cacheHitRate(view.CacheReadTokens, view.InputTokens)
		view.SampleLevel = providerSampleLevel(view.AttemptCount)
		if !view.Configured {
			view.Role = observedProviderRole(accum)
		}
		view.Aliases = providerHealthAliasViews(accum.aliasStats)
		items = append(items, view)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].AttemptCount != items[j].AttemptCount {
			return items[i].AttemptCount > items[j].AttemptCount
		}
		return items[i].Provider < items[j].Provider
	})
	return items
}

func providerHealthAliasViews(in map[string]*ProviderHealthAlias) []ProviderHealthAlias {
	items := make([]ProviderHealthAlias, 0, len(in))
	for _, item := range in {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Alias != items[j].Alias {
			return items[i].Alias < items[j].Alias
		}
		if items[i].TargetIndex != items[j].TargetIndex {
			return items[i].TargetIndex < items[j].TargetIndex
		}
		return items[i].Model < items[j].Model
	})
	return items
}

func summarizeProviderHealth(items []ProviderHealthView, traces []proxy.RequestTrace, accums map[string]*providerHealthAccum, providerFilter map[string]bool) ProviderHealthSummary {
	summary := ProviderHealthSummary{}
	requestIDs := map[uint64]bool{}
	firstByteMs := []int64{}
	durationMs := []int64{}
	for _, item := range items {
		if item.AttemptCount > 0 {
			summary.SampledProviders++
			if item.AttemptCount < providerHealthLowSampleThreshold {
				summary.LowSampleProviders++
			}
		}
		summary.AttemptCount += item.AttemptCount
		summary.RetryableFailures += item.RetryableFailures
		summary.RateLimited += item.RateLimited
		summary.Upstream5xx += item.Upstream5xx
		summary.Timeouts += item.Timeouts
		summary.TransportErrors += item.TransportErrors
		summary.StreamErrors += item.StreamErrors
		summary.InputTokens += item.InputTokens
		summary.OutputTokens += item.OutputTokens
		summary.TotalTokens += item.TotalTokens
		summary.CacheReadTokens += item.CacheReadTokens
	}
	for _, trace := range traces {
		if !traceMatchesProviderFilter(trace, providerFilter) {
			continue
		}
		requestIDs[trace.ID] = true
		if trace.Success {
			summary.Success++
		} else {
			summary.Failed++
		}
		if trace.Failover || len(trace.Attempts) > 1 || trace.AttemptCount > 1 {
			summary.Failover++
		}
		for _, attempt := range trace.Attempts {
			if len(providerFilter) > 0 && !providerFilter[attempt.Provider] {
				continue
			}
			if attempt.FirstByteMs > 0 {
				firstByteMs = append(firstByteMs, attempt.FirstByteMs)
			}
			if attempt.DurationMs > 0 {
				durationMs = append(durationMs, attempt.DurationMs)
			}
		}
	}
	summary.RequestCount = len(requestIDs)
	if len(providerFilter) > 0 {
		requestIDs = map[uint64]bool{}
		for providerID := range providerFilter {
			if accum := accums[providerID]; accum != nil {
				for id := range accum.requestIDs {
					requestIDs[id] = true
				}
			}
		}
		summary.RequestCount = len(requestIDs)
	}
	summary.FirstByteP50Ms = percentileInt64(firstByteMs, 50)
	summary.FirstByteP95Ms = percentileInt64(firstByteMs, 95)
	summary.DurationP50Ms = percentileInt64(durationMs, 50)
	summary.DurationP95Ms = percentileInt64(durationMs, 95)
	summary.CacheHitRate = cacheHitRate(summary.CacheReadTokens, summary.InputTokens)
	return summary
}

func traceMatchesProviderFilter(trace proxy.RequestTrace, providerFilter map[string]bool) bool {
	if len(providerFilter) == 0 {
		return true
	}
	for _, attempt := range trace.Attempts {
		if providerFilter[attempt.Provider] {
			return true
		}
	}
	return trace.FinalProvider != "" && providerFilter[trace.FinalProvider]
}

func providerIDs(cfg *config.Config) []string {
	items := make([]string, 0, len(cfg.Providers))
	for _, provider := range cfg.Providers {
		if strings.TrimSpace(provider.ID) != "" {
			items = append(items, provider.ID)
		}
	}
	sort.Strings(items)
	return items
}

func normalizeStringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out[trimmed] = true
		}
	}
	return out
}

func providerTargetKey(alias, provider, model string) string {
	return alias + "\x00" + provider + "\x00" + model
}

func roleFromTargetIndex(index int) string {
	if index == 0 {
		return "primary"
	}
	return "backup"
}

func observedProviderRole(accum *providerHealthAccum) string {
	switch {
	case accum.roleHasPrimary && accum.roleHasBackup:
		return "mixed"
	case accum.roleHasPrimary:
		return "primary"
	case accum.roleHasBackup:
		return "backup"
	default:
		return "unknown"
	}
}

func providerSampleLevel(attempts int) string {
	switch {
	case attempts <= 0:
		return "none"
	case attempts < providerHealthLowSampleThreshold:
		return "low"
	default:
		return "ok"
	}
}

func providerHealthFailureKind(attempt proxy.TraceAttempt) string {
	result := strings.ToLower(strings.TrimSpace(attempt.Result))
	switch {
	case attempt.StatusCode == 429:
		return "rate_limited"
	case attempt.StatusCode >= 500:
		return "upstream_5xx"
	case attempt.StatusCode >= 400:
		return "upstream_4xx"
	case strings.Contains(result, "timeout"):
		return "timeout"
	case result == "transport_error":
		return "transport_error"
	case result == "stream_error" || result == "downstream_write_error":
		return "stream_error"
	case result == "empty_response":
		return "empty_response"
	default:
		return "other"
	}
}

func percentileInt64(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	items := append([]int64(nil), values...)
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	index := int(math.Ceil(float64(percentile)/100*float64(len(items)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(items) {
		index = len(items) - 1
	}
	return items[index]
}

func ratio(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func cacheHitRate(cacheReadTokens, inputTokens int64) float64 {
	denominator := cacheReadTokens + inputTokens
	if denominator <= 0 {
		return 0
	}
	return float64(cacheReadTokens) / float64(denominator)
}
