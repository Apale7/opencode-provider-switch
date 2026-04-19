import type {
  AliasTargetInput,
  AliasUpsertInput,
  ConfigExportView,
  ConfigImportInput,
  ConfigImportResult,
  DesktopPrefsSaveResult,
  AliasView,
  DesktopPrefsView,
  DoctorRunResult,
  MetaView,
  Overview,
  ProviderImportInput,
  ProviderImportResult,
  ProviderSaveResult,
  ProviderStateInput,
  ProviderUpsertInput,
  ProviderView,
  ProxySettingsSaveResult,
  ProxySettingsView,
  ProxyStatusView,
  RequestTrace,
  SyncInput,
  SyncPreview,
  SyncResult,
} from './types'

type ApiEnvelope<T> = {
  data: T
  error?: string
}

function isWails(): boolean {
  return typeof window.go?.desktop?.App !== 'undefined'
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  const payload = (await response.json()) as ApiEnvelope<T>
  if (!response.ok) {
    throw new Error(payload.error || 'request failed')
  }
  return payload.data
}

function bridge() {
  const app = window.go?.desktop?.App
  if (!app) {
    throw new Error('Wails bridge unavailable')
  }
  return app
}

export async function getMeta(): Promise<MetaView> {
  if (isWails()) {
    const data = await bridge().Meta()
    return { version: data.version || 'dev', shell: data.shell || 'wails' }
  }
  return http<MetaView>('/api/meta')
}

export async function openExternalURL(url: string): Promise<void> {
  if (isWails()) {
    await bridge().OpenExternalURL(url)
    return
  }
  const target = url.trim()
  if (!target) {
    return
  }
  window.open(target, '_blank', 'noopener,noreferrer')
}

export function getOverview(): Promise<Overview> {
  return isWails() ? bridge().Overview() : http<Overview>('/api/overview')
}

export function exportConfig(): Promise<ConfigExportView> {
  return isWails() ? bridge().ExportConfig() : http<ConfigExportView>('/api/config/export')
}

export function importConfig(input: ConfigImportInput): Promise<ConfigImportResult> {
  return isWails()
    ? bridge().ImportConfig(input)
    : http<ConfigImportResult>('/api/config/import', { method: 'POST', body: JSON.stringify(input) })
}

export function listProviders(): Promise<ProviderView[]> {
  return isWails() ? bridge().Providers() : http<ProviderView[]>('/api/providers')
}

export function listAliases(): Promise<AliasView[]> {
  return isWails() ? bridge().Aliases() : http<AliasView[]>('/api/aliases')
}

export function saveProvider(input: ProviderUpsertInput): Promise<ProviderSaveResult> {
  return isWails()
    ? bridge().SaveProvider(input)
    : http<ProviderSaveResult>('/api/providers', { method: 'POST', body: JSON.stringify(input) })
}

export function setProviderState(input: ProviderStateInput): Promise<ProviderView> {
  return isWails()
    ? bridge().SetProviderState(input)
    : http<ProviderView>('/api/providers/state', { method: 'POST', body: JSON.stringify(input) })
}

export async function deleteProvider(id: string): Promise<void> {
  if (isWails()) {
    await bridge().DeleteProvider(id)
    return
  }
  await http<{ ok: boolean }>('/api/providers/delete', { method: 'POST', body: JSON.stringify({ id }) })
}

export function importProviders(input: ProviderImportInput): Promise<ProviderImportResult> {
  return isWails()
    ? bridge().ImportProviders(input)
    : http<ProviderImportResult>('/api/providers/import', { method: 'POST', body: JSON.stringify(input) })
}

export function saveAlias(input: AliasUpsertInput): Promise<AliasView> {
  return isWails()
    ? bridge().SaveAlias(input)
    : http<AliasView>('/api/aliases', { method: 'POST', body: JSON.stringify(input) })
}

export async function deleteAlias(alias: string): Promise<void> {
  if (isWails()) {
    await bridge().DeleteAlias(alias)
    return
  }
  await http<{ ok: boolean }>('/api/aliases/delete', { method: 'POST', body: JSON.stringify({ alias }) })
}

export function bindAliasTarget(input: AliasTargetInput): Promise<AliasView> {
  return isWails()
    ? bridge().BindTarget(input)
    : http<AliasView>('/api/aliases/bind', { method: 'POST', body: JSON.stringify(input) })
}

export function setAliasTargetState(input: AliasTargetInput): Promise<AliasView> {
  return isWails()
    ? bridge().SetTargetState(input)
    : http<AliasView>('/api/aliases/state', { method: 'POST', body: JSON.stringify(input) })
}

export function unbindAliasTarget(input: AliasTargetInput): Promise<AliasView> {
  return isWails()
    ? bridge().UnbindTarget(input)
    : http<AliasView>('/api/aliases/unbind', { method: 'POST', body: JSON.stringify(input) })
}

export function getDesktopPrefs(): Promise<DesktopPrefsView> {
  return isWails() ? bridge().DesktopPrefs() : http<DesktopPrefsView>('/api/desktop-prefs')
}

export function saveDesktopPrefs(input: DesktopPrefsView): Promise<DesktopPrefsSaveResult> {
  return isWails()
    ? bridge().SavePrefs(input)
    : http<DesktopPrefsSaveResult>('/api/desktop-prefs', { method: 'POST', body: JSON.stringify(input) })
}

export function runDoctor(): Promise<DoctorRunResult> {
  return isWails() ? bridge().DoctorRun() : http<DoctorRunResult>('/api/doctor', { method: 'POST' })
}

export function getProxyStatus(): Promise<ProxyStatusView> {
  return isWails() ? bridge().ProxyStatus() : http<ProxyStatusView>('/api/proxy/status')
}

export function getProxySettings(): Promise<ProxySettingsView> {
  return isWails() ? bridge().ProxySettings() : http<ProxySettingsView>('/api/proxy/settings')
}

export function saveProxySettings(input: ProxySettingsView): Promise<ProxySettingsSaveResult> {
  return isWails()
    ? bridge().SaveProxySettings(input)
    : http<ProxySettingsSaveResult>('/api/proxy/settings', { method: 'POST', body: JSON.stringify(input) })
}

export function listRequestTraces(): Promise<RequestTrace[]> {
  return isWails() ? bridge().RequestTraces(100) : http<RequestTrace[]>('/api/proxy/traces')
}

export function startProxy(): Promise<ProxyStatusView> {
  return isWails() ? bridge().StartProxy() : http<ProxyStatusView>('/api/proxy/start', { method: 'POST' })
}

export function stopProxy(): Promise<ProxyStatusView> {
  return isWails() ? bridge().StopProxy() : http<ProxyStatusView>('/api/proxy/stop', { method: 'POST' })
}

export function previewSync(input: SyncInput): Promise<SyncPreview> {
  return isWails()
    ? bridge().PreviewSync(input)
    : http<SyncPreview>('/api/opencode-sync/preview', { method: 'POST', body: JSON.stringify(input) })
}

export function applySync(input: SyncInput): Promise<SyncResult> {
  return isWails()
    ? bridge().ApplySync(input)
    : http<SyncResult>('/api/opencode-sync/apply', { method: 'POST', body: JSON.stringify(input) })
}
