import type {
  AliasLockInput,
  AliasTargetInput,
  AliasTargetReorderInput,
  AliasUpsertInput,
  AutoAliasSettingsInput,
  AutoAliasSettingsResult,
  ConfigExportView,
  ConfigImportInput,
  ConfigImportResult,
  ConfigRevisionView,
  DesktopPrefsSaveResult,
  AliasView,
  DesktopPrefsView,
  DoctorRunResult,
  LifecycleExecuteInput,
  LifecycleExecuteResult,
  LifecyclePlanView,
  LifecyclePreviewInput,
  MetaView,
  Overview,
  ProviderImportInput,
  ProviderImportResult,
  ProviderHealthInput,
  ProviderHealthResult,
  ProviderGroupCreateInput,
  ProviderGroupDeleteInput,
  ProviderGroupPingInput,
  ProviderGroupRefreshModelsInput,
  ProviderGroupUpdateInput,
  ProviderGroupView,
  ProviderPingInput,
  ProviderPingResult,
  ProviderPriorityInput,
  ProviderPriorityResult,
  ProviderRefreshModelsInput,
  ProviderSaveResult,
  ProviderStateInput,
  ProviderUpsertInput,
  ProviderView,
  ProxySettingsSaveResult,
  ProxySettingsView,
  ProxyStatusView,
  RequestRewriteRuleInput,
  RequestRewriteRuleRemoveInput,
  RequestRewriteRuleRemoveResult,
  RequestRewriteRuleReorderInput,
  RequestRewriteRuleReorderResult,
  RequestRewriteRuleStateInput,
  RequestRewriteRuleView,
  RequestTrace,
  RequestTraceListInput,
  RequestTraceListResult,
  SyncInput,
  SyncPreview,
  SyncResult,
  ApiEnvelope,
  TransportOutcome,
} from './types'
import { TransportError } from './types'

const adminTokenKey = 'ocswitch.adminToken'

export function getAdminToken(): string {
  return window.sessionStorage.getItem(adminTokenKey) || ''
}

export function setAdminToken(token: string): void {
  const trimmed = token.trim()
  if (trimmed) {
    window.sessionStorage.setItem(adminTokenKey, trimmed)
    return
  }
  window.sessionStorage.removeItem(adminTokenKey)
}

function isWails(): boolean {
  return typeof window.go?.desktop?.App !== 'undefined'
}

function emptyOutcome(code = 'internal_error'): TransportOutcome {
  return { code, params: {}, retryable: false }
}

function normalizeEnvelope<T>(raw: unknown, fallbackCode = 'internal_error'): ApiEnvelope<T> {
  if (!raw || typeof raw !== 'object') {
    return { ok: false, error: fallbackCode, outcome: emptyOutcome(fallbackCode) }
  }
  const obj = raw as Record<string, unknown>
  const outcomeRaw = (obj.outcome && typeof obj.outcome === 'object' ? obj.outcome : {}) as Record<string, unknown>
  const code =
    (typeof outcomeRaw.code === 'string' && outcomeRaw.code) ||
    (typeof obj.error === 'string' && obj.error) ||
    (obj.ok === true ? 'ok' : fallbackCode)
  const params =
    outcomeRaw.params && typeof outcomeRaw.params === 'object' && !Array.isArray(outcomeRaw.params)
      ? (outcomeRaw.params as Record<string, unknown>)
      : {}
  const retryable = typeof outcomeRaw.retryable === 'boolean' ? outcomeRaw.retryable : false
  const ok = typeof obj.ok === 'boolean' ? obj.ok : code === 'ok' || code === 'restart_pending'
  return {
    ok,
    data: obj.data as T | undefined,
    error: typeof obj.error === 'string' ? obj.error : ok ? undefined : code,
    outcome: { code, params, retryable },
  }
}

/**
 * Unwrap a transport envelope. On classified failure throws TransportError
 * while preserving data (e.g. runtime_apply_failed execute result).
 */
export function unwrapEnvelope<T>(envelope: ApiEnvelope<T>, httpStatus?: number): T {
  if (envelope.ok) {
    return envelope.data as T
  }
  throw new TransportError(envelope, httpStatus)
}

async function httpEnvelope<T>(path: string, init?: RequestInit): Promise<ApiEnvelope<T>> {
  const headers = new Headers(init?.headers)
  headers.set('Content-Type', 'application/json')
  const token = getAdminToken()
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }
  const response = await fetch(path, {
    ...init,
    headers,
  })
  let raw: unknown
  try {
    raw = await response.json()
  } catch {
    throw new TransportError(
      {
        ok: false,
        error: 'internal_error',
        outcome: emptyOutcome('internal_error'),
      },
      response.status,
    )
  }
  const envelope = normalizeEnvelope<T>(raw)
  // Legacy responses may omit ok/outcome; treat HTTP ok + data as success.
  if (envelope.ok === false && response.ok && envelope.data !== undefined && !envelope.error) {
    return {
      ok: true,
      data: envelope.data,
      outcome: emptyOutcome('ok'),
    }
  }
  if (!envelope.ok) {
    throw new TransportError(envelope, response.status)
  }
  return envelope
}

async function http<T>(path: string, init?: RequestInit): Promise<T> {
  const envelope = await httpEnvelope<T>(path, init)
  return unwrapEnvelope(envelope)
}

function bridge() {
  const app = window.go?.desktop?.App
  if (!app) {
    throw new Error('Wails bridge unavailable')
  }
  return app
}

type ProviderGroupBridge = {
  ListProviderGroups: (providerID: string) => Promise<ProviderGroupView[]>
  CreateProviderGroup: (input: ProviderGroupCreateInput) => Promise<ProviderGroupView>
  UpdateProviderGroup: (input: ProviderGroupUpdateInput) => Promise<ProviderGroupView>
  DeleteProviderGroup: (input: ProviderGroupDeleteInput) => Promise<void>
  RefreshProviderGroupModels: (input: ProviderGroupRefreshModelsInput) => Promise<ProviderSaveResult>
  PingProviderGroupBaseURL: (input: ProviderGroupPingInput) => Promise<ProviderPingResult>
}

function providerGroupBridge(): ProviderGroupBridge {
  return bridge() as unknown as ProviderGroupBridge
}

/** Map Wails lifecycle envelope (resolved, never rejected for business codes). */
function fromWailsEnvelope<T>(raw: unknown): T {
  const envelope = normalizeEnvelope<T>(raw)
  return unwrapEnvelope(envelope)
}

export async function getMeta(): Promise<MetaView> {
  if (isWails()) {
    const data = await bridge().Meta()
    return {
      version: data.version || '',
      shell: data.shell || 'wails',
      capabilities: {
        desktopPrefs: true,
        openCodeDirectSync: true,
        proxyControl: true,
        transportEnvelopeVersion: 1,
        lifecycleContractVersion: 1,
      },
    }
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

export function refreshProviderModels(input: ProviderRefreshModelsInput): Promise<ProviderSaveResult> {
	return isWails()
		? bridge().RefreshProviderModels(input)
		: http<ProviderSaveResult>('/api/providers/refresh-models', { method: 'POST', body: JSON.stringify(input) })
}

export function pingProviderBaseUrl(input: ProviderPingInput): Promise<ProviderPingResult> {
	return isWails()
		? bridge().PingProviderBaseURL(input)
		: http<ProviderPingResult>('/api/providers/ping', { method: 'POST', body: JSON.stringify(input) })
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

export function getProviderPriority(): Promise<ProviderPriorityResult> {
  return isWails()
    ? bridge().GetProviderPriority()
    : http<ProviderPriorityResult>('/api/providers/priority')
}

export function setProviderPriority(input: ProviderPriorityInput): Promise<ProviderPriorityResult> {
  return isWails()
    ? bridge().SetProviderPriority(input)
    : http<ProviderPriorityResult>('/api/providers/priority', { method: 'POST', body: JSON.stringify(input) })
}

export function getAutoAliasSettings(): Promise<AutoAliasSettingsResult> {
  return isWails()
    ? bridge().GetAutoAliasSettings()
    : http<AutoAliasSettingsResult>('/api/auto-alias-settings')
}

export function setAutoAliasSettings(input: AutoAliasSettingsInput): Promise<AutoAliasSettingsResult> {
  return isWails()
    ? bridge().SetAutoAliasSettings(input)
    : http<AutoAliasSettingsResult>('/api/auto-alias-settings', { method: 'POST', body: JSON.stringify(input) })
}

export function upgradeAutoAlias(input: AliasLockInput): Promise<AliasView> {
  return isWails()
    ? bridge().UpgradeAutoAlias(input)
    : http<AliasView>('/api/aliases/upgrade-manual', { method: 'POST', body: JSON.stringify(input) })
}

export function resetAliasTargetOrder(input: AliasLockInput): Promise<AliasView> {
  return isWails()
    ? bridge().ResetTargetOrder(input)
    : http<AliasView>('/api/aliases/reset-target-order', { method: 'POST', body: JSON.stringify(input) })
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

export function reorderAliasTargets(input: AliasTargetReorderInput): Promise<AliasView> {
  return isWails()
    ? bridge().ReorderTargets(input)
    : http<AliasView>('/api/aliases/reorder-targets', { method: 'POST', body: JSON.stringify(input) })
}

export function listRewriteRules(): Promise<RequestRewriteRuleView[]> {
  return isWails() ? bridge().RewriteRules() : http<RequestRewriteRuleView[]>('/api/rewrite-rules')
}

export function saveRewriteRule(input: RequestRewriteRuleInput): Promise<RequestRewriteRuleView> {
  return isWails()
    ? bridge().SaveRewriteRule(input)
    : http<RequestRewriteRuleView>('/api/rewrite-rules', { method: 'POST', body: JSON.stringify(input) })
}

export function setRewriteRuleState(input: RequestRewriteRuleStateInput): Promise<RequestRewriteRuleView> {
  return isWails()
    ? bridge().SetRewriteRuleState(input)
    : http<RequestRewriteRuleView>('/api/rewrite-rules/state', { method: 'POST', body: JSON.stringify(input) })
}

export function deleteRewriteRule(input: RequestRewriteRuleRemoveInput): Promise<RequestRewriteRuleRemoveResult> {
  return isWails()
    ? bridge().DeleteRewriteRule(input)
    : http<RequestRewriteRuleRemoveResult>('/api/rewrite-rules/delete', { method: 'POST', body: JSON.stringify(input) })
}

export function reorderRewriteRules(input: RequestRewriteRuleReorderInput): Promise<RequestRewriteRuleReorderResult> {
  return isWails()
    ? bridge().ReorderRewriteRules(input)
    : http<RequestRewriteRuleReorderResult>('/api/rewrite-rules/reorder', { method: 'POST', body: JSON.stringify(input) })
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

export function queryRequestTraces(input: RequestTraceListInput): Promise<RequestTraceListResult> {
	if (isWails()) {
		return bridge().TraceList(input)
	}
	return http<RequestTraceListResult>('/api/proxy/traces/query', { method: 'POST', body: JSON.stringify(input) })
}

export function getRequestTrace(id: number): Promise<RequestTrace> {
	if (isWails()) {
		return bridge().TraceDetail(id)
	}
	return http<RequestTrace>('/api/proxy/traces/detail', { method: 'POST', body: JSON.stringify({ id }) })
}

export function queryProviderHealth(input: ProviderHealthInput): Promise<ProviderHealthResult> {
	if (isWails()) {
		return bridge().ProviderHealth(input)
	}
	return http<ProviderHealthResult>('/api/proxy/health', { method: 'POST', body: JSON.stringify(input) })
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

/** Current config revision (HTTP GET /api/config/revision or Wails envelope). */
export async function getConfigRevision(): Promise<ConfigRevisionView> {
  if (isWails()) {
    return fromWailsEnvelope<ConfigRevisionView>(await bridge().GetConfigRevision())
  }
  return http<ConfigRevisionView>('/api/config/revision')
}

/** Preview a lifecycle mutation without side effects. */
export async function previewLifecycle(input: LifecyclePreviewInput): Promise<LifecyclePlanView> {
  if (isWails()) {
    return fromWailsEnvelope<LifecyclePlanView>(await bridge().PreviewLifecycle(input))
  }
  return http<LifecyclePlanView>('/api/lifecycle/preview', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/**
 * Execute a previously previewed lifecycle plan.
 * On runtime_apply_failed, throws TransportError with execute result in data.
 */
export async function executeLifecycle(input: LifecycleExecuteInput): Promise<LifecycleExecuteResult> {
  if (isWails()) {
    return fromWailsEnvelope<LifecycleExecuteResult>(await bridge().ExecuteLifecycle(input))
  }
  return http<LifecycleExecuteResult>('/api/lifecycle/execute', {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

/** Envelope-preserving variants for callers that need full outcome (e.g. conflict UI). */
export async function getConfigRevisionEnvelope(): Promise<ApiEnvelope<ConfigRevisionView>> {
  if (isWails()) {
    return normalizeEnvelope<ConfigRevisionView>(await bridge().GetConfigRevision())
  }
  try {
    const data = await http<ConfigRevisionView>('/api/config/revision')
    return { ok: true, data, outcome: emptyOutcome('ok') }
  } catch (err) {
    if (err instanceof TransportError) {
      return { ok: false, error: err.code, data: err.data as ConfigRevisionView | undefined, outcome: err.outcome }
    }
    throw err
  }
}

export async function previewLifecycleEnvelope(
  input: LifecyclePreviewInput,
): Promise<ApiEnvelope<LifecyclePlanView>> {
  if (isWails()) {
    return normalizeEnvelope<LifecyclePlanView>(await bridge().PreviewLifecycle(input))
  }
  try {
    const data = await http<LifecyclePlanView>('/api/lifecycle/preview', {
      method: 'POST',
      body: JSON.stringify(input),
    })
    return { ok: true, data, outcome: emptyOutcome('ok') }
  } catch (err) {
    if (err instanceof TransportError) {
      return { ok: false, error: err.code, data: err.data as LifecyclePlanView | undefined, outcome: err.outcome }
    }
    throw err
  }
}

export async function executeLifecycleEnvelope(
  input: LifecycleExecuteInput,
): Promise<ApiEnvelope<LifecycleExecuteResult>> {
  if (isWails()) {
    return normalizeEnvelope<LifecycleExecuteResult>(await bridge().ExecuteLifecycle(input))
  }
  try {
    const data = await http<LifecycleExecuteResult>('/api/lifecycle/execute', {
      method: 'POST',
      body: JSON.stringify(input),
    })
    return { ok: true, data, outcome: emptyOutcome(data.pendingRestart ? 'restart_pending' : 'ok') }
  } catch (err) {
    if (err instanceof TransportError) {
      return {
        ok: false,
        error: err.code,
        data: err.data as LifecycleExecuteResult | undefined,
        outcome: err.outcome,
      }
    }
    throw err
  }
}

export function listProviderGroups(providerID: string): Promise<ProviderGroupView[]> {
  return isWails()
    ? providerGroupBridge().ListProviderGroups(providerID)
    : http<ProviderGroupView[]>('/api/admin/providers/' + encodeURIComponent(providerID) + '/groups')
}

export function createProviderGroup(input: ProviderGroupCreateInput): Promise<ProviderGroupView> {
  return isWails()
    ? providerGroupBridge().CreateProviderGroup(input)
    : http<ProviderGroupView>('/api/admin/providers/' + encodeURIComponent(input.providerId) + '/groups', {
        method: 'POST',
        body: JSON.stringify(input),
      })
}

export function updateProviderGroup(input: ProviderGroupUpdateInput): Promise<ProviderGroupView> {
  const path = '/api/admin/providers/' + encodeURIComponent(input.providerId) + '/groups/' + encodeURIComponent(input.groupId)
  return isWails()
    ? providerGroupBridge().UpdateProviderGroup(input)
    : http<ProviderGroupView>(path, { method: 'PUT', body: JSON.stringify(input) })
}

export async function deleteProviderGroup(input: ProviderGroupDeleteInput): Promise<void> {
  const path = '/api/admin/providers/' + encodeURIComponent(input.providerId) + '/groups/' + encodeURIComponent(input.groupId)
  if (isWails()) {
    await providerGroupBridge().DeleteProviderGroup(input)
    return
  }
  await http<{ ok: boolean }>(path, { method: 'DELETE', body: JSON.stringify(input) })
}

export function refreshProviderGroupModels(input: ProviderGroupRefreshModelsInput): Promise<ProviderSaveResult> {
  const path = '/api/admin/providers/' + encodeURIComponent(input.providerId) + '/groups/' + encodeURIComponent(input.groupId) + '/refresh-models'
  return isWails()
    ? providerGroupBridge().RefreshProviderGroupModels(input)
    : http<ProviderSaveResult>(path, { method: 'POST', body: JSON.stringify(input) })
}

export function pingProviderGroupBaseURL(input: ProviderGroupPingInput): Promise<ProviderPingResult> {
  const path = '/api/admin/providers/' + encodeURIComponent(input.providerId) + '/groups/' + encodeURIComponent(input.groupId) + '/ping'
  return isWails()
    ? providerGroupBridge().PingProviderGroupBaseURL(input)
    : http<ProviderPingResult>(path, { method: 'POST', body: JSON.stringify(input) })
}
