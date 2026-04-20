package app

import "github.com/Apale7/opencode-provider-switch/internal/proxy"

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
	ConnectTimeoutMs        int `json:"connectTimeoutMs"`
	ResponseHeaderTimeoutMs int `json:"responseHeaderTimeoutMs"`
	FirstByteTimeoutMs      int `json:"firstByteTimeoutMs"`
	RequestReadTimeoutMs    int `json:"requestReadTimeoutMs"`
	StreamIdleTimeoutMs     int `json:"streamIdleTimeoutMs"`
}

type ProxySettingsInput struct {
	ConnectTimeoutMs        int `json:"connectTimeoutMs"`
	ResponseHeaderTimeoutMs int `json:"responseHeaderTimeoutMs"`
	FirstByteTimeoutMs      int `json:"firstByteTimeoutMs"`
	RequestReadTimeoutMs    int `json:"requestReadTimeoutMs"`
	StreamIdleTimeoutMs     int `json:"streamIdleTimeoutMs"`
}

type ProxySettingsSaveResult struct {
	Settings ProxySettingsView `json:"settings"`
	Warnings []string          `json:"warnings,omitempty"`
}

type RequestTrace struct {
	ID             uint64            `json:"id"`
	StartedAt      string            `json:"startedAt"`
	FinishedAt     string            `json:"finishedAt,omitempty"`
	DurationMs     int64             `json:"durationMs"`
	FirstByteMs    int64             `json:"firstByteMs,omitempty"`
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

type ProviderView struct {
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	Protocol     string            `json:"protocol"`
	BaseURL      string            `json:"baseUrl"`
	APIKeySet    bool              `json:"apiKeySet"`
	APIKeyMasked string            `json:"apiKeyMasked,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Models       []string          `json:"models,omitempty"`
	ModelsSource string            `json:"modelsSource,omitempty"`
	Disabled     bool              `json:"disabled"`
}

type ProviderSaveResult struct {
	Provider ProviderView `json:"provider"`
	Warnings []string     `json:"warnings,omitempty"`
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
	Message string `json:"message"`
}

type DoctorReport struct {
	OK                  bool          `json:"ok"`
	Issues              []DoctorIssue `json:"issues"`
	SyncProtocols       []string      `json:"syncProtocols"`
	ConfigPath          string        `json:"configPath"`
	ProviderCount       int           `json:"providerCount"`
	AliasCount          int           `json:"aliasCount"`
	ProxyBindAddress    string        `json:"proxyBindAddress"`
	OpenCodeTargetPath  string        `json:"openCodeTargetPath"`
	OpenCodeTargetFound bool          `json:"openCodeTargetFound"`
}

type DoctorRunResult struct {
	Report DoctorReport `json:"report"`
	Error  string       `json:"error,omitempty"`
}

type SyncInput struct {
	Target        string `json:"target,omitempty"`
	SetModel      string `json:"setModel,omitempty"`
	SetSmallModel string `json:"setSmallModel,omitempty"`
	DryRun        bool   `json:"dryRun"`
}

type SyncPreview struct {
	TargetPath    string               `json:"targetPath"`
	Protocols     []SyncedProviderView `json:"protocols"`
	SetModel      string               `json:"setModel,omitempty"`
	SetSmallModel string               `json:"setSmallModel,omitempty"`
	WouldChange   bool                 `json:"wouldChange"`
}

type SyncResult struct {
	TargetPath    string               `json:"targetPath"`
	Protocols     []SyncedProviderView `json:"protocols"`
	Changed       bool                 `json:"changed"`
	DryRun        bool                 `json:"dryRun"`
	SetModel      string               `json:"setModel,omitempty"`
	SetSmallModel string               `json:"setSmallModel,omitempty"`
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
	ID           string            `json:"id"`
	Name         string            `json:"name,omitempty"`
	Protocol     string            `json:"protocol"`
	BaseURL      string            `json:"baseUrl"`
	APIKey       string            `json:"apiKey,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Disabled     bool              `json:"disabled"`
	SkipModels   bool              `json:"skipModels"`
	ClearHeaders bool              `json:"clearHeaders"`
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
