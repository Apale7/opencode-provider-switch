export type ThemePreference = 'system' | 'light' | 'dark'

export type LanguagePreference = 'system' | 'en-US' | 'zh-CN'

export type DesktopPrefsView = {
  launchAtLogin: boolean
  minimizeToTray: boolean
  notifications: boolean
  theme: ThemePreference
  language: LanguagePreference
}

export type DesktopPrefsSaveResult = {
  prefs: DesktopPrefsView
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
  enabled: boolean
  targetCount: number
  availableTargetCount: number
  targets: AliasTargetView[]
}

export type AliasUpsertInput = {
  alias: string
  displayName?: string
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
  aliasNames: string[]
  setModel?: string
  setSmallModel?: string
  wouldChange: boolean
}

export type SyncResult = {
  targetPath: string
  aliasNames: string[]
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
