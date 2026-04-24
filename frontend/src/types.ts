export type ThemePreference = 'system' | 'light' | 'dark'

export type LanguagePreference = 'system' | 'en-US' | 'zh-CN'

export type ProviderProtocol = 'openai-responses' | 'anthropic-messages' | 'openai-compatible'

export type ProviderBaseURLStrategy = 'ordered' | 'latency'

export type SyncedProviderView = {
  key: string
  protocol: ProviderProtocol
  aliasNames: string[]
}

export type DesktopPrefsView = {
  launchAtLogin: boolean
  autoStartProxy: boolean
  minimizeToTray: boolean
  notifications: boolean
  theme: ThemePreference
  language: LanguagePreference
}

export type DesktopPrefsSaveResult = {
  prefs: DesktopPrefsView
  warnings?: string[]
}

export type ProxySettingsView = {
  connectTimeoutMs: number
  responseHeaderTimeoutMs: number
  firstByteTimeoutMs: number
  requestReadTimeoutMs: number
  streamIdleTimeoutMs: number
  routing: ProxyRoutingSettingsView
}

export type ProxyRoutingSettingsView = {
  strategy: string
  params?: Record<string, unknown>
  descriptors?: RoutingStrategyDescriptor[]
}

export type RoutingStrategyDescriptor = {
  name: string
  displayName: string
  description?: string
  defaults?: Record<string, unknown>
  parameters?: RoutingStrategyParamSpec[]
}

export type RoutingStrategyParamSpec = {
  key: string
  type: string
  required: boolean
  defaultValue?: unknown
  description?: string
  enum?: string[]
  min?: number
  max?: number
}

export type ProxySettingsSaveResult = {
  settings: ProxySettingsView
  warnings?: string[]
}

export type ConfigExportView = {
  configPath: string
  content: string
}

export type ConfigImportInput = {
  content: string
}

export type ConfigImportResult = {
  configPath: string
  warnings?: string[]
}

export type ProxyStatusView = {
  running: boolean
  bindAddress: string
  startedAt?: string
  lastError?: string
}

export type TraceAttempt = {
  attempt: number
  provider?: string
  model?: string
  url?: string
  startedAt: string
  durationMs: number
  firstByteMs?: number
  statusCode?: number
  success: boolean
  retryable: boolean
  skipped: boolean
  result?: string
  error?: string
  requestHeaders?: Record<string, string>
  requestParams?: unknown
  responseHeaders?: Record<string, string>
  responseBody?: string
}

export type TraceUsage = {
  rawInputTokens?: number
  rawOutputTokens?: number
  rawTotalTokens?: number
  inputTokens?: number
  outputTokens?: number
  reasoningTokens?: number
  cacheReadTokens?: number
  cacheWriteTokens?: number
  cacheWrite1hTokens?: number
  estimatedCost?: number
  source?: string
  precision?: string
  notes?: string[]
}

export type RequestTrace = {
  id: number
  startedAt: string
  finishedAt?: string
  durationMs: number
  firstByteMs?: number
  usage?: TraceUsage
  inputTokens?: number
  outputTokens?: number
  protocol: ProviderProtocol
  rawModel?: string
  alias?: string
  stream: boolean
  success: boolean
  statusCode?: number
  error?: string
  finalProvider?: string
  finalModel?: string
  finalUrl?: string
  failover: boolean
  attemptCount: number
  requestHeaders?: Record<string, string>
  requestParams?: unknown
  attempts: TraceAttempt[]
}

export type RequestTraceListInput = {
  page: number
  pageSize: number
  aliases?: string[]
  failoverCounts?: number[]
  statusCodes?: number[]
}

export type RequestTraceListResult = {
  items: RequestTrace[]
  total: number
  page: number
  pageSize: number
  availableAliases?: string[]
  availableFailoverCounts?: number[]
  availableStatusCodes?: number[]
}

export type Overview = {
  configPath: string
  providerCount: number
  aliasCount: number
  availableAliases: string[]
  proxy: ProxyStatusView
  desktop: DesktopPrefsView
}

export type ProviderView = {
  id: string
  name?: string
  protocol: ProviderProtocol
  baseUrl: string
  baseUrls?: string[]
  baseUrlStrategy: ProviderBaseURLStrategy
  apiKeySet: boolean
  apiKeyMasked?: string
  headers?: Record<string, string>
  models?: string[]
  modelsSource?: string
  disabled: boolean
}

export type ProviderSaveResult = {
  provider: ProviderView
  warnings?: string[]
}

export type ProviderRefreshModelsInput = {
  id: string
}

export type ProviderPingInput = {
  id?: string
  protocol?: ProviderProtocol
  baseUrl: string
  apiKey?: string
  headers?: Record<string, string>
}

export type ProviderPingResult = {
  id: string
  baseUrl: string
  latencyMs: number
  reachable: boolean
  statusCode?: number
  error?: string
}

export type ProviderUpsertInput = {
  id: string
  name?: string
  protocol: ProviderProtocol
  baseUrl: string
  baseUrls?: string[]
  baseUrlStrategy: ProviderBaseURLStrategy
  apiKey?: string
  headers?: Record<string, string>
  disabled: boolean
  skipModels: boolean
  clearHeaders: boolean
}

export type ProviderStateInput = {
  id: string
  disabled: boolean
}

export type ProviderImportInput = {
  sourcePath?: string
  overwrite: boolean
}

export type ProviderImportResult = {
  sourcePath: string
  imported: number
  skipped: number
  warnings?: string[]
}

export type AliasTargetView = {
  provider: string
  model: string
  enabled: boolean
}

export type AliasView = {
  alias: string
  displayName?: string
  protocol: ProviderProtocol
  enabled: boolean
  targetCount: number
  availableTargetCount: number
  targets: AliasTargetView[]
}

export type AliasUpsertInput = {
  alias: string
  displayName?: string
  protocol: ProviderProtocol
  disabled: boolean
}

export type AliasTargetInput = {
  alias: string
  provider: string
  model: string
  disabled: boolean
}

export type AliasTargetRefInput = {
  provider: string
  model: string
}

export type AliasTargetReorderInput = {
  alias: string
  targets: AliasTargetRefInput[]
}

export type DoctorIssue = {
  code: string
  severity: string
  message: string
  protocol?: string
  providerKey?: string
  alias?: string
  path?: string
  directory?: string
  expected?: string
  actual?: string
  actionHint?: string
  autoFixAvailable?: boolean
  details?: string[]
  relatedFields?: string[]
}

export type OpenCodeProviderSnapshot = {
  key: string
  name?: string
  npm?: string
  protocol?: ProviderProtocol
  baseUrl?: string
  modelAliases?: string[]
  missingFields?: string[]
  unknownFieldKeys?: string[]
  rawJsonFragment?: string
  contractConfigured: boolean
}

export type OpenCodeFileSnapshot = {
  targetPath: string
  exists: boolean
  schema?: string
  defaultModel?: string
  smallModel?: string
  providerKeys?: string[]
  expectedProtocols?: ProviderProtocol[]
  syncedProviders?: OpenCodeProviderSnapshot[]
  unknownTopLevelKeys?: string[]
  parseError?: string
  defaultModelRoutable: boolean
  smallModelRoutable: boolean
}

export type OpenCodeRuntimeModelSnapshot = {
  id: string
  name?: string
  providerId?: string
  providerNpm?: string
  rawJson?: string
  extraFieldKeys?: string[]
  optionKeys?: string[]
  experimental?: boolean
  reasoning?: boolean
  toolCall?: boolean
  temperature?: boolean
  attachment?: boolean
  contextLimit?: number
  outputLimit?: number
  releaseDate?: string
  status?: string
  inputModalities?: string[]
  outputModalities?: string[]
}

export type OpenCodeRuntimeProviderSnapshot = {
  id: string
  name?: string
  api?: string
  npm?: string
  env?: string[]
  modelIds?: string[]
  models?: OpenCodeRuntimeModelSnapshot[]
  extraFieldKeys?: string[]
  rawJson?: string
}

export type OpenCodeRuntimeSnapshot = {
  baseUrl: string
  directory?: string
  reachable: boolean
  configLoaded: boolean
  providersLoaded: boolean
  defaultModel?: string
  smallModel?: string
  providerKeys?: string[]
  defaultProviderModels?: Record<string, string>
  providers?: OpenCodeRuntimeProviderSnapshot[]
  errorCode?: string
  errorMessage?: string
  httpStatus?: number
  rawConfigJson?: string
  rawProvidersJson?: string
  configExtraFieldKeys?: string[]
  providerExtraFieldMap?: Record<string, string[]>
}

export type OpenCodeReconciliationSummary = {
  availableAliases?: string[]
  missingProviders?: string[]
  invalidDefaultModels?: string[]
  catalogMismatches?: string[]
  fileOnlyProviders?: string[]
  runtimeOnlyProviders?: string[]
  runtimeReachable: boolean
  fileSnapshotAvailable: boolean
}

export type DoctorReport = {
  ok: boolean
  issues: DoctorIssue[]
  syncProtocols: ProviderProtocol[]
  configPath: string
  providerCount: number
  aliasCount: number
  proxyBindAddress: string
  openCodeTargetPath: string
  openCodeTargetFound: boolean
  runtimeBaseUrl?: string
  runtimeDirectory?: string
  fileSnapshot: OpenCodeFileSnapshot
  runtimeSnapshot: OpenCodeRuntimeSnapshot
  summary: OpenCodeReconciliationSummary
}

export type DoctorRunResult = {
  report: DoctorReport
  error?: string
}

export type SyncInput = {
  target?: string
  setModel?: string
  setSmallModel?: string
  dryRun?: boolean
  runtimeBaseUrl?: string
  runtimeDirectory?: string
}

export type SyncPreview = {
  targetPath: string
  protocols: SyncedProviderView[]
  setModel?: string
  setSmallModel?: string
  wouldChange: boolean
  runtimeBaseUrl?: string
  runtimeDirectory?: string
  fileSnapshot: OpenCodeFileSnapshot
  runtimeSnapshot: OpenCodeRuntimeSnapshot
  doctorIssues?: DoctorIssue[]
  summary: OpenCodeReconciliationSummary
}

export type SyncResult = {
  targetPath: string
  protocols: SyncedProviderView[]
  changed: boolean
  dryRun: boolean
  setModel?: string
  setSmallModel?: string
  runtimeBaseUrl?: string
  runtimeDirectory?: string
  fileSnapshot: OpenCodeFileSnapshot
  runtimeSnapshot: OpenCodeRuntimeSnapshot
  doctorIssues?: DoctorIssue[]
  summary: OpenCodeReconciliationSummary
}

export type MetaView = {
  version: string
  shell: string
  url?: string
}
