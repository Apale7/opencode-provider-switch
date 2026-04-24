package app

import (
	"encoding/json"

	"github.com/Apale7/opencode-provider-switch/internal/proxy"
	"github.com/Apale7/opencode-provider-switch/internal/routing"
)

type Overview struct {
	ConfigPath       string           `json:"configPath"`
	ProviderCount    int              `json:"providerCount"`
	AliasCount       int              `json:"aliasCount"`
	AvailableAliases []string         `json:"availableAliases"`
	Proxy            ProxyStatusView  `json:"proxy"`
	Desktop          DesktopPrefsView `json:"desktop"`
}

type ProxyStatusView struct {
	Running     bool   `json:"running"`
	BindAddress string `json:"bindAddress"`
	StartedAt   string `json:"startedAt,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

type ProxySettingsView struct {
	ConnectTimeoutMs        int                      `json:"connectTimeoutMs"`
	ResponseHeaderTimeoutMs int                      `json:"responseHeaderTimeoutMs"`
	FirstByteTimeoutMs      int                      `json:"firstByteTimeoutMs"`
	RequestReadTimeoutMs    int                      `json:"requestReadTimeoutMs"`
	StreamIdleTimeoutMs     int                      `json:"streamIdleTimeoutMs"`
	Routing                 ProxyRoutingSettingsView `json:"routing"`
}

type ProxySettingsInput struct {
	ConnectTimeoutMs        int                       `json:"connectTimeoutMs"`
	ResponseHeaderTimeoutMs int                       `json:"responseHeaderTimeoutMs"`
	FirstByteTimeoutMs      int                       `json:"firstByteTimeoutMs"`
	RequestReadTimeoutMs    int                       `json:"requestReadTimeoutMs"`
	StreamIdleTimeoutMs     int                       `json:"streamIdleTimeoutMs"`
	Routing                 ProxyRoutingSettingsInput `json:"routing"`
}

type ProxySettingsSaveResult struct {
	Settings ProxySettingsView `json:"settings"`
	Warnings []string          `json:"warnings,omitempty"`
}

type ProxyRoutingSettingsView struct {
	Strategy    string                      `json:"strategy"`
	Params      map[string]any              `json:"params,omitempty"`
	Descriptors []RoutingStrategyDescriptor `json:"descriptors,omitempty"`
}

type ProxyRoutingSettingsInput struct {
	Strategy string          `json:"strategy"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type RoutingStrategyDescriptor struct {
	Name        string                     `json:"name"`
	DisplayName string                     `json:"displayName"`
	Description string                     `json:"description,omitempty"`
	Defaults    map[string]any             `json:"defaults,omitempty"`
	Parameters  []RoutingStrategyParamSpec `json:"parameters,omitempty"`
}

type RoutingStrategyParamSpec struct {
	Key          string   `json:"key"`
	Type         string   `json:"type"`
	Required     bool     `json:"required"`
	DefaultValue any      `json:"defaultValue,omitempty"`
	Description  string   `json:"description,omitempty"`
	Enum         []string `json:"enum,omitempty"`
	Min          *float64 `json:"min,omitempty"`
	Max          *float64 `json:"max,omitempty"`
}

func routingSettingsView(cfg routing.Config) ProxyRoutingSettingsView {
	params, _ := routing.ResolveParams(cfg)
	descriptors := routing.ListDescriptors()
	items := make([]RoutingStrategyDescriptor, 0, len(descriptors))
	for _, descriptor := range descriptors {
		parameters := make([]RoutingStrategyParamSpec, 0, len(descriptor.Parameters))
		for _, parameter := range descriptor.Parameters {
			parameters = append(parameters, RoutingStrategyParamSpec{
				Key:          parameter.Key,
				Type:         parameter.Type,
				Required:     parameter.Required,
				DefaultValue: parameter.DefaultValue,
				Description:  parameter.Description,
				Enum:         append([]string(nil), parameter.Enum...),
				Min:          parameter.Min,
				Max:          parameter.Max,
			})
		}
		items = append(items, RoutingStrategyDescriptor{
			Name:        descriptor.Name,
			DisplayName: descriptor.DisplayName,
			Description: descriptor.Description,
			Defaults:    descriptor.Defaults,
			Parameters:  parameters,
		})
	}
	return ProxyRoutingSettingsView{
		Strategy:    routing.NormalizeConfig(cfg).Strategy,
		Params:      params,
		Descriptors: items,
	}
}

type RequestTrace struct {
	ID             uint64            `json:"id"`
	StartedAt      string            `json:"startedAt"`
	FinishedAt     string            `json:"finishedAt,omitempty"`
	DurationMs     int64             `json:"durationMs"`
	FirstByteMs    int64             `json:"firstByteMs,omitempty"`
	Usage          TraceUsage        `json:"usage,omitempty"`
	InputTokens    int64             `json:"inputTokens,omitempty"`
	OutputTokens   int64             `json:"outputTokens,omitempty"`
	Protocol       string            `json:"protocol"`
	RawModel       string            `json:"rawModel,omitempty"`
	Alias          string            `json:"alias,omitempty"`
	Stream         bool              `json:"stream"`
	Success        bool              `json:"success"`
	StatusCode     int               `json:"statusCode,omitempty"`
	Error          string            `json:"error,omitempty"`
	FinalProvider  string            `json:"finalProvider,omitempty"`
	FinalModel     string            `json:"finalModel,omitempty"`
	FinalURL       string            `json:"finalUrl,omitempty"`
	Failover       bool              `json:"failover"`
	AttemptCount   int               `json:"attemptCount"`
	RequestHeaders map[string]string `json:"requestHeaders,omitempty"`
	RequestParams  any               `json:"requestParams,omitempty"`
	Attempts       []TraceAttempt    `json:"attempts"`
}

type TraceUsage struct {
	RawInputTokens     *int64   `json:"rawInputTokens,omitempty"`
	RawOutputTokens    *int64   `json:"rawOutputTokens,omitempty"`
	RawTotalTokens     *int64   `json:"rawTotalTokens,omitempty"`
	InputTokens        *int64   `json:"inputTokens,omitempty"`
	OutputTokens       *int64   `json:"outputTokens,omitempty"`
	ReasoningTokens    *int64   `json:"reasoningTokens,omitempty"`
	CacheReadTokens    *int64   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens   *int64   `json:"cacheWriteTokens,omitempty"`
	CacheWrite1HTokens *int64   `json:"cacheWrite1hTokens,omitempty"`
	EstimatedCost      *float64 `json:"estimatedCost,omitempty"`
	Source             string   `json:"source,omitempty"`
	Precision          string   `json:"precision,omitempty"`
	Notes              []string `json:"notes,omitempty"`
}

type RequestTraceListInput struct {
	Page           int      `json:"page"`
	PageSize       int      `json:"pageSize"`
	Aliases        []string `json:"aliases,omitempty"`
	FailoverCounts []int    `json:"failoverCounts,omitempty"`
	StatusCodes    []int    `json:"statusCodes,omitempty"`
}

type RequestTraceListResult struct {
	Items                   []RequestTrace `json:"items"`
	Total                   int            `json:"total"`
	Page                    int            `json:"page"`
	PageSize                int            `json:"pageSize"`
	AvailableAliases        []string       `json:"availableAliases,omitempty"`
	AvailableFailoverCounts []int          `json:"availableFailoverCounts,omitempty"`
	AvailableStatusCodes    []int          `json:"availableStatusCodes,omitempty"`
}

type TraceAttempt struct {
	Attempt         int               `json:"attempt"`
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	URL             string            `json:"url,omitempty"`
	StartedAt       string            `json:"startedAt"`
	DurationMs      int64             `json:"durationMs"`
	FirstByteMs     int64             `json:"firstByteMs,omitempty"`
	StatusCode      int               `json:"statusCode,omitempty"`
	Success         bool              `json:"success"`
	Retryable       bool              `json:"retryable"`
	Skipped         bool              `json:"skipped"`
	Result          string            `json:"result,omitempty"`
	Error           string            `json:"error,omitempty"`
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`
	RequestParams   any               `json:"requestParams,omitempty"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	ResponseBody    string            `json:"responseBody,omitempty"`
}

func requestTraceView(trace proxy.RequestTrace) RequestTrace {
	attempts := make([]TraceAttempt, 0, len(trace.Attempts))
	for _, attempt := range trace.Attempts {
		attempts = append(attempts, traceAttemptView(attempt))
	}
	return RequestTrace{
		ID:             trace.ID,
		StartedAt:      formatTimestamp(trace.StartedAt),
		FinishedAt:     formatTimestamp(trace.FinishedAt),
		DurationMs:     trace.DurationMs,
		FirstByteMs:    trace.FirstByteMs,
		Usage:          traceUsageView(trace.Usage),
		InputTokens:    trace.InputTokens,
		OutputTokens:   trace.OutputTokens,
		Protocol:       trace.Protocol,
		RawModel:       trace.RawModel,
		Alias:          trace.Alias,
		Stream:         trace.Stream,
		Success:        trace.Success,
		StatusCode:     trace.StatusCode,
		Error:          trace.Error,
		FinalProvider:  trace.FinalProvider,
		FinalModel:     trace.FinalModel,
		FinalURL:       trace.FinalURL,
		Failover:       trace.Failover,
		AttemptCount:   trace.AttemptCount,
		RequestHeaders: trace.RequestHeaders,
		RequestParams:  trace.RequestParams,
		Attempts:       attempts,
	}
}

func traceUsageView(usage proxy.TraceUsage) TraceUsage {
	return TraceUsage{
		RawInputTokens:     cloneInt64Ptr(usage.RawInputTokens),
		RawOutputTokens:    cloneInt64Ptr(usage.RawOutputTokens),
		RawTotalTokens:     cloneInt64Ptr(usage.RawTotalTokens),
		InputTokens:        cloneInt64Ptr(usage.InputTokens),
		OutputTokens:       cloneInt64Ptr(usage.OutputTokens),
		ReasoningTokens:    cloneInt64Ptr(usage.ReasoningTokens),
		CacheReadTokens:    cloneInt64Ptr(usage.CacheReadTokens),
		CacheWriteTokens:   cloneInt64Ptr(usage.CacheWriteTokens),
		CacheWrite1HTokens: cloneInt64Ptr(usage.CacheWrite1HTokens),
		Source:             usage.Source,
		Precision:          usage.Precision,
		Notes:              append([]string(nil), usage.Notes...),
	}
}

func cloneInt64Ptr(in *int64) *int64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneFloat64Ptr(in *float64) *float64 {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func traceAttemptView(attempt proxy.TraceAttempt) TraceAttempt {
	return TraceAttempt{
		Attempt:         attempt.Attempt,
		Provider:        attempt.Provider,
		Model:           attempt.Model,
		URL:             attempt.URL,
		StartedAt:       formatTimestamp(attempt.StartedAt),
		DurationMs:      attempt.DurationMs,
		FirstByteMs:     attempt.FirstByteMs,
		StatusCode:      attempt.StatusCode,
		Success:         attempt.Success,
		Retryable:       attempt.Retryable,
		Skipped:         attempt.Skipped,
		Result:          attempt.Result,
		Error:           attempt.Error,
		RequestHeaders:  attempt.RequestHeaders,
		RequestParams:   attempt.RequestParams,
		ResponseHeaders: attempt.ResponseHeaders,
		ResponseBody:    attempt.ResponseBody,
	}
}

func requestTraceListResultView(result proxy.TraceQueryResult) RequestTraceListResult {
	items := make([]RequestTrace, 0, len(result.Items))
	for _, trace := range result.Items {
		items = append(items, requestTraceView(trace))
	}
	return RequestTraceListResult{
		Items:                   items,
		Total:                   result.Total,
		Page:                    result.Page,
		PageSize:                result.PageSize,
		AvailableAliases:        append([]string(nil), result.AvailableAliases...),
		AvailableFailoverCounts: append([]int(nil), result.AvailableFailoverCounts...),
		AvailableStatusCodes:    append([]int(nil), result.AvailableStatusCodes...),
	}
}

type ProviderView struct {
	ID              string            `json:"id"`
	Name            string            `json:"name,omitempty"`
	Protocol        string            `json:"protocol"`
	BaseURL         string            `json:"baseUrl"`
	BaseURLs        []string          `json:"baseUrls,omitempty"`
	BaseURLStrategy string            `json:"baseUrlStrategy"`
	APIKeySet       bool              `json:"apiKeySet"`
	APIKeyMasked    string            `json:"apiKeyMasked,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Models          []string          `json:"models,omitempty"`
	ModelsSource    string            `json:"modelsSource,omitempty"`
	Disabled        bool              `json:"disabled"`
}

type ProviderSaveResult struct {
	Provider ProviderView `json:"provider"`
	Warnings []string     `json:"warnings,omitempty"`
}

type ProviderRefreshModelsInput struct {
	ID string `json:"id"`
}

type ProviderPingInput struct {
	ID       string            `json:"id,omitempty"`
	Protocol string            `json:"protocol,omitempty"`
	BaseURL  string            `json:"baseUrl"`
	APIKey   string            `json:"apiKey,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
}

type ProviderPingResult struct {
	ID         string `json:"id"`
	BaseURL    string `json:"baseUrl"`
	LatencyMs  int64  `json:"latencyMs"`
	Reachable  bool   `json:"reachable"`
	StatusCode int    `json:"statusCode,omitempty"`
	Error      string `json:"error,omitempty"`
}

type AliasTargetView struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Enabled  bool   `json:"enabled"`
}

type AliasView struct {
	Alias                string            `json:"alias"`
	DisplayName          string            `json:"displayName,omitempty"`
	Protocol             string            `json:"protocol"`
	Enabled              bool              `json:"enabled"`
	TargetCount          int               `json:"targetCount"`
	AvailableTargetCount int               `json:"availableTargetCount"`
	Targets              []AliasTargetView `json:"targets"`
}

type DoctorIssue struct {
	Code             string   `json:"code"`
	Severity         string   `json:"severity"`
	Message          string   `json:"message"`
	Protocol         string   `json:"protocol,omitempty"`
	ProviderKey      string   `json:"providerKey,omitempty"`
	Alias            string   `json:"alias,omitempty"`
	Path             string   `json:"path,omitempty"`
	Directory        string   `json:"directory,omitempty"`
	Expected         string   `json:"expected,omitempty"`
	Actual           string   `json:"actual,omitempty"`
	ActionHint       string   `json:"actionHint,omitempty"`
	AutoFixAvailable bool     `json:"autoFixAvailable,omitempty"`
	Details          []string `json:"details,omitempty"`
	RelatedFields    []string `json:"relatedFields,omitempty"`
}

type OpenCodeProviderSnapshot struct {
	Key                string   `json:"key"`
	Name               string   `json:"name,omitempty"`
	NPM                string   `json:"npm,omitempty"`
	Protocol           string   `json:"protocol,omitempty"`
	BaseURL            string   `json:"baseUrl,omitempty"`
	ModelAliases       []string `json:"modelAliases,omitempty"`
	MissingFields      []string `json:"missingFields,omitempty"`
	UnknownFieldKeys   []string `json:"unknownFieldKeys,omitempty"`
	RawJSONFragment    string   `json:"rawJsonFragment,omitempty"`
	ContractConfigured bool     `json:"contractConfigured"`
}

type OpenCodeFileSnapshot struct {
	TargetPath           string                     `json:"targetPath"`
	Exists               bool                       `json:"exists"`
	Schema               string                     `json:"schema,omitempty"`
	DefaultModel         string                     `json:"defaultModel,omitempty"`
	SmallModel           string                     `json:"smallModel,omitempty"`
	ProviderKeys         []string                   `json:"providerKeys,omitempty"`
	ExpectedProtocols    []string                   `json:"expectedProtocols,omitempty"`
	SyncedProviders      []OpenCodeProviderSnapshot `json:"syncedProviders,omitempty"`
	UnknownTopLevelKeys  []string                   `json:"unknownTopLevelKeys,omitempty"`
	ParseError           string                     `json:"parseError,omitempty"`
	DefaultModelRoutable bool                       `json:"defaultModelRoutable"`
	SmallModelRoutable   bool                       `json:"smallModelRoutable"`
}

type OpenCodeRuntimeModelSnapshot struct {
	ID               string   `json:"id"`
	Name             string   `json:"name,omitempty"`
	ProviderID       string   `json:"providerId,omitempty"`
	ProviderNPM      string   `json:"providerNpm,omitempty"`
	RawJSON          string   `json:"rawJson,omitempty"`
	ExtraFieldKeys   []string `json:"extraFieldKeys,omitempty"`
	OptionKeys       []string `json:"optionKeys,omitempty"`
	Experimental     bool     `json:"experimental,omitempty"`
	Reasoning        bool     `json:"reasoning,omitempty"`
	ToolCall         bool     `json:"toolCall,omitempty"`
	Temperature      bool     `json:"temperature,omitempty"`
	Attachment       bool     `json:"attachment,omitempty"`
	ContextLimit     int64    `json:"contextLimit,omitempty"`
	OutputLimit      int64    `json:"outputLimit,omitempty"`
	ReleaseDate      string   `json:"releaseDate,omitempty"`
	Status           string   `json:"status,omitempty"`
	InputModalities  []string `json:"inputModalities,omitempty"`
	OutputModalities []string `json:"outputModalities,omitempty"`
}

type OpenCodeRuntimeProviderSnapshot struct {
	ID             string                         `json:"id"`
	Name           string                         `json:"name,omitempty"`
	API            string                         `json:"api,omitempty"`
	NPM            string                         `json:"npm,omitempty"`
	Env            []string                       `json:"env,omitempty"`
	ModelIDs       []string                       `json:"modelIds,omitempty"`
	Models         []OpenCodeRuntimeModelSnapshot `json:"models,omitempty"`
	ExtraFieldKeys []string                       `json:"extraFieldKeys,omitempty"`
	RawJSON        string                         `json:"rawJson,omitempty"`
}

type OpenCodeRuntimeSnapshot struct {
	BaseURL               string                            `json:"baseUrl"`
	Directory             string                            `json:"directory,omitempty"`
	Reachable             bool                              `json:"reachable"`
	ConfigLoaded          bool                              `json:"configLoaded"`
	ProvidersLoaded       bool                              `json:"providersLoaded"`
	DefaultModel          string                            `json:"defaultModel,omitempty"`
	SmallModel            string                            `json:"smallModel,omitempty"`
	ProviderKeys          []string                          `json:"providerKeys,omitempty"`
	DefaultProviderModels map[string]string                 `json:"defaultProviderModels,omitempty"`
	Providers             []OpenCodeRuntimeProviderSnapshot `json:"providers,omitempty"`
	ErrorCode             string                            `json:"errorCode,omitempty"`
	ErrorMessage          string                            `json:"errorMessage,omitempty"`
	HTTPStatus            int                               `json:"httpStatus,omitempty"`
	RawConfigJSON         string                            `json:"rawConfigJson,omitempty"`
	RawProvidersJSON      string                            `json:"rawProvidersJson,omitempty"`
	ConfigExtraFieldKeys  []string                          `json:"configExtraFieldKeys,omitempty"`
	ProviderExtraFieldMap map[string][]string               `json:"providerExtraFieldMap,omitempty"`
}

type OpenCodeReconciliationSummary struct {
	AvailableAliases      []string `json:"availableAliases,omitempty"`
	MissingProviders      []string `json:"missingProviders,omitempty"`
	InvalidDefaultModels  []string `json:"invalidDefaultModels,omitempty"`
	CatalogMismatches     []string `json:"catalogMismatches,omitempty"`
	FileOnlyProviders     []string `json:"fileOnlyProviders,omitempty"`
	RuntimeOnlyProviders  []string `json:"runtimeOnlyProviders,omitempty"`
	RuntimeReachable      bool     `json:"runtimeReachable"`
	FileSnapshotAvailable bool     `json:"fileSnapshotAvailable"`
}

type DoctorReport struct {
	OK                  bool                          `json:"ok"`
	Issues              []DoctorIssue                 `json:"issues"`
	SyncProtocols       []string                      `json:"syncProtocols"`
	ConfigPath          string                        `json:"configPath"`
	ProviderCount       int                           `json:"providerCount"`
	AliasCount          int                           `json:"aliasCount"`
	ProxyBindAddress    string                        `json:"proxyBindAddress"`
	OpenCodeTargetPath  string                        `json:"openCodeTargetPath"`
	OpenCodeTargetFound bool                          `json:"openCodeTargetFound"`
	RuntimeBaseURL      string                        `json:"runtimeBaseUrl,omitempty"`
	RuntimeDirectory    string                        `json:"runtimeDirectory,omitempty"`
	FileSnapshot        OpenCodeFileSnapshot          `json:"fileSnapshot"`
	RuntimeSnapshot     OpenCodeRuntimeSnapshot       `json:"runtimeSnapshot"`
	Summary             OpenCodeReconciliationSummary `json:"summary"`
}

type DoctorRunResult struct {
	Report DoctorReport `json:"report"`
	Error  string       `json:"error,omitempty"`
}

type SyncInput struct {
	Target           string `json:"target,omitempty"`
	SetModel         string `json:"setModel,omitempty"`
	SetSmallModel    string `json:"setSmallModel,omitempty"`
	DryRun           bool   `json:"dryRun"`
	RuntimeBaseURL   string `json:"runtimeBaseUrl,omitempty"`
	RuntimeDirectory string `json:"runtimeDirectory,omitempty"`
}

type SyncPreview struct {
	TargetPath       string                        `json:"targetPath"`
	Protocols        []SyncedProviderView          `json:"protocols"`
	SetModel         string                        `json:"setModel,omitempty"`
	SetSmallModel    string                        `json:"setSmallModel,omitempty"`
	WouldChange      bool                          `json:"wouldChange"`
	RuntimeBaseURL   string                        `json:"runtimeBaseUrl,omitempty"`
	RuntimeDirectory string                        `json:"runtimeDirectory,omitempty"`
	FileSnapshot     OpenCodeFileSnapshot          `json:"fileSnapshot"`
	RuntimeSnapshot  OpenCodeRuntimeSnapshot       `json:"runtimeSnapshot"`
	DoctorIssues     []DoctorIssue                 `json:"doctorIssues,omitempty"`
	Summary          OpenCodeReconciliationSummary `json:"summary"`
}

type SyncResult struct {
	TargetPath       string                        `json:"targetPath"`
	Protocols        []SyncedProviderView          `json:"protocols"`
	Changed          bool                          `json:"changed"`
	DryRun           bool                          `json:"dryRun"`
	SetModel         string                        `json:"setModel,omitempty"`
	SetSmallModel    string                        `json:"setSmallModel,omitempty"`
	RuntimeBaseURL   string                        `json:"runtimeBaseUrl,omitempty"`
	RuntimeDirectory string                        `json:"runtimeDirectory,omitempty"`
	FileSnapshot     OpenCodeFileSnapshot          `json:"fileSnapshot"`
	RuntimeSnapshot  OpenCodeRuntimeSnapshot       `json:"runtimeSnapshot"`
	DoctorIssues     []DoctorIssue                 `json:"doctorIssues,omitempty"`
	Summary          OpenCodeReconciliationSummary `json:"summary"`
}

type SyncedProviderView struct {
	Key        string   `json:"key"`
	Protocol   string   `json:"protocol"`
	AliasNames []string `json:"aliasNames"`
}

type DesktopPrefsView struct {
	LaunchAtLogin  bool   `json:"launchAtLogin"`
	AutoStartProxy bool   `json:"autoStartProxy"`
	MinimizeToTray bool   `json:"minimizeToTray"`
	Notifications  bool   `json:"notifications"`
	Theme          string `json:"theme"`
	Language       string `json:"language"`
}

type DesktopPrefsSaveResult struct {
	Prefs    DesktopPrefsView `json:"prefs"`
	Warnings []string         `json:"warnings,omitempty"`
}

type DesktopPrefsInput struct {
	LaunchAtLogin  bool   `json:"launchAtLogin"`
	AutoStartProxy bool   `json:"autoStartProxy"`
	MinimizeToTray bool   `json:"minimizeToTray"`
	Notifications  bool   `json:"notifications"`
	Theme          string `json:"theme"`
	Language       string `json:"language"`
}

type ConfigExportView struct {
	ConfigPath string `json:"configPath"`
	Content    string `json:"content"`
}

type ConfigImportInput struct {
	Content string `json:"content"`
}

type ConfigImportResult struct {
	ConfigPath string   `json:"configPath"`
	Warnings   []string `json:"warnings,omitempty"`
}

type ProviderUpsertInput struct {
	ID              string            `json:"id"`
	Name            string            `json:"name,omitempty"`
	Protocol        string            `json:"protocol"`
	BaseURL         string            `json:"baseUrl"`
	BaseURLs        []string          `json:"baseUrls,omitempty"`
	BaseURLStrategy string            `json:"baseUrlStrategy"`
	APIKey          string            `json:"apiKey,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	Disabled        bool              `json:"disabled"`
	SkipModels      bool              `json:"skipModels"`
	ClearHeaders    bool              `json:"clearHeaders"`
}

type ProviderImportInput struct {
	SourcePath string `json:"sourcePath,omitempty"`
	Overwrite  bool   `json:"overwrite"`
}

type ProviderImportResult struct {
	SourcePath string   `json:"sourcePath"`
	Imported   int      `json:"imported"`
	Skipped    int      `json:"skipped"`
	Warnings   []string `json:"warnings,omitempty"`
}

type ProviderStateInput struct {
	ID       string `json:"id"`
	Disabled bool   `json:"disabled"`
}

type AliasUpsertInput struct {
	Alias       string `json:"alias"`
	DisplayName string `json:"displayName,omitempty"`
	Protocol    string `json:"protocol"`
	Disabled    bool   `json:"disabled"`
}

type AliasTargetInput struct {
	Alias    string `json:"alias"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
}

type AliasTargetRefInput struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type AliasTargetReorderInput struct {
	Alias   string                `json:"alias"`
	Targets []AliasTargetRefInput `json:"targets"`
}
