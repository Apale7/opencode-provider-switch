export type ThemePreference = 'system' | 'light' | 'dark'

export type LanguagePreference = 'system' | 'en-US' | 'zh-CN'

export type ProviderProtocol = 'openai-responses' | 'anthropic-messages'

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

export type RequestTrace = {
  id: number
  startedAt: string
  finishedAt?: string
  durationMs: number
  firstByteMs?: number
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

export type ProviderUpsertInput = {
  id: string
  name?: string
  protocol: ProviderProtocol
  baseUrl: string
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

export type DoctorIssue = {
  message: string
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
}

export type SyncPreview = {
  targetPath: string
  protocols: SyncedProviderView[]
  setModel?: string
  setSmallModel?: string
  wouldChange: boolean
}

export type SyncResult = {
  targetPath: string
  protocols: SyncedProviderView[]
  changed: boolean
  dryRun: boolean
  setModel?: string
  setSmallModel?: string
}

export type MetaView = {
  version: string
  shell: string
  url?: string
}
