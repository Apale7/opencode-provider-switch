import { ChangeEvent, DragEvent, FormEvent, KeyboardEvent, useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import {
  applySync,
  bindAliasTarget,
  deleteAlias,
  deleteProvider,
  exportConfig,
  getRequestTrace,
  getMeta,
  getOverview,
  getProxySettings,
  importConfig,
  importProviders,
  listAliases,
  listProviders,
  queryRequestTraces,
  refreshProviderModels,
  previewSync,
  pingProviderBaseUrl,
  reorderAliasTargets,
  runDoctor,
  saveAlias,
  saveDesktopPrefs,
  saveProxySettings,
  saveProvider,
  openExternalURL,
  setAliasTargetState,
  setProviderState,
  startProxy,
  stopProxy,
  unbindAliasTarget,
} from './api'
import i18n, { resolveLanguagePreference } from './i18n'
import githubMark from './assets/GitHub_Invertocat_Black_Clearspace.png'
import type {
  AliasTargetInput,
  AliasTargetView,
  AliasView,
  AliasUpsertInput,
  DesktopPrefsSaveResult,
  DesktopPrefsView,
  DoctorIssue,
  DoctorRunResult,
  LanguagePreference,
  OpenCodeReconciliationSummary,
  Overview,
  ProviderImportInput,
  ProviderImportResult,
  ProviderBaseURLStrategy,
  ProviderPingInput,
  ProviderPingResult,
  ProviderRefreshModelsInput,
  ProviderSaveResult,
  ProviderProtocol,
  ProviderUpsertInput,
  ProviderView,
  RoutingStrategyDescriptor,
  RoutingStrategyParamSpec,
  ProxyStatusView,
  ProxySettingsSaveResult,
  ProxySettingsView,
  RequestTrace,
  RequestTraceListResult,
  SyncInput,
  SyncPreview,
  SyncResult,
  ThemePreference,
} from './types'

type MetaState = {
  version: string
  shell: string
  url?: string
}

type ProviderFormState = {
  id: string
  name: string
  protocol: ProviderProtocol
  baseUrls: string[]
  baseUrlStrategy: ProviderBaseURLStrategy
  apiKey: string
  headersText: string
  disabled: boolean
  skipModels: boolean
  clearHeaders: boolean
}

type ProviderBaseUrlPingState = Record<string, ProviderPingResult | undefined>

type AliasFormState = {
  alias: string
  displayName: string
  protocol: ProviderProtocol
  disabled: boolean
}

type TabKey = 'overview' | 'providers' | 'aliases' | 'log' | 'network' | 'sync' | 'settings'
type FilterState = 'all' | 'enabled' | 'disabled'
type ResolvedTheme = 'light' | 'dark'
type ModalKey = 'provider-import' | 'alias-target' | null
type DetailMode = 'empty' | 'create' | 'edit'
type ConfigImportMode = 'text' | 'file'
type TraceFilterKey = 'all' | 'none'
type ConfirmIntent =
  | { kind: 'delete-provider'; id: string }
  | { kind: 'delete-alias'; alias: string }
  | { kind: 'unbind-target'; alias: string; provider: string; model: string }

const tabs: TabKey[] = ['overview', 'providers', 'aliases', 'log', 'network', 'sync', 'settings']
const GITHUB_REPOSITORY_URL = 'https://github.com/Apale7/opencode-provider-switch'
const protocolOptions: ProviderProtocol[] = ['openai-responses', 'anthropic-messages', 'openai-compatible']

const emptyPrefs: DesktopPrefsView = {
  launchAtLogin: false,
  autoStartProxy: false,
  minimizeToTray: false,
  notifications: false,
  theme: 'system',
  language: 'system',
}

const emptyProxySettings: ProxySettingsView = {
  connectTimeoutMs: 10000,
  responseHeaderTimeoutMs: 15000,
  firstByteTimeoutMs: 15000,
  requestReadTimeoutMs: 30000,
  streamIdleTimeoutMs: 60000,
  routing: {
    strategy: 'circuit-breaker',
    params: {
      failureThreshold: 2,
      baseCooldownMs: 30000,
      maxCooldownMs: 300000,
      backoffMultiplier: 2,
      halfOpenMaxRequests: 1,
      closeAfterSuccesses: 1,
      countPostCommitErrors: true,
      rateLimitCooldownMs: 15000,
    },
    descriptors: [],
  },
}

const emptySync: SyncInput = {
  target: '',
  setModel: '',
  setSmallModel: '',
}

const emptyProviderForm: ProviderFormState = {
  id: '',
  name: '',
  protocol: 'openai-responses',
  baseUrls: [''],
  baseUrlStrategy: 'ordered',
  apiKey: '',
  headersText: '',
  disabled: false,
  skipModels: false,
  clearHeaders: false,
}

const emptyProviderImport: ProviderImportInput = {
  sourcePath: '',
  overwrite: false,
}

const emptyAliasForm: AliasFormState = {
  alias: '',
  displayName: '',
  protocol: 'openai-responses',
  disabled: false,
}

function protocolLabel(protocol: ProviderProtocol): string {
	return protocol === 'openai-responses'
		? 'OpenAI Responses'
		: protocol === 'anthropic-messages'
			? 'Anthropic Messages'
			: protocol === 'openai-compatible'
				? 'OpenAI Compatible'
			: protocol
}

function resolveAliasProtocol(alias: AliasView | null): ProviderProtocol {
	return alias?.protocol || 'openai-responses'
}

function protocolBadgeClass(protocol: ProviderProtocol): string {
	return `badge protocol-badge protocol-${protocol}`
}

const emptyTargetForm: AliasTargetInput = {
  alias: '',
  provider: '',
  model: '',
  disabled: false,
}

const tracePageSize = 25

type TraceQueryState = {
	page: number
	aliases: string[]
	failoverCounts: number[]
	statusCodes: number[]
}

type TraceCatalogState = {
	aliases: string[]
	failoverCounts: number[]
	statusCodes: number[]
}

type TracePageKind = 'log' | 'network'

const emptyTraceQuery: TraceQueryState = {
	page: 1,
	aliases: [],
	failoverCounts: [],
	statusCodes: [],
}

const emptyTraceCatalog: TraceCatalogState = {
	aliases: [],
	failoverCounts: [],
	statusCodes: [],
}

function traceQueryKey(query: TraceQueryState): string {
	return [query.page, query.aliases.join('\u0000'), query.failoverCounts.join(','), query.statusCodes.join(',')].join('|')
}

function resolveDraftAliasProtocol(
	aliasName: string,
	aliasForm: AliasFormState,
	selectedAlias: AliasView | null,
	aliases: AliasView[],
	preferDraftProtocol: boolean,
): ProviderProtocol {
	const trimmedAlias = aliasName.trim()
	const draftAlias = aliasForm.alias.trim()
	const storedAlias = aliases.find((alias) => alias.alias === trimmedAlias) || null
	if (preferDraftProtocol && (!trimmedAlias || trimmedAlias === draftAlias || !storedAlias)) {
		return aliasForm.protocol
	}
	return resolveAliasProtocol(storedAlias || selectedAlias)
}

function selectedTraceModel(providerId: string, providers: ProviderView[]): string[] {
	return providers.find((provider) => provider.id === providerId)?.models || []
}

function tracePageCount(total: number, pageSize: number): number {
	return Math.max(1, Math.ceil(total / pageSize))
}

function toggleNumberFilter(values: number[], value: number): number[] {
	return values.includes(value) ? values.filter((item) => item !== value) : [...values, value].sort((a, b) => a - b)
}

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

function issueToneClass(severity?: string): string {
  if (severity === 'error') {
    return 'tone-error'
  }
  if (severity === 'warning') {
    return 'tone-warning'
  }
  return 'tone-ok'
}

function issueBadgeClass(severity?: string): string {
  return `badge outline ${severity === 'error' ? 'idle' : severity === 'warning' ? 'warn' : 'live'}`
}

function issueLocation(issue: DoctorIssue): string {
  return issue.path || issue.directory || issue.providerKey || issue.protocol || ''
}

function renderIssue(issue: DoctorIssue, key: string) {
  const location = issueLocation(issue)
  return (
    <div className="issue-card" key={key}>
      <div className="issue-card-head">
        <span className={issueBadgeClass(issue.severity)}>{issue.severity || 'info'}</span>
        {issue.code ? <code>{issue.code}</code> : null}
        {issue.protocol ? <span className={protocolBadgeClass(issue.protocol as ProviderProtocol)}>{protocolLabel(issue.protocol as ProviderProtocol)}</span> : null}
      </div>
      <div className={issueToneClass(issue.severity)}>{issue.message}</div>
      {location ? <div className="subtle">{location}</div> : null}
      {issue.expected || issue.actual ? <div className="subtle">{issue.expected ? `expected: ${issue.expected}` : ''}{issue.expected && issue.actual ? ' | ' : ''}{issue.actual ? `actual: ${issue.actual}` : ''}</div> : null}
      {issue.actionHint ? <div className="subtle">{issue.actionHint}</div> : null}
      {issue.details && issue.details.length > 0 ? <div className="subtle">{issue.details.join(' | ')}</div> : null}
    </div>
  )
}

function renderReconciliationSummary(summary?: OpenCodeReconciliationSummary) {
  if (!summary) {
    return null
  }
  const facts = [
    `runtime ${summary.runtimeReachable ? 'reachable' : 'unreachable'}`,
    `file ${summary.fileSnapshotAvailable ? 'loaded' : 'unavailable'}`,
    `aliases ${(summary.availableAliases || []).length}`,
    `missing ${(summary.missingProviders || []).length}`,
    `catalog drift ${(summary.catalogMismatches || []).length}`,
  ]
  return <div className="issue-summary subtle">{facts.join(' · ')}</div>
}

function syncOutputChanged(value: SyncPreview | SyncResult): boolean {
  return 'changed' in value ? value.changed : value.wouldChange
}

function overviewDebugSnapshot(overview: Overview | null) {
  if (!overview) {
    return null
  }
  return {
    ...overview,
    configPath: undefined,
  }
}

function formatError(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function headersTextFromMap(headers?: Record<string, string>): string {
  if (!headers || Object.keys(headers).length === 0) {
    return ''
  }
  return Object.entries(headers)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => `${key}=${value}`)
    .join('\n')
}

function parseHeadersText(input: string): Record<string, string> | undefined {
  const lines = input
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
  if (lines.length === 0) {
    return undefined
  }
  const headers: Record<string, string> = {}
  for (const line of lines) {
    const index = line.indexOf('=')
    if (index <= 0) {
      throw new Error(i18n.t('errors.invalidHeaderFormat', { line }))
    }
    const key = line.slice(0, index).trim().toLowerCase()
    const value = line.slice(index + 1).trim()
    if (!key) {
      throw new Error(i18n.t('errors.invalidHeaderName', { line }))
    }
    headers[key] = value
  }
  return headers
}

function providerFormFromView(provider: ProviderView): ProviderFormState {
  return {
    id: provider.id,
    name: provider.name || '',
    protocol: provider.protocol,
    baseUrls: provider.baseUrls && provider.baseUrls.length > 0 ? [...provider.baseUrls] : [provider.baseUrl],
    baseUrlStrategy: provider.baseUrlStrategy || 'ordered',
    apiKey: '',
    headersText: headersTextFromMap(provider.headers),
    disabled: provider.disabled,
    skipModels: false,
    clearHeaders: false,
  }
}

function providerEffectiveBaseUrls(provider: ProviderView | null): string[] {
	if (!provider) {
		return []
	}
	return provider.baseUrls && provider.baseUrls.length > 0 ? provider.baseUrls : [provider.baseUrl]
}

function providerBaseUrlSummary(provider: ProviderView | null, form: ProviderFormState, t: (key: string, options?: Record<string, unknown>) => string): string {
	const baseUrls = providerEffectiveBaseUrls(provider)
	if (baseUrls.length > 0) {
		return baseUrls.join('\n')
	}
	const draft = form.baseUrls.map((item) => item.trim()).filter(Boolean)
	if (draft.length > 0) {
		return draft.join('\n')
	}
	return t('providers.baseUrlsEmpty')
}

function aliasFormFromView(alias: AliasView): AliasFormState {
  return {
    alias: alias.alias,
    displayName: alias.displayName || '',
    protocol: alias.protocol,
    disabled: !alias.enabled,
  }
}

function joinWarnings(warnings?: string[]): string {
  if (!warnings || warnings.length === 0) {
    return ''
  }
  return warnings.join(' | ')
}

function withWarnings(base: string, warnings?: string[]): string {
  const merged = joinWarnings(warnings)
  if (!merged) {
    return base
  }
  return `${base}. ${i18n.t('messages.warningsSuffix', { warnings: merged })}`
}

function providerSaveStatus(result: ProviderSaveResult): string {
  return withWarnings(i18n.t('providers.statusSaved', { id: result.provider.id }), result.warnings)
}

function providerImportStatus(result: ProviderImportResult): string {
  return withWarnings(
    i18n.t('providers.statusImportDone', { imported: result.imported, skipped: result.skipped }),
    result.warnings,
  )
}

function desktopPrefsSaveStatus(result: DesktopPrefsSaveResult): string {
  return withWarnings(i18n.t('messages.saved'), result.warnings)
}

function proxySettingsSaveStatus(result: ProxySettingsSaveResult): string {
  return withWarnings(i18n.t('messages.saved'), result.warnings)
}

function activeRoutingDescriptor(proxySettings: ProxySettingsView): RoutingStrategyDescriptor | null {
	return proxySettings.routing.descriptors?.find((item) => item.name === proxySettings.routing.strategy) || null
}

function routingI18nBase(strategyName: string): string | null {
	if (strategyName === 'circuit-breaker') {
		return 'settings.routing.circuitBreaker'
	}
	return null
}

function routingStrategyLabel(descriptor: RoutingStrategyDescriptor): string {
	const key = routingI18nBase(descriptor.name)
	return key ? i18n.t(`${key}.displayName`, { defaultValue: descriptor.displayName }) : descriptor.displayName
}

function routingStrategyDescription(descriptor: RoutingStrategyDescriptor): string {
	const key = routingI18nBase(descriptor.name)
	return key ? i18n.t(`${key}.description`, { defaultValue: descriptor.description || '' }) : (descriptor.description || '')
}

function routingParameterLabel(strategyName: string, parameter: RoutingStrategyParamSpec): string {
	const key = routingI18nBase(strategyName)
	return key ? i18n.t(`${key}.params.${parameter.key}.label`, { defaultValue: parameter.description || parameter.key }) : (parameter.description || parameter.key)
}

function routingParameterDescription(strategyName: string, parameter: RoutingStrategyParamSpec): string {
	const key = routingI18nBase(strategyName)
	return key ? i18n.t(`${key}.params.${parameter.key}.description`, { defaultValue: parameter.description || '' }) : (parameter.description || '')
}

function updateRoutingParam(current: ProxySettingsView, key: string, value: unknown): ProxySettingsView {
	return {
		...current,
		routing: {
			...current.routing,
			params: {
				...(current.routing.params || {}),
				[key]: value,
			},
		},
	}
}

function routingParamInput(
	proxySettings: ProxySettingsView,
	strategyName: string,
	parameter: RoutingStrategyParamSpec,
	onChange: (next: ProxySettingsView) => void,
) {
	const value = proxySettings.routing.params?.[parameter.key] ?? parameter.defaultValue
	if (parameter.type === 'bool') {
		return (
			<label className="checkbox-row" key={parameter.key}>
				<input
					type="checkbox"
					checked={Boolean(value)}
					onChange={(event) => onChange(updateRoutingParam(proxySettings, parameter.key, event.target.checked))}
				/>
				<span>{routingParameterLabel(strategyName, parameter)}</span>
			</label>
		)
	}
	if (parameter.enum && parameter.enum.length > 0) {
		return (
			<label key={parameter.key}>
				<span>{routingParameterLabel(strategyName, parameter)}</span>
				<select value={String(value ?? '')} onChange={(event) => onChange(updateRoutingParam(proxySettings, parameter.key, event.target.value))}>
					{parameter.enum.map((option) => (
						<option key={option} value={option}>{option}</option>
					))}
				</select>
				{routingParameterDescription(strategyName, parameter) ? <p className="subtle">{routingParameterDescription(strategyName, parameter)}</p> : null}
			</label>
		)
	}
	return (
		<label key={parameter.key}>
			<span>{routingParameterLabel(strategyName, parameter)}</span>
			<input
				type="number"
				min={parameter.min}
				max={parameter.max}
				step={parameter.type === 'float' ? '0.1' : 1}
				value={typeof value === 'number' ? value : Number(value) || 0}
				onChange={(event) => onChange(updateRoutingParam(proxySettings, parameter.key, parameter.type === 'float' ? Number(event.target.value) || 0 : Math.trunc(Number(event.target.value) || 0)))}
			/>
			{routingParameterDescription(strategyName, parameter) ? <p className="subtle">{routingParameterDescription(strategyName, parameter)}</p> : null}
		</label>
	)
}

function normalizeTab(hash: string): TabKey {
  const value = hash.replace(/^#/, '')
  return tabs.includes(value as TabKey) ? (value as TabKey) : 'overview'
}

function resolveThemePreference(theme: ThemePreference, systemTheme: ResolvedTheme): ResolvedTheme {
  if (theme === 'light' || theme === 'dark') {
    return theme
  }
  return systemTheme
}

function configFileName(configPath: string): string {
  const parts = configPath.split(/[/\\]/).filter(Boolean)
  return parts[parts.length - 1] || 'ocswitch-config.json'
}

function formatDateTime(value?: string): string {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function formatCompactDateTime(value?: string): string {
	if (!value) {
		return '-'
	}
	const date = new Date(value)
	if (Number.isNaN(date.getTime())) {
		return value
	}
	const month = String(date.getMonth() + 1).padStart(2, '0')
	const day = String(date.getDate()).padStart(2, '0')
	const hours = String(date.getHours()).padStart(2, '0')
	const minutes = String(date.getMinutes()).padStart(2, '0')
	const seconds = String(date.getSeconds()).padStart(2, '0')
	return `${month}-${day} ${hours}:${minutes}:${seconds}`
}

function formatDuration(value?: number): string {
  if (value == null) {
    return '-'
  }
  return `${value} ms`
}

function formatCompactDuration(value?: number): string {
	if (value == null) {
		return '-'
	}
	if (value >= 1000) {
		return `${(value / 1000).toFixed(2)} s`
	}
	return `${Math.round(value)} ms`
}

function formatTokenCount(value?: number): string {
  if (value == null) {
    return '-'
  }
  return value.toLocaleString()
}

function formatUsageText(value?: number | string): string {
  if (value == null || value === '') {
    return '-'
  }
  if (typeof value === 'number') {
    return value.toLocaleString()
  }
  return value
}

function isProviderProtocol(value?: string): value is ProviderProtocol {
  return value === 'openai-responses' || value === 'anthropic-messages' || value === 'openai-compatible'
}

function usageSourceLabel(source?: string): string {
  if (!source) {
    return '-'
  }
  if (isProviderProtocol(source)) {
    return protocolLabel(source)
  }
  return source
}

function usageSourceBadgeClass(source?: string): string {
  if (isProviderProtocol(source)) {
    return protocolBadgeClass(source)
  }
  return 'badge outline'
}

function usagePrecisionLabel(precision?: string): string {
  switch (precision) {
    case 'exact':
      return i18n.t('log.usagePrecisionValues.exact')
    case 'partial':
      return i18n.t('log.usagePrecisionValues.partial')
    case 'unavailable':
      return i18n.t('log.usagePrecisionValues.unavailable')
    default:
      return formatUsageText(precision)
  }
}

function usagePrecisionBadgeClass(precision?: string): string {
  switch (precision) {
    case 'exact':
      return 'badge live'
    case 'partial':
      return 'badge warn'
    case 'unavailable':
      return 'badge outline idle'
    default:
      return 'badge outline'
  }
}

function formatTokenRate(trace: RequestTrace): string {
  if (!trace.outputTokens || trace.outputTokens <= 0 || !trace.durationMs || trace.durationMs <= 0) {
    return '-'
  }
  return `${((trace.outputTokens * 1000) / trace.durationMs).toFixed(1)} token/s`
}

function formatCompactTokenRate(trace: RequestTrace): string {
	if (!trace.outputTokens || trace.outputTokens <= 0 || !trace.durationMs || trace.durationMs <= 0) {
		return '-'
	}
	return `${((trace.outputTokens * 1000) / trace.durationMs).toFixed(2)} tok/s`
}

function traceTotalTokens(trace: RequestTrace): number | null {
	if (typeof trace.usage?.rawTotalTokens === 'number') {
		return trace.usage.rawTotalTokens
	}
	const inputTokens = trace.inputTokens ?? trace.usage?.inputTokens ?? trace.usage?.rawInputTokens
	const outputTokens = trace.outputTokens ?? trace.usage?.outputTokens ?? trace.usage?.rawOutputTokens
	if (typeof inputTokens === 'number' || typeof outputTokens === 'number') {
		return (inputTokens || 0) + (outputTokens || 0)
	}
	return null
}

function traceDisplayModel(trace: RequestTrace): string {
	return trace.finalModel || trace.alias || trace.rawModel || `#${trace.id}`
}

function InfoIcon() {
	return (
		<svg className="trace-info-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
			<circle cx="12" cy="12" r="10" />
			<path d="M12 16v-4" />
			<path d="M12 8h.01" />
		</svg>
	)
}

function TraceInfoPopover({ label, children }: { label: string; children: ReactNode }) {
	return (
		<span
			className="trace-info-popover"
			onClick={(event) => event.stopPropagation()}
			onMouseDown={(event) => event.stopPropagation()}
			onKeyDown={(event) => event.stopPropagation()}
		>
			<button type="button" className="trace-info-trigger" aria-label={label}>
				<InfoIcon />
			</button>
			<span className="trace-info-card" role="tooltip">
				{children}
			</span>
		</span>
	)
}

function tracePrimaryText(trace: RequestTrace): string {
  if (trace.finalProvider && trace.finalModel) {
    return `${trace.finalProvider}/${trace.finalModel}`
  }
  return trace.error || i18n.t('messages.noData')
}

function downloadTextFile(filename: string, content: string) {
  const blob = new Blob([content], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.append(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function focusableElements(root: HTMLElement): HTMLElement[] {
  return Array.from(
    root.querySelectorAll<HTMLElement>(
      'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    ),
  )
}

function focusFirstInput(root: HTMLElement | null) {
  if (!root) {
    return
  }
  const target = root.querySelector<HTMLElement>(
    'input:not([disabled]):not([type="checkbox"]), select:not([disabled]), textarea:not([disabled])',
  )
  ;(target || root).focus()
}

function focusFirstFocusable(root: HTMLElement | null) {
  if (!root) {
    return
  }
  const [firstFocusable] = focusableElements(root)
  ;(firstFocusable || root).focus()
}

function resolveSelectedTraceId(current: number | null, traces: RequestTrace[]): number | null {
  if (!current) {
    return null
  }
  return traces.some((trace) => trace.id === current) ? current : null
}

function providerMatches(provider: ProviderView, query: string, filter: FilterState): boolean {
  if (filter === 'enabled' && provider.disabled) {
    return false
  }
  if (filter === 'disabled' && !provider.disabled) {
    return false
  }
  if (!query) {
    return true
  }
  const haystack = [provider.id, provider.name || '', ...(provider.baseUrls || [provider.baseUrl]), provider.models?.join(' ') || '']
    .join(' ')
    .toLowerCase()
  return haystack.includes(query)
}

function aliasMatches(alias: AliasView, query: string): boolean {
  if (!query) {
    return true
  }
  const targets = alias.targets.map((target) => `${target.provider} ${target.model}`).join(' ')
  const haystack = [alias.alias, alias.displayName || '', targets].join(' ').toLowerCase()
  return haystack.includes(query)
}

export default function App() {
  const { t } = useTranslation()
  const [meta, setMeta] = useState<MetaState>({ version: 'dev', shell: 'loading' })
  const [overview, setOverview] = useState<Overview | null>(null)
  const [providers, setProviders] = useState<ProviderView[]>([])
  const [aliases, setAliases] = useState<AliasView[]>([])
  const [prefs, setPrefs] = useState<DesktopPrefsView>(emptyPrefs)
  const [proxySettings, setProxySettings] = useState<ProxySettingsView>(emptyProxySettings)
  const [prefsStatus, setPrefsStatus] = useState('')
  const [proxySettingsStatus, setProxySettingsStatus] = useState('')
  const [doctorStatus, setDoctorStatus] = useState('')
  const [doctorResult, setDoctorResult] = useState<DoctorRunResult | null>(null)
  const [syncStatus, setSyncStatus] = useState('')
  const [syncInput, setSyncInput] = useState<SyncInput>(emptySync)
  const [syncOutput, setSyncOutput] = useState<SyncPreview | SyncResult | string>('')
  const [providerStatus, setProviderStatus] = useState('')
  const [providerForm, setProviderForm] = useState<ProviderFormState>(emptyProviderForm)
  const [providerBaseUrlPings, setProviderBaseUrlPings] = useState<ProviderBaseUrlPingState>({})
  const [draggingProviderBaseUrlIndex, setDraggingProviderBaseUrlIndex] = useState<number | null>(null)
  const [providerImportForm, setProviderImportForm] = useState<ProviderImportInput>(emptyProviderImport)
  const [aliasStatus, setAliasStatus] = useState('')
  const [aliasForm, setAliasForm] = useState<AliasFormState>(emptyAliasForm)
  const [targetForm, setTargetForm] = useState<AliasTargetInput>(emptyTargetForm)
  const [draggingAliasTargetIndex, setDraggingAliasTargetIndex] = useState<number | null>(null)
  const [logTraces, setLogTraces] = useState<RequestTrace[]>([])
  const [networkTraces, setNetworkTraces] = useState<RequestTrace[]>([])
  const [logTraceQuery, setLogTraceQuery] = useState<TraceQueryState>(emptyTraceQuery)
  const [networkTraceQuery, setNetworkTraceQuery] = useState<TraceQueryState>(emptyTraceQuery)
  const [logTraceTotal, setLogTraceTotal] = useState(0)
  const [networkTraceTotal, setNetworkTraceTotal] = useState(0)
  const [logTraceLoaded, setLogTraceLoaded] = useState(false)
  const [networkTraceLoaded, setNetworkTraceLoaded] = useState(false)
  const [logTraceCatalog, setLogTraceCatalog] = useState<TraceCatalogState>(emptyTraceCatalog)
  const [networkTraceCatalog, setNetworkTraceCatalog] = useState<TraceCatalogState>(emptyTraceCatalog)
  const [logTraceStatus, setLogTraceStatus] = useState('')
  const [networkTraceStatus, setNetworkTraceStatus] = useState('')
  const [selectedLogTraceId, setSelectedLogTraceId] = useState<number | null>(null)
  const [selectedNetworkTraceId, setSelectedNetworkTraceId] = useState<number | null>(null)
  const [logTraceDetail, setLogTraceDetail] = useState<RequestTrace | null>(null)
  const [networkTraceDetail, setNetworkTraceDetail] = useState<RequestTrace | null>(null)
  const [loading, setLoading] = useState(false)
  const [proxyActionLoading, setProxyActionLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('overview')
  const [providerQuery, setProviderQuery] = useState('')
  const [providerFilter, setProviderFilter] = useState<FilterState>('all')
  const [aliasQuery, setAliasQuery] = useState('')
  const [selectedProviderId, setSelectedProviderId] = useState<string | null>(null)
  const [providerDetailMode, setProviderDetailMode] = useState<DetailMode>('empty')
  const [editingProviderId, setEditingProviderId] = useState('')
  const [selectedAliasId, setSelectedAliasId] = useState<string | null>(null)
  const [aliasDetailMode, setAliasDetailMode] = useState<DetailMode>('empty')
  const [editingAliasId, setEditingAliasId] = useState('')
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>('dark')
  const [systemLanguage, setSystemLanguage] = useState('en-US')
  const [activeModal, setActiveModal] = useState<ModalKey>(null)
  const [configTransferStatus, setConfigTransferStatus] = useState('')
  const [configImportMode, setConfigImportMode] = useState<ConfigImportMode>('text')
  const [configImportText, setConfigImportText] = useState('')
  const [configImportFileName, setConfigImportFileName] = useState('')
  const [confirmIntent, setConfirmIntent] = useState<ConfirmIntent | null>(null)
  const activeRouting = activeRoutingDescriptor(proxySettings)
  const providerDetailRef = useRef<HTMLDivElement | null>(null)
  const aliasDetailRef = useRef<HTMLDivElement | null>(null)
  const logDetailRef = useRef<HTMLDivElement | null>(null)
  const networkDetailRef = useRef<HTMLDivElement | null>(null)
  const logTraceRequestRef = useRef(0)
  const networkTraceRequestRef = useRef(0)
  const logTraceLoadingKeyRef = useRef<string | null>(null)
  const networkTraceLoadingKeyRef = useRef<string | null>(null)

  const fetchTracePage = useCallback(async (query: TraceQueryState): Promise<RequestTraceListResult> => {
	return queryRequestTraces({
		page: query.page,
		pageSize: tracePageSize,
		aliases: query.aliases,
		failoverCounts: query.failoverCounts,
		statusCodes: query.statusCodes,
	})
  }, [])

  const applyTracePageResult = useCallback((kind: TracePageKind, result: RequestTraceListResult) => {
	if (kind === 'log') {
		setLogTraces(result.items)
		setLogTraceTotal(result.total)
		setLogTraceLoaded(true)
		setLogTraceCatalog({
			aliases: result.availableAliases || [],
			failoverCounts: result.availableFailoverCounts || [],
			statusCodes: result.availableStatusCodes || [],
		})
		setSelectedLogTraceId((current) => resolveSelectedTraceId(current, result.items))
		return
	}
	setNetworkTraces(result.items)
	setNetworkTraceTotal(result.total)
	setNetworkTraceLoaded(true)
	setNetworkTraceCatalog({
		aliases: result.availableAliases || [],
		failoverCounts: result.availableFailoverCounts || [],
		statusCodes: result.availableStatusCodes || [],
	})
	setSelectedNetworkTraceId((current) => resolveSelectedTraceId(current, result.items))
  }, [])

  const loadTraceQuery = useCallback(async (kind: TracePageKind, query: TraceQueryState) => {
	const requestRef = kind === 'log' ? logTraceRequestRef : networkTraceRequestRef
	const loadingKeyRef = kind === 'log' ? logTraceLoadingKeyRef : networkTraceLoadingKeyRef
	const setStatus = kind === 'log' ? setLogTraceStatus : setNetworkTraceStatus
	const queryKey = traceQueryKey(query)
	if (loadingKeyRef.current === queryKey) {
		return
	}
	const requestId = requestRef.current + 1
	requestRef.current = requestId
	loadingKeyRef.current = queryKey
	try {
		const result = await fetchTracePage(query)
		if (requestRef.current !== requestId) {
			return
		}
		applyTracePageResult(kind, result)
		setStatus(i18n.t('messages.fresh'))
	} catch (error) {
		if (requestRef.current === requestId) {
			setStatus(formatError(error))
		}
	} finally {
		if (loadingKeyRef.current === queryKey) {
			loadingKeyRef.current = null
		}
	}
  }, [applyTracePageResult, fetchTracePage])

  const refreshAll = useCallback(async (options?: { syncDesktopPrefs?: boolean }) => {
    const syncDesktopPrefs = options?.syncDesktopPrefs ?? false
    setLoading(true)
    setPrefsStatus(i18n.t('messages.refreshing'))
    try {
      const [metaData, overviewData, providerData, aliasData, proxySettingsData] = await Promise.all([
        getMeta(),
        getOverview(),
        listProviders(),
        listAliases(),
        getProxySettings(),
      ])
      setMeta(metaData)
      setOverview(overviewData)
      setProviders(providerData)
      setAliases(aliasData)
      if (syncDesktopPrefs) {
        setPrefs(overviewData.desktop)
      }
      setProxySettings(proxySettingsData)
      setPrefsStatus(i18n.t('messages.fresh'))
      setProxySettingsStatus(i18n.t('messages.fresh'))
    } catch (error) {
      setPrefsStatus(formatError(error))
      setProxySettingsStatus(formatError(error))
    } finally {
      setLoading(false)
    }
  }, [])

  function applyProxyStatus(status: ProxyStatusView) {
    setOverview((current) => current ? { ...current, proxy: status } : current)
  }

  useEffect(() => {
    void refreshAll({ syncDesktopPrefs: true })
  }, [refreshAll])

  useEffect(() => {
    const applyHash = () => {
      const tab = normalizeTab(window.location.hash)
      setActiveTab(tab)
      if (window.location.hash !== `#${tab}`) {
        window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#${tab}`)
      }
    }
    applyHash()
    window.addEventListener('hashchange', applyHash)
    return () => window.removeEventListener('hashchange', applyHash)
  }, [])

  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const updateTheme = () => setSystemTheme(media.matches ? 'dark' : 'light')
    updateTheme()
    if (typeof media.addEventListener === 'function') {
      media.addEventListener('change', updateTheme)
      return () => media.removeEventListener('change', updateTheme)
    }
    media.addListener(updateTheme)
    return () => media.removeListener(updateTheme)
  }, [])

  useEffect(() => {
    const updateLanguage = () => setSystemLanguage(window.navigator.language || 'en-US')
    updateLanguage()
    window.addEventListener('languagechange', updateLanguage)
    return () => window.removeEventListener('languagechange', updateLanguage)
  }, [])

  const resolvedTheme = resolveThemePreference(prefs.theme, systemTheme)
  const resolvedLanguage = resolveLanguagePreference(prefs.language, systemLanguage as LanguagePreference)
  const providerSearch = providerQuery.trim().toLowerCase()
  const aliasSearch = aliasQuery.trim().toLowerCase()
  const filteredProviders = providers.filter((provider) => providerMatches(provider, providerSearch, providerFilter))
  const filteredAliases = aliases.filter((alias) => aliasMatches(alias, aliasSearch))
  const selectedProvider = providers.find((provider) => provider.id === selectedProviderId) || null
  const selectedAlias = aliases.find((alias) => alias.alias === selectedAliasId) || null
	const targetAlias = aliases.find((alias) => alias.alias === targetForm.alias.trim()) || null
	const providerDetailOpen = providerDetailMode !== 'empty'
	const aliasDetailOpen = aliasDetailMode !== 'empty'
	const targetProtocol = resolveDraftAliasProtocol(targetForm.alias, aliasForm, selectedAlias, aliases, aliasDetailOpen)
	const bindableProviders = providers
		.filter((provider) => provider.protocol === targetProtocol)
		.sort((left, right) => left.id.localeCompare(right.id))
	const bindableModels = selectedTraceModel(targetForm.provider, providers)
  const selectedLogTrace =
	(logTraceDetail?.id === selectedLogTraceId ? logTraceDetail : null) || logTraces.find((trace) => trace.id === selectedLogTraceId) || null
  const selectedNetworkTrace =
	(networkTraceDetail?.id === selectedNetworkTraceId ? networkTraceDetail : null) || networkTraces.find((trace) => trace.id === selectedNetworkTraceId) || null
  const logDetailOpen = selectedLogTraceId !== null && selectedLogTrace !== null
  const networkDetailOpen = selectedNetworkTraceId !== null && selectedNetworkTrace !== null
  const proxyRunning = overview?.proxy.running ?? false
  const logPageCount = tracePageCount(logTraceTotal, tracePageSize)
  const networkPageCount = tracePageCount(networkTraceTotal, tracePageSize)
  const stats = overview
    ? [
        ['overview.providers', String(overview.providerCount)],
        ['overview.aliases', String(overview.aliasCount)],
        ['overview.routableAliases', String(overview.availableAliases.length)],
        ['overview.proxy', overview.proxy.running ? t('status.proxyRunning') : t('status.proxyIdle')],
      ]
    : []

  useEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme
    document.documentElement.style.colorScheme = resolvedTheme
  }, [resolvedTheme])

  useEffect(() => {
    void i18n.changeLanguage(resolvedLanguage)
  }, [resolvedLanguage])

  useEffect(() => {
    if (!activeModal && !confirmIntent) {
      return
    }
    const modal = document.querySelector<HTMLElement>('.modal-card')
    if (!modal) {
      return
    }
    const [firstFocusable] = focusableElements(modal)
    ;(firstFocusable || modal).focus()
  }, [activeModal, confirmIntent])

  useEffect(() => {
    if (activeTab !== 'log' && activeTab !== 'network') {
      return
    }
    let cancelled = false
    const tick = async () => {
		const kind = activeTab === 'log' ? 'log' : 'network'
		const query = kind === 'log' ? logTraceQuery : networkTraceQuery
		if (!cancelled) {
			await loadTraceQuery(kind, query)
		}
    }
    void tick()
    const timer = window.setInterval(() => {
      void tick()
    }, 3000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [activeTab, loadTraceQuery, logTraceQuery, networkTraceQuery])

  useEffect(() => {
    if (activeTab === 'providers' && providerDetailOpen) {
      focusFirstInput(providerDetailRef.current)
    }
  }, [activeTab, providerDetailOpen])

  useEffect(() => {
    if (activeTab === 'aliases' && aliasDetailOpen) {
      focusFirstInput(aliasDetailRef.current)
    }
  }, [activeTab, aliasDetailOpen])

  useEffect(() => {
    if (activeTab === 'log' && logDetailOpen) {
      focusFirstFocusable(logDetailRef.current)
    }
  }, [activeTab, logDetailOpen])

  useEffect(() => {
    if (activeTab === 'network' && networkDetailOpen) {
      focusFirstFocusable(networkDetailRef.current)
    }
  }, [activeTab, networkDetailOpen])

  useEffect(() => {
	if (selectedLogTraceId === null) {
		setLogTraceDetail(null)
		return
	}
	let cancelled = false
	setLogTraceDetail(null)
	void getRequestTrace(selectedLogTraceId)
		.then((trace) => {
			if (!cancelled) {
				setLogTraceDetail(trace)
			}
		})
		.catch((error) => {
			if (!cancelled) {
				setLogTraceStatus(formatError(error))
			}
		})
	return () => {
		cancelled = true
	}
  }, [selectedLogTraceId])

  useEffect(() => {
	if (selectedNetworkTraceId === null) {
		setNetworkTraceDetail(null)
		return
	}
	let cancelled = false
	setNetworkTraceDetail(null)
	void getRequestTrace(selectedNetworkTraceId)
		.then((trace) => {
			if (!cancelled) {
				setNetworkTraceDetail(trace)
			}
		})
		.catch((error) => {
			if (!cancelled) {
				setNetworkTraceStatus(formatError(error))
			}
		})
	return () => {
		cancelled = true
	}
  }, [selectedNetworkTraceId])

	useEffect(() => {
		const shouldLockScroll = providerDetailOpen || aliasDetailOpen || logDetailOpen || networkDetailOpen
		const appShell = document.querySelector<HTMLElement>('.app-shell')
		const sidebar = document.querySelector<HTMLElement>('.sidebar')
		const workspace = document.querySelector<HTMLElement>('.workspace')
    if (!shouldLockScroll) {
      return
    }
    const previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    appShell?.classList.add('shell-locked')
    sidebar?.classList.add('shell-locked')
    workspace?.classList.add('shell-locked')
    return () => {
      document.body.style.overflow = previousOverflow
      appShell?.classList.remove('shell-locked')
      sidebar?.classList.remove('shell-locked')
      workspace?.classList.remove('shell-locked')
    }
	}, [aliasDetailOpen, logDetailOpen, networkDetailOpen, providerDetailOpen])

  useEffect(() => {
    if (providerDetailMode === 'create') {
      return
    }
    if (selectedProviderId && providers.some((provider) => provider.id === selectedProviderId)) {
      return
    }
    setSelectedProviderId(null)
    setEditingProviderId('')
    setProviderForm(emptyProviderForm)
    setProviderDetailMode('empty')
  }, [providerDetailMode, providers, selectedProviderId])

  useEffect(() => {
    if (aliasDetailMode === 'create') {
      return
    }
    if (selectedAliasId && aliases.some((alias) => alias.alias === selectedAliasId)) {
      return
    }
    setSelectedAliasId(null)
    setEditingAliasId('')
    setAliasForm(emptyAliasForm)
    setAliasDetailMode('empty')
  }, [aliasDetailMode, aliases, selectedAliasId])

  function selectTab(tab: TabKey) {
    window.location.hash = tab
  }

  function resetProviderForm() {
    setEditingProviderId('')
    setProviderForm(emptyProviderForm)
	setProviderBaseUrlPings({})
  }

  function resetAliasForm() {
    setEditingAliasId('')
    setAliasForm(emptyAliasForm)
  }

  function selectProviderDetail(provider: ProviderView) {
    setSelectedProviderId(provider.id)
    setEditingProviderId(provider.id)
    setProviderForm(providerFormFromView(provider))
	setProviderBaseUrlPings({})
    setProviderDetailMode('edit')
  }

	function updateProviderBaseUrl(index: number, value: string) {
		setProviderForm((current) => ({
			...current,
			baseUrls: current.baseUrls.map((item, itemIndex) => itemIndex === index ? value : item),
		}))
	}

	function addProviderBaseUrl() {
		setProviderForm((current) => ({ ...current, baseUrls: [...current.baseUrls, ''] }))
	}

	function removeProviderBaseUrl(index: number) {
		setProviderForm((current) => {
			const next = current.baseUrls.filter((_, itemIndex) => itemIndex !== index)
			return { ...current, baseUrls: next.length > 0 ? next : [''] }
		})
		setProviderBaseUrlPings((current) => {
			const next = { ...current }
			const baseUrl = providerForm.baseUrls[index]?.trim()
			if (baseUrl) {
				delete next[baseUrl]
			}
			return next
		})
	}

  function moveProviderBaseUrl(index: number, direction: -1 | 1) {
		setProviderForm((current) => {
			const nextIndex = index + direction
			if (nextIndex < 0 || nextIndex >= current.baseUrls.length) {
				return current
			}
			const next = [...current.baseUrls]
			const [item] = next.splice(index, 1)
			next.splice(nextIndex, 0, item)
			return { ...current, baseUrls: next }
		})
	}

	function reorderProviderBaseUrl(fromIndex: number, toIndex: number) {
		setProviderForm((current) => {
			if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= current.baseUrls.length || toIndex >= current.baseUrls.length) {
				return current
			}
			const next = [...current.baseUrls]
			const [item] = next.splice(fromIndex, 1)
			next.splice(toIndex, 0, item)
			return { ...current, baseUrls: next }
		})
	}

	function onProviderBaseUrlDragStart(event: DragEvent<HTMLDivElement>, index: number) {
		setDraggingProviderBaseUrlIndex(index)
		event.dataTransfer.effectAllowed = 'move'
		event.dataTransfer.setData('text/plain', String(index))
	}

	function onProviderBaseUrlDrop(event: DragEvent<HTMLDivElement>, index: number) {
		event.preventDefault()
		const fromIndex = Number(event.dataTransfer.getData('text/plain'))
		if (Number.isNaN(fromIndex)) {
			setDraggingProviderBaseUrlIndex(null)
			return
		}
		reorderProviderBaseUrl(fromIndex, index)
		setDraggingProviderBaseUrlIndex(null)
	}

  function selectAliasDetail(alias: AliasView) {
    setSelectedAliasId(alias.alias)
    setEditingAliasId(alias.alias)
    setAliasForm(aliasFormFromView(alias))
    setTargetForm((current) => ({ ...current, alias: alias.alias }))
    setAliasDetailMode('edit')
  }

  function closeModal() {
    setActiveModal(null)
  }

  function closeConfirmDialog() {
    setConfirmIntent(null)
  }

  function closeProviderDetail() {
    setSelectedProviderId(null)
    resetProviderForm()
    setProviderDetailMode('empty')
  }

  function closeAliasDetail() {
    setSelectedAliasId(null)
    resetAliasForm()
    setDraggingAliasTargetIndex(null)
    setAliasDetailMode('empty')
  }

  function closeLogDetail() {
    setSelectedLogTraceId(null)
  }

  function closeNetworkDetail() {
    setSelectedNetworkTraceId(null)
  }

  function openProviderCreateModal() {
    resetProviderForm()
    setSelectedProviderId(null)
    setProviderDetailMode('create')
  }

  function openProviderImportModal() {
    setActiveModal('provider-import')
  }

  function openAliasCreateModal() {
    resetAliasForm()
    setSelectedAliasId(null)
    setAliasDetailMode('create')
  }

	function openAliasTargetModal(alias?: string) {
		const aliasName = alias || ''
		const nextProtocol = resolveDraftAliasProtocol(aliasName, aliasForm, selectedAlias, aliases, aliasDetailOpen)
		const nextProvider = providers.find((provider) => provider.protocol === nextProtocol)?.id || ''
		const nextModel = selectedTraceModel(nextProvider, providers)[0] || ''
		setTargetForm({ ...emptyTargetForm, alias: aliasName, provider: nextProvider, model: nextModel })
    setActiveModal('alias-target')
  }

  function trapModalFocus(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'Tab') {
      return
    }
    const focusables = focusableElements(event.currentTarget)
    if (focusables.length === 0) {
      event.preventDefault()
      event.currentTarget.focus()
      return
    }
    const first = focusables[0]
    const last = focusables[focusables.length - 1]
    const active = document.activeElement
    if (event.shiftKey && active === first) {
      event.preventDefault()
      last.focus()
      return
    }
    if (!event.shiftKey && active === last) {
      event.preventDefault()
      first.focus()
    }
  }

  function onModalKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeModal()
    }
  }

  function onConfirmKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeConfirmDialog()
    }
  }

  function onProviderDetailKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeProviderDetail()
    }
  }

  function onAliasDetailKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeAliasDetail()
    }
  }

  function onLogDetailKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeLogDetail()
    }
  }

  function onNetworkDetailKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    trapModalFocus(event)
    if (event.key === 'Escape') {
      closeNetworkDetail()
    }
  }

  function onResourceCardKeyDown(event: KeyboardEvent<HTMLElement>, onActivate: () => void) {
    if (event.key !== 'Enter' && event.key !== ' ') {
      return
    }
    event.preventDefault()
    onActivate()
  }

  async function onSavePrefs(event: FormEvent) {
    event.preventDefault()
    setPrefsStatus(i18n.t('messages.saving'))
    try {
      const saved = await saveDesktopPrefs(prefs)
      setPrefs(saved.prefs)
      await refreshAll()
      setPrefsStatus(desktopPrefsSaveStatus(saved))
    } catch (error) {
      setPrefsStatus(formatError(error))
    }
  }

  async function onSaveProxySettings() {
    setProxySettingsStatus(i18n.t('messages.saving'))
    try {
      const saved = await saveProxySettings(proxySettings)
      setProxySettings(saved.settings)
      await refreshAll()
      setProxySettingsStatus(proxySettingsSaveStatus(saved))
    } catch (error) {
      setProxySettingsStatus(formatError(error))
    }
  }

  async function onRunDoctor() {
    setDoctorStatus(i18n.t('messages.running'))
    try {
      const result = await runDoctor()
      setDoctorResult(result)
      setDoctorStatus(result.error || i18n.t('messages.doctorOk'))
    } catch (error) {
      setDoctorResult(null)
      setDoctorStatus(formatError(error))
    }
  }

  async function onStartProxy() {
    if (proxyActionLoading) {
      return
    }
    setProxyActionLoading(true)
    setPrefsStatus(i18n.t('messages.running'))
    try {
      applyProxyStatus(await startProxy())
      setPrefsStatus(i18n.t('messages.proxyStarted'))
    } catch (error) {
      setPrefsStatus(formatError(error))
    } finally {
      setProxyActionLoading(false)
    }
  }

  async function onStopProxy() {
    if (proxyActionLoading) {
      return
    }
    setProxyActionLoading(true)
    setPrefsStatus(i18n.t('messages.running'))
    try {
      applyProxyStatus(await stopProxy())
      setPrefsStatus(i18n.t('messages.proxyStopped'))
    } catch (error) {
      setPrefsStatus(formatError(error))
    } finally {
      setProxyActionLoading(false)
    }
  }

  async function onPreviewSync() {
    setSyncStatus(i18n.t('messages.previewing'))
    try {
      const result = await previewSync(syncInput)
      setSyncOutput(result)
      setSyncStatus(result.wouldChange ? i18n.t('messages.previewChanges') : i18n.t('messages.previewNoChanges'))
    } catch (error) {
      const message = formatError(error)
      setSyncOutput(message)
      setSyncStatus(message)
    }
  }

  async function onApplySync(event: FormEvent) {
    event.preventDefault()
    setSyncStatus(i18n.t('messages.applying'))
    try {
      const result = await applySync(syncInput)
      setSyncOutput(result)
      setSyncStatus(result.changed ? i18n.t('messages.syncApplied') : i18n.t('messages.syncUpToDate'))
    } catch (error) {
      const message = formatError(error)
      setSyncOutput(message)
      setSyncStatus(message)
    }
  }

  async function onSaveProvider(event: FormEvent) {
    event.preventDefault()
    setProviderStatus(i18n.t('messages.saving'))
    try {
      const input: ProviderUpsertInput = {
        id: providerForm.id.trim(),
        name: providerForm.name.trim(),
        protocol: providerForm.protocol,
        baseUrl: providerForm.baseUrls.map((item) => item.trim()).find(Boolean) || '',
        baseUrls: providerForm.baseUrls.map((item) => item.trim()).filter(Boolean),
        baseUrlStrategy: providerForm.baseUrlStrategy,
        apiKey: providerForm.apiKey,
        headers: parseHeadersText(providerForm.headersText),
        disabled: providerForm.disabled,
        skipModels: providerForm.skipModels,
        clearHeaders: providerForm.clearHeaders,
      }
      const result = await saveProvider(input)
      setSelectedProviderId(result.provider.id)
      setEditingProviderId(result.provider.id)
      setProviderForm(providerFormFromView(result.provider))
      setProviderDetailMode('edit')
      setProviderStatus(providerSaveStatus(result))
      await refreshAll()
    } catch (error) {
      setProviderStatus(formatError(error))
    }
  }

  async function onRefreshProviderModels(input: ProviderRefreshModelsInput) {
	setProviderStatus(i18n.t('providers.statusRefreshingModels', { id: input.id }))
	try {
		const result = await refreshProviderModels(input)
		setSelectedProviderId(result.provider.id)
		setEditingProviderId(result.provider.id)
		setProviderForm(providerFormFromView(result.provider))
		setProviderDetailMode('edit')
		setProviderStatus(withWarnings(i18n.t('providers.statusRefreshedModels', { id: result.provider.id }), result.warnings))
		await refreshAll()
	} catch (error) {
		setProviderStatus(formatError(error))
	}
  }

	async function onPingProviderBaseUrl(input: ProviderPingInput) {
		setProviderStatus(i18n.t('providers.statusPingingBaseUrl', { baseUrl: input.baseUrl }))
		try {
			const result = await pingProviderBaseUrl(input)
			setProviderBaseUrlPings((current) => ({ ...current, [result.baseUrl]: result }))
			setProviderStatus(
				result.reachable
					? i18n.t('providers.statusPingedBaseUrl', { baseUrl: result.baseUrl, latency: result.latencyMs })
					: (result.error || i18n.t('providers.pingUnreachable')),
			)
		} catch (error) {
			setProviderStatus(formatError(error))
		}
	}

  async function onImportProviders(event: FormEvent) {
    event.preventDefault()
    setProviderStatus(i18n.t('messages.importing'))
    try {
      const result = await importProviders({
        sourcePath: providerImportForm.sourcePath?.trim(),
        overwrite: providerImportForm.overwrite,
      })
      setProviderImportForm(emptyProviderImport)
      closeModal()
      setProviderStatus(providerImportStatus(result))
      await refreshAll()
    } catch (error) {
      setProviderStatus(formatError(error))
    }
  }

  function onEditProvider(provider: ProviderView) {
    selectProviderDetail(provider)
    setProviderStatus(i18n.t('providers.statusEditing', { id: provider.id }))
  }

  async function onToggleProvider(provider: ProviderView) {
    setProviderStatus(
      i18n.t(provider.disabled ? 'providers.statusEnabling' : 'providers.statusDisabling', { id: provider.id }),
    )
    try {
      await setProviderState({ id: provider.id, disabled: !provider.disabled })
      setProviderStatus(
        i18n.t(provider.disabled ? 'providers.statusEnabled' : 'providers.statusDisabled', { id: provider.id }),
      )
      await refreshAll()
    } catch (error) {
      setProviderStatus(formatError(error))
    }
  }

  async function onDeleteProvider(id: string) {
    setProviderStatus(i18n.t('providers.statusDeleting', { id }))
    try {
      await deleteProvider(id)
      if (selectedProviderId === id) {
        setSelectedProviderId(null)
        resetProviderForm()
        setProviderDetailMode('empty')
      }
      setProviderStatus(i18n.t('providers.statusDeleted', { id }))
      await refreshAll()
    } catch (error) {
      setProviderStatus(formatError(error))
    }
  }

  async function onSaveAlias(event: FormEvent) {
    event.preventDefault()
    setAliasStatus(i18n.t('messages.saving'))
    try {
      const input: AliasUpsertInput = {
        alias: aliasForm.alias.trim(),
        displayName: aliasForm.displayName.trim(),
        protocol: aliasForm.protocol,
        disabled: aliasForm.disabled,
      }
      await saveAlias(input)
      setSelectedAliasId(input.alias)
      setEditingAliasId(input.alias)
      setTargetForm((current) => ({ ...current, alias: input.alias }))
      setAliasDetailMode('edit')
      setAliasStatus(i18n.t('aliases.statusSaved', { alias: input.alias }))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  function onEditAlias(alias: AliasView) {
    selectAliasDetail(alias)
    setAliasStatus(i18n.t('aliases.statusEditing', { alias: alias.alias }))
  }

  async function onDeleteAlias(alias: string) {
    setAliasStatus(i18n.t('aliases.statusDeleting', { alias }))
    try {
      await deleteAlias(alias)
      if (selectedAliasId === alias) {
        setSelectedAliasId(null)
        resetAliasForm()
        setAliasDetailMode('empty')
      }
      setAliasStatus(i18n.t('aliases.statusDeleted', { alias }))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  async function onBindTarget(event: FormEvent) {
    event.preventDefault()
    setAliasStatus(i18n.t('aliases.statusBinding'))
    try {
      const input: AliasTargetInput = {
        alias: targetForm.alias.trim(),
        provider: targetForm.provider.trim(),
        model: targetForm.model.trim(),
        disabled: targetForm.disabled,
      }
      await bindAliasTarget(input)
      setTargetForm((current) => ({ ...emptyTargetForm, alias: current.alias }))
      closeModal()
      setAliasStatus(i18n.t('aliases.statusBound', input))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

	function onTargetAliasChange(event: ChangeEvent<HTMLInputElement>) {
		const alias = event.target.value
		const nextProtocol = resolveDraftAliasProtocol(alias, aliasForm, selectedAlias, aliases, aliasDetailOpen)
		setTargetForm((current) => {
			const nextProvider = current.provider && providers.some((provider) => provider.id === current.provider && provider.protocol === nextProtocol)
        ? current.provider
        : providers.find((provider) => provider.protocol === nextProtocol)?.id || ''
      const nextModels = selectedTraceModel(nextProvider, providers)
      const nextModel = nextProvider === current.provider && nextModels.includes(current.model) ? current.model : nextModels[0] || ''
      return { ...current, alias, provider: nextProvider, model: nextModel }
    })
  }

  function onTargetProviderChange(providerId: string) {
	const models = selectedTraceModel(providerId, providers)
	setTargetForm((current) => ({
		...current,
		provider: providerId,
		model: models.includes(current.model) ? current.model : models[0] || '',
	}))
  }

	function updateLogTraceQuery(update: (current: TraceQueryState) => TraceQueryState) {
	setLogTraceQuery((current) => update(current))
	setSelectedLogTraceId(null)
  }

	function loadLogTracePage(page: number) {
		const nextQuery = { ...logTraceQuery, page }
		updateLogTraceQuery(() => nextQuery)
	}

	function setLogAliasFilter(alias: string, selected: boolean) {
		updateLogTraceQuery((current) => ({
			...current,
			page: 1,
			aliases: selected
				? [...current.aliases, alias].sort((left, right) => left.localeCompare(right))
				: current.aliases.filter((item) => item !== alias),
		}))
	}

	function clearLogAliasFilter() {
		updateLogTraceQuery((current) => ({ ...current, page: 1, aliases: [] }))
	}

  function updateNetworkTraceQuery(update: (current: TraceQueryState) => TraceQueryState) {
	setNetworkTraceQuery((current) => update(current))
	setSelectedNetworkTraceId(null)
  }

	function loadNetworkTracePage(page: number) {
		const nextQuery = { ...networkTraceQuery, page }
		updateNetworkTraceQuery(() => nextQuery)
	}

  async function onUnbindTarget(alias: string, provider: string, model: string) {
    setAliasStatus(i18n.t('aliases.statusRemoving', { alias, provider, model }))
    try {
      await unbindAliasTarget({ alias, provider, model, disabled: false })
      setAliasStatus(i18n.t('aliases.statusRemoved', { alias, provider, model }))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  async function onToggleTarget(alias: string, provider: string, model: string, enabled: boolean) {
    setAliasStatus(
      i18n.t(enabled ? 'aliases.statusDisabling' : 'aliases.statusEnabling', { alias, provider, model }),
    )
    try {
      await setAliasTargetState({ alias, provider, model, disabled: enabled })
      setAliasStatus(
        i18n.t(enabled ? 'aliases.statusDisabled' : 'aliases.statusEnabled', { alias, provider, model }),
      )
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  function reorderAliasTargetItems(targets: AliasTargetView[], fromIndex: number, toIndex: number): AliasTargetView[] | null {
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || fromIndex >= targets.length || toIndex >= targets.length) {
      return null
    }
    const next = [...targets]
    const [item] = next.splice(fromIndex, 1)
    next.splice(toIndex, 0, item)
    return next
  }

  async function persistAliasTargetOrder(alias: AliasView, targets: AliasTargetView[]) {
    setAliasStatus(i18n.t('aliases.statusReorderingTarget', { alias: alias.alias }))
    try {
      const updated = await reorderAliasTargets({
        alias: alias.alias,
        targets: targets.map((target) => ({ provider: target.provider, model: target.model })),
      })
      setAliases((current) => current.map((item) => (item.alias === updated.alias ? updated : item)))
      setSelectedAliasId(updated.alias)
      setAliasStatus(i18n.t('aliases.statusReorderedTarget', { alias: updated.alias }))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  async function moveAliasTarget(index: number, direction: -1 | 1) {
    if (!selectedAlias) {
      return
    }
    const next = reorderAliasTargetItems(selectedAlias.targets, index, index + direction)
    if (!next) {
      return
    }
    await persistAliasTargetOrder(selectedAlias, next)
  }

  async function reorderAliasTarget(fromIndex: number, toIndex: number) {
    if (!selectedAlias) {
      return
    }
    const next = reorderAliasTargetItems(selectedAlias.targets, fromIndex, toIndex)
    if (!next) {
      return
    }
    await persistAliasTargetOrder(selectedAlias, next)
  }

  function onAliasTargetDragStart(event: DragEvent<HTMLDivElement>, index: number) {
    setDraggingAliasTargetIndex(index)
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(index))
  }

  function onAliasTargetDrop(event: DragEvent<HTMLDivElement>, index: number) {
    event.preventDefault()
    const fromIndex = Number(event.dataTransfer.getData('text/plain'))
    if (Number.isNaN(fromIndex)) {
      setDraggingAliasTargetIndex(null)
      return
    }
    void reorderAliasTarget(fromIndex, index)
    setDraggingAliasTargetIndex(null)
  }

  async function onToggleProxy() {
    if (overview?.proxy.running) {
      await onStopProxy()
      return
    }
    await onStartProxy()
  }

  async function onExportConfig() {
    setConfigTransferStatus(i18n.t('messages.running'))
    try {
      const result = await exportConfig()
      downloadTextFile(configFileName(result.configPath), result.content)
      setConfigTransferStatus(i18n.t('settings.exportDone', { path: result.configPath }))
    } catch (error) {
      setConfigTransferStatus(formatError(error))
    }
  }

  async function onImportConfig() {
    const content = configImportText.trim()
    if (configImportMode === 'file' && (!configImportFileName || !content)) {
      setConfigTransferStatus(i18n.t('settings.fileEmpty'))
      return
    }
    if (!content) {
      setConfigTransferStatus(i18n.t('settings.importEmpty'))
      return
    }
    setConfigTransferStatus(i18n.t('messages.applying'))
    try {
      const result = await importConfig({ content })
      setConfigTransferStatus(withWarnings(i18n.t('settings.importDone', { path: result.configPath }), result.warnings))
      setConfigImportText('')
      setConfigImportFileName('')
      await refreshAll()
    } catch (error) {
      setConfigTransferStatus(formatError(error))
    }
  }

  async function onSelectConfigFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) {
      return
    }
    try {
      const text = await file.text()
      setConfigImportText(text)
      setConfigImportFileName(file.name)
      setConfigTransferStatus(i18n.t('settings.fileLoaded', { name: file.name }))
    } catch (error) {
      setConfigTransferStatus(formatError(error))
    } finally {
      event.target.value = ''
    }
  }

  async function onConfirmAction() {
    const intent = confirmIntent
    if (!intent) {
      return
    }
    closeConfirmDialog()
    if (intent.kind === 'delete-provider') {
      await onDeleteProvider(intent.id)
      return
    }
    if (intent.kind === 'delete-alias') {
      await onDeleteAlias(intent.alias)
      return
    }
    await onUnbindTarget(intent.alias, intent.provider, intent.model)
  }

  const confirmTitle = confirmIntent
    ? confirmIntent.kind === 'delete-provider'
      ? t('confirm.deleteProviderTitle')
      : confirmIntent.kind === 'delete-alias'
        ? t('confirm.deleteAliasTitle')
        : t('confirm.unbindTargetTitle')
    : ''

  function openRepository() {
    if (!GITHUB_REPOSITORY_URL) {
      return
    }
    void openExternalURL(GITHUB_REPOSITORY_URL)
  }

  const confirmMessage = confirmIntent
    ? confirmIntent.kind === 'delete-provider'
      ? t('messages.confirmDeleteProvider', { id: confirmIntent.id })
      : confirmIntent.kind === 'delete-alias'
        ? t('messages.confirmDeleteAlias', { alias: confirmIntent.alias })
        : t('messages.confirmUnbindTarget', {
            alias: confirmIntent.alias,
            provider: confirmIntent.provider,
            model: confirmIntent.model,
          })
    : ''

  const importModeLabelId = useId()
  const logAliasFilterId = useId()
  const providerImportTitleId = useId()
  const aliasTargetTitleId = useId()
  const providerDetailTitleId = useId()
  const aliasDetailTitleId = useId()
  const logDetailTitleId = useId()
  const networkDetailTitleId = useId()
  const confirmTitleId = useId()

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-brand">
          <div className="sidebar-brand-line" />
          <div className="sidebar-brand-header">
            <button
              type="button"
              className="brand-github"
              onClick={openRepository}
              disabled={!GITHUB_REPOSITORY_URL}
              aria-label="Open project repository"
              title={GITHUB_REPOSITORY_URL || 'Set GITHUB_REPOSITORY_URL in App.tsx'}
            >
              <img src={githubMark} alt="" />
            </button>
            <h1>{t('app.brand')}</h1>
          </div>
          <div className="brand-meta">
            <span className="badge">v{meta.version || t('app.dev')}</span>
          </div>
        </div>

        <nav className="nav-list" aria-label="Primary">
          {tabs.map((tab) => (
            <button
              key={tab}
              type="button"
              className={`nav-item ${activeTab === tab ? 'active' : ''}`}
              onClick={() => selectTab(tab)}
            >
              <span className="nav-title">{t(`nav.${tab}.title`)}</span>
            </button>
          ))}
        </nav>
      </aside>

      <main className="workspace">
        {activeTab === 'overview' ? (
          <section className="tab-layout overview-layout">
            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('overview.title')}</h3>
                  <p className="subtle">{t('overview.subtitle')}</p>
                </div>
                <span className={`badge ${overview?.proxy.running ? 'live' : 'idle'}`}>
                  {overview?.proxy.running ? t('status.proxyRunning') : t('status.proxyIdle')}
                </span>
              </div>
              <div className="stats-grid">
                {stats.map(([label, value]) => (
                  <div className="stat-card" key={label}>
                    <span className="stat-label">{t(label)}</span>
                    <span className="stat-value">{value}</span>
                  </div>
                ))}
              </div>
            </article>

            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('overview.actionsTitle')}</h3>
                  <p className="subtle">{prefsStatus || t('messages.noData')}</p>
                </div>
              </div>
              <div className="action-grid">
                <button
                  type="button"
                  className={overview?.proxy.running ? 'danger' : 'primary'}
                  disabled={proxyActionLoading}
                  onClick={() => void onToggleProxy()}
                >
                  {overview?.proxy.running ? t('actions.stopProxy') : t('actions.startProxy')}
                </button>
                <button type="button" onClick={() => void onRunDoctor()}>
                  {t('actions.runDoctor')}
                </button>
                <button type="button" onClick={() => void refreshAll()} disabled={loading}>
                  {t('actions.refresh')}
                </button>
                <button type="button" onClick={() => selectTab('settings')}>
                  {t('nav.settings.title')}
                </button>
              </div>
            </article>

            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('overview.environmentTitle')}</h3>
                  <p className="subtle">{t('overview.environmentSubtitle')}</p>
                </div>
              </div>
              <dl className="info-grid">
                <div>
                  <dt>{t('overview.version')}</dt>
                  <dd>{meta.version || t('app.dev')}</dd>
                </div>
                <div>
                  <dt>{t('overview.startedAt')}</dt>
                  <dd>{overview?.proxy.startedAt ? String(overview.proxy.startedAt) : '-'}</dd>
                </div>
                <div>
                  <dt>{t('overview.lastError')}</dt>
                  <dd>{overview?.proxy.lastError || '-'}</dd>
                </div>
              </dl>
            </article>

            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('overview.doctorTitle')}</h3>
                  <p className={`subtle ${doctorResult?.error ? 'tone-error' : 'tone-ok'}`}>{doctorStatus}</p>
                </div>
              </div>
              <div className="issue-stack">
                {doctorResult ? (
                  doctorResult.error ? (
                    <p className="tone-error">{doctorResult.error}</p>
                  ) : doctorResult.report.issues.length > 0 ? (
                    <>
                      {renderReconciliationSummary(doctorResult.report.summary)}
                      {doctorResult.report.issues.map((issue, index) => renderIssue(issue, `${index}-${issue.code}-${issue.message}`))}
                    </>
                  ) : (
                    <p className="tone-ok">{t('overview.doctorHealthy')}</p>
                  )
                ) : (
                  <p className="subtle">{t('overview.doctorReady')}</p>
                )}
              </div>
            </article>

            <article className="panel panel-full">
              <div className="panel-header">
                <div>
                  <h3>{t('overview.debugTitle')}</h3>
                  <p className="subtle">{t('overview.debugSummary')}</p>
                </div>
              </div>
              <details className="details-toggle">
                <summary>{t('overview.debugSummary')}</summary>
                <pre className="details">{overview ? pretty(overviewDebugSnapshot(overview)) : t('messages.loading')}</pre>
              </details>
            </article>
          </section>
        ) : null}

        {activeTab === 'providers' ? (
          <section className="tab-layout providers-layout">
            <article className="panel panel-fill list-column">
              <div className="panel-header">
                <div>
                  <h3>{t('providers.title')}</h3>
                  <p className="subtle">
                    {providerDetailOpen
                      ? providerDetailMode === 'create'
                        ? t('providers.formCreateTitle')
                        : selectedProviderId
                          ? t('providers.formEditTitle', { id: selectedProviderId })
                          : t('providers.subtitle')
                      : t('providers.subtitle')}
                  </p>
                </div>
                <div className="list-header-actions">
                  <span className="subtle list-status-text">
                    {providerStatus ||
                      t('providers.listCount', { shown: filteredProviders.length, total: providers.length })}
                  </span>
                  <div className="toolbar list-toolbar-actions">
                    {providerDetailOpen ? (
                      <button type="button" onClick={closeProviderDetail}>
                        {t('actions.close')}
                      </button>
                    ) : null}
                    <button type="button" className="primary" onClick={openProviderCreateModal}>
                      {t('actions.newProvider')}
                    </button>
                    <button type="button" onClick={openProviderImportModal}>
                      {t('actions.import')}
                    </button>
                  </div>
                </div>
              </div>

              <div className="list-toolbar">
                <div className="filter-group filter-group-search">
                  <span className="filter-group-label">{t('providers.search')}</span>
                  <label className="search-field">
                    <input
                      type="text"
                      value={providerQuery}
                      onChange={(event) => setProviderQuery(event.target.value)}
                      placeholder={t('providers.searchPlaceholder')}
                    />
                  </label>
                </div>
                <div className="filter-group filter-group-select">
                  <span className="filter-group-label">{t('providers.filter')}</span>
                  <label>
                    <select
                      value={providerFilter}
                      onChange={(event) => setProviderFilter(event.target.value as FilterState)}
                    >
                      <option value="all">{t('providers.filterAll')}</option>
                      <option value="enabled">{t('providers.filterEnabled')}</option>
                      <option value="disabled">{t('providers.filterDisabled')}</option>
                    </select>
                  </label>
                </div>
              </div>

              <div className="scroll-list compact-list">
                {providers.length === 0 ? (
                  <article className="empty-card">
                    <div className="empty-illustration" aria-hidden="true">◌</div>
                    <span className="empty-kicker">{t('providers.title')}</span>
                    <h4>{t('providers.empty')}</h4>
                    <p className="subtle">{t('providers.emptyHint')}</p>
                    <div className="toolbar">
                      <button type="button" className="primary" onClick={openProviderCreateModal}>
                        {t('actions.newProvider')}
                      </button>
                      <button type="button" onClick={openProviderImportModal}>
                        {t('actions.import')}
                      </button>
                    </div>
                  </article>
                ) : null}
                {providers.length > 0 && filteredProviders.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('providers.noMatches')}</h4>
                    <p className="subtle">{t('providers.noMatchesHint')}</p>
                  </article>
                ) : null}
                {filteredProviders.map((provider) => (
                  <article
                    key={provider.id}
                    className={`resource-card ${providerDetailOpen && selectedProviderId === provider.id ? 'active' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => onEditProvider(provider)}
                    onKeyDown={(event) => onResourceCardKeyDown(event, () => onEditProvider(provider))}
                  >
                    <div className="resource-card-top">
                      <div className="resource-card-heading">
                        <div className="resource-card-titlewrap">
                          <strong className="resource-card-title">{provider.name || provider.id}</strong>
                          <code className="resource-card-code">{provider.id}</code>
                        </div>
                        <p className="resource-card-subtitle resource-card-subtitle-multiline">{providerEffectiveBaseUrls(provider).join('\n')}</p>
                      </div>
                      <div className="resource-card-side">
                        <span className={`badge status-badge ${provider.disabled ? 'idle' : 'live'}`}>
                          {provider.disabled ? t('status.disabled') : t('status.enabled')}
                        </span>
                      </div>
                    </div>
                    <div className="resource-card-meta">
                      <div className="resource-meta-item resource-meta-route">
                        <span className="resource-meta-label">{t('common.protocol')}</span>
                        <span className={protocolBadgeClass(provider.protocol)}>{protocolLabel(provider.protocol)}</span>
                      </div>
                      <div className="resource-meta-item resource-meta-route">
                        <span className="resource-meta-label">{t('providers.cardBaseUrl')}</span>
                        <span className="resource-meta-value">{providerEffectiveBaseUrls(provider).length}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('providers.cardModels')}</span>
                        <span className="resource-meta-value">{t('providers.modelsCount', { count: provider.models?.length || 0 })}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('providers.cardApiKey')}</span>
                        <span className="resource-meta-value">{provider.apiKeyMasked || t('providers.apiKeyNotSet')}</span>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'aliases' ? (
          <section className="tab-layout aliases-layout">
            <article className="panel panel-fill list-column">
              <div className="panel-header">
                <div>
                  <h3>{t('aliases.title')}</h3>
                  <p className="subtle">
                    {aliasDetailOpen
                      ? aliasDetailMode === 'create'
                        ? t('aliases.formCreateTitle')
                        : selectedAliasId
                          ? t('aliases.formEditTitle', { alias: selectedAliasId })
                          : t('aliases.subtitle')
                      : t('aliases.subtitle')}
                  </p>
                </div>
                <div className="list-header-actions">
                  <span className="subtle list-status-text">
                    {aliasStatus || t('aliases.listCount', { shown: filteredAliases.length, total: aliases.length })}
                  </span>
                  <div className="toolbar list-toolbar-actions">
                    {aliasDetailOpen ? (
                      <button type="button" onClick={closeAliasDetail}>
                        {t('actions.close')}
                      </button>
                    ) : null}
                    <button type="button" className="primary" onClick={openAliasCreateModal}>
                      {t('actions.newAlias')}
                    </button>
                    <button type="button" onClick={() => openAliasTargetModal()}>
                      {t('actions.bind')}
                    </button>
                  </div>
                </div>
              </div>

              <div className="list-toolbar list-toolbar-single">
                <div className="filter-group filter-group-search">
                  <span className="filter-group-label">{t('aliases.search')}</span>
                  <label className="search-field">
                    <input
                      type="text"
                      value={aliasQuery}
                      onChange={(event) => setAliasQuery(event.target.value)}
                      placeholder={t('aliases.searchPlaceholder')}
                    />
                  </label>
                </div>
              </div>

              <div className="scroll-list compact-list">
                {aliases.length === 0 ? (
                  <article className="empty-card">
                    <div className="empty-illustration" aria-hidden="true">◎</div>
                    <span className="empty-kicker">{t('aliases.title')}</span>
                    <h4>{t('aliases.empty')}</h4>
                    <p className="subtle">{t('aliases.emptyHint')}</p>
                    <div className="toolbar">
                      <button type="button" className="primary" onClick={openAliasCreateModal}>
                        {t('actions.newAlias')}
                      </button>
                      <button type="button" onClick={() => openAliasTargetModal()}>
                        {t('actions.bind')}
                      </button>
                    </div>
                  </article>
                ) : null}
                {aliases.length > 0 && filteredAliases.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('aliases.noMatches')}</h4>
                    <p className="subtle">{t('aliases.noMatchesHint')}</p>
                  </article>
                ) : null}
                {filteredAliases.map((alias) => (
                  <article
                    key={alias.alias}
                    className={`resource-card ${aliasDetailOpen && selectedAliasId === alias.alias ? 'active' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => onEditAlias(alias)}
                    onKeyDown={(event) => onResourceCardKeyDown(event, () => onEditAlias(alias))}
                  >
                    <div className="resource-card-top">
                      <div className="resource-card-heading">
                        <div className="resource-card-titlewrap">
                          <strong className="resource-card-title">{alias.displayName || alias.alias}</strong>
                          <code className="resource-card-code">{alias.alias}</code>
                        </div>
                        <p className="resource-card-subtitle">
                          {alias.targets[0]
                            ? `${alias.targets[0].provider}/${alias.targets[0].model}`
                            : t('aliases.noTargets')}
                        </p>
                      </div>
                      <div className="resource-card-side">
                        <span className={`badge status-badge ${alias.enabled ? 'live' : 'idle'}`}>
                          {alias.enabled ? t('status.enabled') : t('status.disabled')}
                        </span>
                      </div>
                    </div>
                    <div className="resource-card-meta">
                      <div className="resource-meta-item resource-meta-route">
                        <span className="resource-meta-label">{t('common.protocol')}</span>
                        <span className={protocolBadgeClass(alias.protocol)}>{protocolLabel(alias.protocol)}</span>
                      </div>
                      <div className="resource-meta-item resource-meta-route">
                        <span className="resource-meta-label">{t('aliases.cardRoute')}</span>
                        <span className="resource-meta-value">{t('aliases.routable', { available: alias.availableTargetCount, total: alias.targetCount })}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('aliases.cardTargets')}</span>
                        <span className="resource-meta-value">{t('aliases.targetsCount', { count: alias.targets.length })}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('aliases.cardPrimary')}</span>
                        <span className="resource-meta-value">
                          {alias.targets[0] ? `${alias.targets[0].provider}/${alias.targets[0].model}` : t('aliases.noTargets')}
                        </span>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'sync' ? (
          <section className="tab-layout sync-layout">
            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('sync.syncTitle')}</h3>
                  <p className="subtle">{t('sync.subtitle')}</p>
                </div>
                <span className="subtle">{syncStatus}</span>
              </div>
              <form className="stack" onSubmit={(event) => void onApplySync(event)}>
                <label>
                  <span>{t('sync.targetPath')}</span>
                  <input
                    type="text"
                    value={syncInput.target || ''}
                    onChange={(event) => setSyncInput((current) => ({ ...current, target: event.target.value }))}
                    placeholder={t('sync.placeholderTargetPath')}
                  />
                </label>
                <label>
                  <span>{t('sync.model')}</span>
                  <input
                    type="text"
                    value={syncInput.setModel || ''}
                    onChange={(event) => setSyncInput((current) => ({ ...current, setModel: event.target.value }))}
                    placeholder={t('sync.placeholderModel')}
                  />
                </label>
                <label>
                  <span>{t('sync.smallModel')}</span>
                  <input
                    type="text"
                    value={syncInput.setSmallModel || ''}
                    onChange={(event) =>
                      setSyncInput((current) => ({ ...current, setSmallModel: event.target.value }))
                    }
                    placeholder={t('sync.placeholderModel')}
                  />
                </label>
                <div className="toolbar">
                  <button type="button" onClick={() => void onPreviewSync()}>
                    {t('actions.preview')}
                  </button>
                  <button type="submit" className="primary">
                    {t('actions.apply')}
                  </button>
                </div>
              </form>
            </article>

            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('sync.outputTitle')}</h3>
                </div>
              </div>
              {typeof syncOutput === 'string' ? (
                <pre className="details">{syncOutput || t('messages.noData')}</pre>
              ) : syncOutput ? (
                <div className="stack-blocks">
                  {renderReconciliationSummary(syncOutput.summary)}
                  <div className="issue-card">
                    <div className="issue-card-head">
                      <span className={`badge status-badge ${syncOutputChanged(syncOutput) ? 'live' : 'idle'}`}>
                        {syncOutputChanged(syncOutput) ? 'changed' : 'no change'}
                      </span>
                      <code>{syncOutput.targetPath}</code>
                    </div>
                    <div className="subtle">
                      {(syncOutput.protocols || []).map((provider) => `${provider.key}(${provider.aliasNames.length})`).join(' · ') || t('messages.noData')}
                    </div>
                  </div>
                  {syncOutput.doctorIssues && syncOutput.doctorIssues.length > 0 ? (
                    <div className="issue-stack">
                      {syncOutput.doctorIssues.map((issue, index) => renderIssue(issue, `sync-${index}-${issue.code}-${issue.message}`))}
                    </div>
                  ) : null}
                  <details className="details-toggle">
                    <summary>{t('sync.debugSummary')}</summary>
                    <pre className="details">{pretty(syncOutput)}</pre>
                  </details>
                </div>
              ) : (
                <pre className="details">{t('messages.noData')}</pre>
              )}
            </article>

            <article className="panel panel-full">
              <div className="panel-header">
                <div>
                  <h3>{t('sync.doctorTitle')}</h3>
                  <p className={`subtle ${doctorResult?.error ? 'tone-error' : 'tone-ok'}`}>{doctorStatus}</p>
                </div>
              </div>
              {doctorResult ? (
                <div className="stack-blocks">
                  {doctorResult.error ? <p className="tone-error">{doctorResult.error}</p> : null}
                  <div className="issue-stack">
                    {doctorResult.report.issues.length > 0 ? (
                      <>
                        {renderReconciliationSummary(doctorResult.report.summary)}
                        {doctorResult.report.issues.map((issue, index) => renderIssue(issue, `${index}-${issue.code}-${issue.message}`))}
                      </>
                    ) : (
                      <p className="tone-ok">{t('sync.noIssues')}</p>
                    )}
                  </div>
                  <details className="details-toggle">
                    <summary>{t('sync.debugSummary')}</summary>
                    <pre className="details">{pretty(doctorResult)}</pre>
                  </details>
                </div>
              ) : (
                <p className="subtle">{t('sync.doctorHint')}</p>
              )}
            </article>
          </section>
        ) : null}

        {activeTab === 'log' ? (
          <section className="tab-layout trace-tab-layout">
            <article className="panel panel-fill list-column trace-panel">
              <div className="panel-header">
                <div>
                  <h3>{t('log.title')}</h3>
                  <p className="subtle">{logTraceStatus || t('log.subtitle')}</p>
                </div>
                <div className="list-header-actions">
	                  <span className="subtle list-status-text">{logTraceLoaded ? t('log.count', { count: logTraceTotal }) : t('messages.loading')}</span>
                </div>
              </div>
	              <div className="list-toolbar trace-toolbar">
	                <div className="filter-group filter-group-alias">
	                  <span className="filter-group-label" id={logAliasFilterId}>{t('log.aliasFilter')}</span>
	                  <details className="filter-popover">
	                    <summary className="filter-popover-trigger" aria-labelledby={logAliasFilterId}>
	                      <span>{logTraceQuery.aliases.length > 0 ? t('log.aliasSelectedCount', { count: logTraceQuery.aliases.length }) : t('log.aliasAll')}</span>
	                      <span className="filter-popover-caret" aria-hidden="true" />
	                    </summary>
	                    <div className="filter-popover-panel">
	                      <div className="filter-popover-head">
	                        <span className="filter-group-label">{t('log.aliasFilter')}</span>
	                        <button type="button" className="filter-link-button" onClick={clearLogAliasFilter} disabled={logTraceQuery.aliases.length === 0}>
	                          {t('log.aliasClear')}
	                        </button>
	                      </div>
	                      <div className="filter-option-list">
	                        {logTraceCatalog.aliases.length > 0 ? logTraceCatalog.aliases.map((alias) => (
	                          <label className="filter-option" key={alias}>
	                            <input
	                              type="checkbox"
	                              checked={logTraceQuery.aliases.includes(alias)}
	                              onChange={(event) => setLogAliasFilter(alias, event.target.checked)}
	                            />
	                            <span>{alias}</span>
	                          </label>
	                        )) : <span className="subtle small-text">{t('log.aliasEmpty')}</span>}
	                      </div>
	                    </div>
	                  </details>
	                </div>
	                <div className="filter-group">
	                  <span className="filter-group-label">{t('log.failoverFilter')}</span>
	                  <div className="filter-chip-grid">
	                    {logTraceCatalog.failoverCounts.map((count) => (
	                      <button
	                        key={count}
	                        type="button"
	                        className={logTraceQuery.failoverCounts.includes(count) ? 'filter-chip active' : 'filter-chip'}
	                        onClick={() => updateLogTraceQuery((current) => ({ ...current, page: 1, failoverCounts: toggleNumberFilter(current.failoverCounts, count) }))}
	                      >
	                        {t('log.failoverCountValue', { count })}
	                      </button>
	                    ))}
	                  </div>
	                </div>
	              </div>
	              <div className="scroll-list compact-list trace-scroll-list">
	                {logTraceLoaded && logTraces.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('log.empty')}</h4>
                    <p className="subtle">{t('log.emptyHint')}</p>
                  </article>
                ) : null}
	                {logTraces.length > 0 ? (
					<div className="trace-table trace-table-log" role="table" aria-label={t('log.title')}>
						<div className="trace-table-header" role="row">
							<span className="trace-table-head" role="columnheader">{t('log.tableTime')}</span>
							<span className="trace-table-head" role="columnheader">{t('log.tableModel')}</span>
							<span className="trace-table-head" role="columnheader">{t('log.tablePerformance')}</span>
							<span className="trace-table-head" role="columnheader">{t('log.tableTokens')}</span>
							<span className="trace-table-head trace-table-head-end" role="columnheader">{t('log.tableStatus')}</span>
						</div>
						<div className="trace-table-body">
							{logTraces.map((trace) => (
								<article
									key={trace.id}
									className={`trace-table-row ${logDetailOpen && selectedLogTrace?.id === trace.id ? 'active' : ''}`}
									role="button"
									tabIndex={0}
									onClick={() => setSelectedLogTraceId(trace.id)}
									onKeyDown={(event) => onResourceCardKeyDown(event, () => setSelectedLogTraceId(trace.id))}
								>
									<div className="trace-table-cell" role="cell" data-label={t('log.tableTime')}>
										<span className="trace-mono">{formatCompactDateTime(trace.startedAt)}</span>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('log.tableModel')}>
										<div className="trace-model-cell">
											<div className="trace-model-line">
												<strong className="trace-model-name">{traceDisplayModel(trace)}</strong>
												<span className={protocolBadgeClass(trace.protocol)}>{protocolLabel(trace.protocol)}</span>
												{trace.stream ? <span className="trace-mini-tag">{t('log.stream')}</span> : null}
												{trace.failover ? <span className="trace-mini-tag">{t('log.failover')}</span> : null}
											</div>
											<span className="trace-table-muted">{trace.finalProvider || tracePrimaryText(trace)}</span>
										</div>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('log.tablePerformance')}>
										<div className="trace-table-metric">
											<span className="trace-mono">
												{`${formatCompactDuration(trace.firstByteMs)} / ${formatCompactDuration(trace.durationMs)} / ${formatCompactTokenRate(trace)}`}
											</span>
											<TraceInfoPopover label={t('log.performanceInfo')}>
												<dl className="trace-popover-list">
													<div>
														<dt>{t('log.firstByte')}</dt>
														<dd className="trace-mono">{formatCompactDuration(trace.firstByteMs)}</dd>
													</div>
													<div>
														<dt>{t('log.totalTime')}</dt>
														<dd className="trace-mono">{formatCompactDuration(trace.durationMs)}</dd>
													</div>
													<div>
														<dt>{t('log.outputRate')}</dt>
														<dd className="trace-mono">{formatCompactTokenRate(trace)}</dd>
													</div>
													<div>
														<dt>{t('log.chainTitle')}</dt>
														<dd className="trace-mono">{trace.failover ? `${trace.attemptCount} · ${t('log.failover')}` : trace.attemptCount}</dd>
													</div>
												</dl>
											</TraceInfoPopover>
										</div>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('log.tableTokens')}>
										<div className="trace-table-metric">
											<span className="trace-mono">{formatTokenCount(traceTotalTokens(trace) ?? undefined)}</span>
											<TraceInfoPopover label={t('log.tokensInfo')}>
												<dl className="trace-popover-list">
													<div>
														<dt>{t('log.totalTokens')}</dt>
														<dd className="trace-mono">{formatTokenCount(traceTotalTokens(trace) ?? undefined)}</dd>
													</div>
													<div>
														<dt>{t('log.inputTokens')}</dt>
														<dd className="trace-mono">{formatTokenCount(trace.inputTokens)}</dd>
													</div>
													<div>
														<dt>{t('log.outputTokens')}</dt>
														<dd className="trace-mono">{formatTokenCount(trace.outputTokens)}</dd>
													</div>
													<div>
														<dt>{t('log.cacheReadTokens')}</dt>
														<dd className="trace-mono">{formatUsageText(trace.usage?.cacheReadTokens)}</dd>
													</div>
													<div>
														<dt>{t('log.cacheWriteTokens')}</dt>
														<dd className="trace-mono">{formatUsageText(trace.usage?.cacheWriteTokens)}</dd>
													</div>
													<div>
														<dt>{t('log.reasoningTokens')}</dt>
														<dd className="trace-mono">{formatUsageText(trace.usage?.reasoningTokens)}</dd>
													</div>
												</dl>
											</TraceInfoPopover>
										</div>
									</div>
									<div className="trace-table-cell trace-status-cell" role="cell" data-label={t('log.tableStatus')}>
										<div className="trace-status-stack">
											<span className={`badge status-badge ${trace.success ? 'live' : 'idle'}`}>
												{trace.success ? t('log.success') : trace.statusCode || t('log.failed')}
											</span>
										</div>
									</div>
								</article>
							))}
						</div>
					</div>
	                ) : null}
              </div>
	              <div className="list-pagination">
	                <button type="button" disabled={logTraceQuery.page <= 1} onClick={() => loadLogTracePage(logTraceQuery.page - 1)}>
	                  {t('log.prevPage')}
	                </button>
	                <span className="subtle">{t('log.pageStatus', { page: logTraceQuery.page, total: logPageCount })}</span>
	                <button type="button" disabled={logTraceQuery.page >= logPageCount} onClick={() => loadLogTracePage(logTraceQuery.page + 1)}>
	                  {t('log.nextPage')}
	                </button>
	              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'network' ? (
          <section className="tab-layout trace-tab-layout">
            <article className="panel panel-fill list-column trace-panel">
              <div className="panel-header">
                <div>
                  <h3>{t('network.title')}</h3>
                  <p className="subtle">{networkTraceStatus || t('network.subtitle')}</p>
                </div>
                <div className="list-header-actions">
	                  <span className="subtle list-status-text">{networkTraceLoaded ? t('network.count', { count: networkTraceTotal }) : t('messages.loading')}</span>
                </div>
              </div>
	              <div className="list-toolbar trace-toolbar">
	                <div className="filter-group">
	                  <span className="filter-group-label">{t('network.statusCodeFilter')}</span>
	                  <div className="filter-chip-grid">
	                    {networkTraceCatalog.statusCodes.map((code) => (
	                      <button
	                        key={code}
	                        type="button"
	                        className={networkTraceQuery.statusCodes.includes(code) ? 'filter-chip active' : 'filter-chip'}
	                        onClick={() => updateNetworkTraceQuery((current) => ({ ...current, page: 1, statusCodes: toggleNumberFilter(current.statusCodes, code) }))}
	                      >
	                        {code}
	                      </button>
	                    ))}
	                  </div>
	                </div>
	              </div>
	              <div className="scroll-list compact-list trace-scroll-list">
	                {networkTraceLoaded && networkTraces.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('network.empty')}</h4>
                    <p className="subtle">{t('network.emptyHint')}</p>
                  </article>
                ) : null}
	                {networkTraces.length > 0 ? (
					<div className="trace-table trace-table-network" role="table" aria-label={t('network.title')}>
						<div className="trace-table-header" role="row">
							<span className="trace-table-head" role="columnheader">{t('network.tableTime')}</span>
							<span className="trace-table-head" role="columnheader">{t('network.tableTarget')}</span>
							<span className="trace-table-head" role="columnheader">{t('network.tableRequest')}</span>
							<span className="trace-table-head" role="columnheader">{t('network.tablePerformance')}</span>
							<span className="trace-table-head trace-table-head-end" role="columnheader">{t('network.tableStatus')}</span>
						</div>
						<div className="trace-table-body">
							{networkTraces.map((trace) => (
								<article
									key={trace.id}
									className={`trace-table-row ${networkDetailOpen && selectedNetworkTrace?.id === trace.id ? 'active' : ''}`}
									role="button"
									tabIndex={0}
									onClick={() => setSelectedNetworkTraceId(trace.id)}
									onKeyDown={(event) => onResourceCardKeyDown(event, () => setSelectedNetworkTraceId(trace.id))}
								>
									<div className="trace-table-cell" role="cell" data-label={t('network.tableTime')}>
										<span className="trace-mono">{formatCompactDateTime(trace.startedAt)}</span>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('network.tableTarget')}>
										<div className="trace-model-cell">
											<div className="trace-model-line">
												<strong className="trace-model-name">{trace.finalProvider || trace.alias || `#${trace.id}`}</strong>
												<span className={protocolBadgeClass(trace.protocol)}>{protocolLabel(trace.protocol)}</span>
											</div>
											<span className="trace-table-muted">{trace.rawModel || '-'}</span>
										</div>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('network.tableRequest')}>
										<span className="trace-table-ellipsis">{trace.finalUrl || tracePrimaryText(trace)}</span>
									</div>
									<div className="trace-table-cell" role="cell" data-label={t('network.tablePerformance')}>
										<div className="trace-table-metric">
											<span className="trace-mono">{`${formatCompactDuration(trace.firstByteMs)} / ${formatCompactDuration(trace.durationMs)}`}</span>
											<TraceInfoPopover label={t('network.performanceInfo')}>
												<dl className="trace-popover-list">
													<div>
														<dt>{t('network.firstByte')}</dt>
														<dd className="trace-mono">{formatCompactDuration(trace.firstByteMs)}</dd>
													</div>
													<div>
														<dt>{t('network.totalTime')}</dt>
														<dd className="trace-mono">{formatCompactDuration(trace.durationMs)}</dd>
													</div>
													<div>
														<dt>{t('network.chainTitle')}</dt>
														<dd className="trace-mono">{trace.attemptCount}</dd>
													</div>
													<div>
														<dt>{t('network.statusCode')}</dt>
														<dd className="trace-mono">{trace.statusCode || '-'}</dd>
													</div>
												</dl>
											</TraceInfoPopover>
										</div>
									</div>
									<div className="trace-table-cell trace-status-cell" role="cell" data-label={t('network.tableStatus')}>
										<div className="trace-status-stack">
											<span className={`badge status-badge ${trace.success ? 'live' : 'idle'}`}>{trace.statusCode || '-'}</span>
											<span className="trace-table-muted trace-mono">#{trace.id}</span>
										</div>
									</div>
								</article>
							))}
						</div>
					</div>
	                ) : null}
              </div>
	              <div className="list-pagination">
	                <button type="button" disabled={networkTraceQuery.page <= 1} onClick={() => loadNetworkTracePage(networkTraceQuery.page - 1)}>
	                  {t('network.prevPage')}
	                </button>
	                <span className="subtle">{t('network.pageStatus', { page: networkTraceQuery.page, total: networkPageCount })}</span>
	                <button type="button" disabled={networkTraceQuery.page >= networkPageCount} onClick={() => loadNetworkTracePage(networkTraceQuery.page + 1)}>
	                  {t('network.nextPage')}
	                </button>
	              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'settings' ? (
          <section className="tab-layout settings-layout">
            <article className="panel settings-main-panel">
              <div className="panel-header">
                <div>
                  <p className="subtle">{t('settings.subtitle')}</p>
                </div>
                {prefsStatus ? <p className="subtle settings-status">{prefsStatus}</p> : null}
              </div>

              <form className="stack-blocks" onSubmit={(event) => void onSavePrefs(event)}>
                <section className="subpanel">
                  <div className="subpanel-header">
                    <h4>{t('settings.appearanceTitle')}</h4>
                  </div>
                  <div className="stack">
                    <label>
                      <span>{t('settings.theme')}</span>
                      <select
                        value={prefs.theme}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, theme: event.target.value as ThemePreference }))
                        }
                      >
                        <option value="system">{t('settings.themeSystem')}</option>
                        <option value="light">{t('settings.themeLight')}</option>
                        <option value="dark">{t('settings.themeDark')}</option>
                      </select>
                    </label>
                    <div className="inline-meta">
                      <span className="meta-label">{t('settings.resolvedTheme')}</span>
                      <strong>{t(`settings.theme${resolvedTheme === 'dark' ? 'Dark' : 'Light'}`)}</strong>
                    </div>
                  </div>
                </section>

                <section className="subpanel">
                  <div className="subpanel-header">
                    <h4>{t('settings.languageTitle')}</h4>
                  </div>
                  <div className="stack">
                    <label>
                      <span>{t('settings.languageLabel')}</span>
                      <select
                        value={prefs.language}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, language: event.target.value as LanguagePreference }))
                        }
                      >
                        <option value="system">{t('settings.languageSystem')}</option>
                        <option value="en-US">{t('settings.languageEnglish')}</option>
                        <option value="zh-CN">{t('settings.languageChinese')}</option>
                      </select>
                    </label>
                    <div className="inline-meta">
                      <span className="meta-label">{t('settings.resolvedLanguage')}</span>
                      <strong>
                        {resolvedLanguage === 'zh-CN' ? t('settings.languageChinese') : t('settings.languageEnglish')}
                      </strong>
                    </div>
                  </div>
                </section>

                <section className="subpanel">
                  <div className="subpanel-header">
                    <h4>{t('settings.behaviorTitle')}</h4>
                  </div>
                  <div className="stack">
                    <label className="checkbox-row">
                      <input
                        type="checkbox"
                        checked={prefs.launchAtLogin}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, launchAtLogin: event.target.checked }))
                        }
                      />
                      <span>{t('settings.launchAtLogin')}</span>
                    </label>
                    <label className="checkbox-row">
                      <input
                        type="checkbox"
                        checked={prefs.autoStartProxy}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, autoStartProxy: event.target.checked }))
                        }
                      />
                      <span>{t('settings.autoStartProxy')}</span>
                    </label>
                    <label className="checkbox-row">
                      <input
                        type="checkbox"
                        checked={prefs.minimizeToTray}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, minimizeToTray: event.target.checked }))
                        }
                      />
                      <span>{t('settings.minimizeToTray')}</span>
                    </label>
                    <label className="checkbox-row">
                      <input
                        type="checkbox"
                        checked={prefs.notifications}
                        onChange={(event) =>
                          setPrefs((current) => ({ ...current, notifications: event.target.checked }))
                        }
                      />
                      <span>{t('settings.notifications')}</span>
                    </label>
                  </div>
                </section>

                <section className="subpanel">
                  <div className="subpanel-header">
                    <h4>{t('settings.configTitle')}</h4>
                    {configTransferStatus ? <p className="subtle settings-status">{configTransferStatus}</p> : null}
                  </div>
                  <div className="stack">
                    <div className="inline-meta config-path-meta">
                      <span className="meta-label">{t('overview.configPath')}</span>
                      <strong className="path-value">{overview?.configPath || t('app.loadingConfig')}</strong>
                    </div>
                    <div className="toolbar">
                      <button type="button" onClick={() => void onExportConfig()}>
                        {t('actions.exportConfig')}
                      </button>
                    </div>
                    <div className="mode-switch" aria-labelledby={importModeLabelId}>
                      <span id={importModeLabelId} className="sr-only">
                        {t('settings.importTitle')}
                      </span>
                      <button
                        type="button"
                        aria-pressed={configImportMode === 'text'}
                        className={configImportMode === 'text' ? 'active-toggle' : ''}
                        onClick={() => setConfigImportMode('text')}
                      >
                        {t('settings.importModeText')}
                      </button>
                      <button
                        type="button"
                        aria-pressed={configImportMode === 'file'}
                        className={configImportMode === 'file' ? 'active-toggle' : ''}
                        onClick={() => {
                          setConfigImportMode('file')
                          setConfigImportFileName('')
                        }}
                      >
                        {t('settings.importModeFile')}
                      </button>
                    </div>
                    <div className="stack">
                      {configImportMode === 'text' ? (
                        <label>
                          <span>{t('settings.importText')}</span>
                          <textarea
                            value={configImportText}
                            onChange={(event) => setConfigImportText(event.target.value)}
                            placeholder={t('settings.importPlaceholder')}
                            rows={10}
                          />
                        </label>
                      ) : (
                        <label>
                          <span>{t('settings.importFile')}</span>
                          <input type="file" accept="application/json,.json" onChange={(event) => void onSelectConfigFile(event)} />
                          <span className="subtle">{configImportFileName || t('settings.fileEmpty')}</span>
                        </label>
                      )}
                      <button type="button" className="primary" onClick={() => void onImportConfig()}>
                        {t('actions.importConfig')}
                      </button>
                    </div>
                  </div>
                </section>

                <div className="toolbar">
                  <button type="submit" className="primary">
                    {t('actions.save')}
                  </button>
                </div>
              </form>
            </article>

            <article className="panel settings-side-panel">
              <section className="subpanel">
                <div className="subpanel-header">
                  <h4>{t('settings.timeoutTitle')}</h4>
                  {proxySettingsStatus ? <p className="subtle settings-status">{proxySettingsStatus}</p> : null}
                </div>
                <form className="stack" onSubmit={(event) => {
                  event.preventDefault()
                  void onSaveProxySettings()
                }}>
                  <label>
                    <span>{t('settings.connectTimeout')}</span>
                    <input
                      type="number"
                      min={1000}
                      step={1000}
                      value={proxySettings.connectTimeoutMs}
                      onChange={(event) =>
                        setProxySettings((current) => ({ ...current, connectTimeoutMs: Number(event.target.value) || 0 }))
                      }
                    />
                  </label>
                  <label>
                    <span>{t('settings.responseHeaderTimeout')}</span>
                    <input
                      type="number"
                      min={1000}
                      step={1000}
                      value={proxySettings.responseHeaderTimeoutMs}
                      onChange={(event) =>
                        setProxySettings((current) => ({ ...current, responseHeaderTimeoutMs: Number(event.target.value) || 0 }))
                      }
                    />
                  </label>
                  <label>
                    <span>{t('settings.firstByteTimeout')}</span>
                    <input
                      type="number"
                      min={1000}
                      step={1000}
                      value={proxySettings.firstByteTimeoutMs}
                      onChange={(event) =>
                        setProxySettings((current) => ({ ...current, firstByteTimeoutMs: Number(event.target.value) || 0 }))
                      }
                    />
                  </label>
                  <label>
                    <span>{t('settings.requestReadTimeout')}</span>
                    <input
                      type="number"
                      min={1000}
                      step={1000}
                      value={proxySettings.requestReadTimeoutMs}
                      onChange={(event) =>
                        setProxySettings((current) => ({ ...current, requestReadTimeoutMs: Number(event.target.value) || 0 }))
                      }
                    />
                  </label>
                  <label>
                    <span>{t('settings.streamIdleTimeout')}</span>
                    <input
                      type="number"
                      min={1000}
                      step={1000}
                      value={proxySettings.streamIdleTimeoutMs}
                      onChange={(event) =>
                        setProxySettings((current) => ({ ...current, streamIdleTimeoutMs: Number(event.target.value) || 0 }))
                      }
                    />
                  </label>
                  <label>
                    <span>{t('settings.routingStrategy')}</span>
                    <select
                      value={proxySettings.routing.strategy}
                      onChange={(event) => {
                        const nextStrategy = event.target.value
                        const nextDescriptor = proxySettings.routing.descriptors?.find((item) => item.name === nextStrategy) || null
                        setProxySettings((current) => ({
                          ...current,
                          routing: {
                            ...current.routing,
                            strategy: nextStrategy,
                            params: nextDescriptor?.defaults || {},
                          },
                        }))
                      }}
					>
						{(proxySettings.routing.descriptors || []).map((descriptor) => (
							<option key={descriptor.name} value={descriptor.name}>{routingStrategyLabel(descriptor)}</option>
						))}
					</select>
				</label>
				{activeRouting?.description ? <p className="subtle">{routingStrategyDescription(activeRouting)}</p> : null}
				{(activeRoutingDescriptor(proxySettings)?.parameters || []).map((parameter) =>
					routingParamInput(proxySettings, proxySettings.routing.strategy, parameter, setProxySettings),
				)}
                  <p className="subtle">
                    {proxyRunning ? t('settings.timeoutRunningHint') : t('settings.timeoutHint')}
                  </p>
                  <div className="toolbar">
                    <button type="submit" className="primary">
                      {t('settings.saveTimeouts')}
                    </button>
                  </div>
                </form>
              </section>

              <div className="panel-header">
                <div>
                  <h3>{t('settings.aboutTitle')}</h3>
                  <p className="subtle">{t('settings.aboutSubtitle')}</p>
                </div>
              </div>
              <dl className="info-grid info-grid-single settings-about-grid">
                <div>
                  <dt>{t('overview.version')}</dt>
                  <dd>{meta.version || t('app.dev')}</dd>
                </div>
                <div>
                  <dt>{t('overview.configPath')}</dt>
                  <dd className="path-value">{overview?.configPath || t('app.loadingConfig')}</dd>
                </div>
                <div>
                  <dt>{t('settings.resolvedTheme')}</dt>
                  <dd>{t(`settings.theme${resolvedTheme === 'dark' ? 'Dark' : 'Light'}`)}</dd>
                </div>
                <div>
                  <dt>{t('settings.resolvedLanguage')}</dt>
                  <dd>{resolvedLanguage === 'zh-CN' ? t('settings.languageChinese') : t('settings.languageEnglish')}</dd>
                </div>
              </dl>
            </article>
          </section>
        ) : null}

        {activeModal === 'provider-import' ? (
          <div className="modal-backdrop" onClick={closeModal}>
            <div
              className="modal-card"
              role="dialog"
              aria-modal="true"
              aria-labelledby={providerImportTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onModalKeyDown}
            >
              <div className="subpanel-header">
                <h4 id={providerImportTitleId}>{t('providers.importTitle')}</h4>
                <button type="button" onClick={closeModal}>
                  {t('actions.close')}
                </button>
              </div>
              <form className="stack" onSubmit={(event) => void onImportProviders(event)}>
                <p className="subtle">{t('providers.importHint')}</p>
                <label>
                  <span>{t('providers.sourcePath')}</span>
                  <input
                    type="text"
                    value={providerImportForm.sourcePath || ''}
                    onChange={(event) => setProviderImportForm((current) => ({ ...current, sourcePath: event.target.value }))}
                    placeholder={t('providers.placeholderSourcePath')}
                  />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={providerImportForm.overwrite}
                    onChange={(event) => setProviderImportForm((current) => ({ ...current, overwrite: event.target.checked }))}
                  />
                  <span>{t('providers.overwrite')}</span>
                </label>
                <button type="submit" className="primary">{t('actions.import')}</button>
              </form>
            </div>
          </div>
        ) : null}

        {providerDetailOpen ? (
          <div className="detail-backdrop" onClick={closeProviderDetail}>
            <div
              ref={providerDetailRef}
              className="detail-sheet"
              role="dialog"
              aria-modal="true"
              aria-labelledby={providerDetailTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onProviderDetailKeyDown}
            >
              <div className="panel-header detail-sheet-header">
                <div>
                  <h3 id={providerDetailTitleId}>
                    {providerDetailMode === 'create'
                      ? t('providers.formCreateTitle')
                      : editingProviderId
                        ? t('providers.formEditTitle', { id: editingProviderId })
                        : t('providers.listTitle')}
                  </h3>
                  <p className="subtle">{providerStatus || t('providers.detailHint')}</p>
                </div>
                <div className="toolbar">
                  {providerDetailMode === 'edit' && selectedProvider ? (
	                    <button type="button" onClick={() => void onRefreshProviderModels({ id: selectedProvider.id })}>
	                      {t('providers.refreshModels')}
	                    </button>
	                  ) : null}
	                  {providerDetailMode === 'edit' && selectedProvider ? (
                    <button type="button" onClick={() => void onToggleProvider(selectedProvider)}>
                      {selectedProvider.disabled ? t('actions.enable') : t('actions.disable')}
                    </button>
                  ) : null}
                  {providerDetailMode === 'edit' && selectedProvider ? (
                    <button
                      type="button"
                      className="danger ghost-danger"
                      onClick={() => setConfirmIntent({ kind: 'delete-provider', id: selectedProvider.id })}
                    >
                      {t('actions.delete')}
                    </button>
                  ) : null}
                  <button type="button" onClick={closeProviderDetail}>
                    {t('actions.close')}
                  </button>
                </div>
              </div>

              <form className="stack-blocks" onSubmit={(event) => void onSaveProvider(event)}>
                <section className="detail-hero detail-hero-grid">
                  <div className="detail-hero-card detail-hero-primary">
                    <span className="meta-label">{t('providers.name')}</span>
                    <strong>{providerForm.name || providerForm.id || t('providers.formCreateTitle')}</strong>
                    <p className="subtle detail-hero-subtle detail-hero-prewrap">{providerBaseUrlSummary(selectedProvider, providerForm, t)}</p>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('status.status')}</span>
                    <strong>{providerForm.disabled ? t('status.disabled') : t('status.enabled')}</strong>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('common.protocol')}</span>
                    <strong><span className={protocolBadgeClass(providerForm.protocol)}>{protocolLabel(providerForm.protocol)}</span></strong>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('providers.models')}</span>
                    <strong>{t('providers.modelsCount', { count: selectedProvider?.models?.length || 0 })}</strong>
                  </div>
                </section>

                <section className="detail-section">
                  <div className="detail-section-header">
                    <div>
                      <h4>{t('providers.detailBasicsTitle')}</h4>
                      <p className="subtle">{t('providers.detailHint')}</p>
                    </div>
                  </div>
                  <div className="detail-form-grid">
                    <label>
                      <span>{t('providers.id')}</span>
                      <input
                        type="text"
                        value={providerForm.id}
                        onChange={(event) => setProviderForm((current) => ({ ...current, id: event.target.value }))}
                        placeholder={t('providers.placeholderId')}
                      />
                    </label>
                    <label>
                      <span>{t('providers.name')}</span>
                      <input
                        type="text"
                        value={providerForm.name}
                        onChange={(event) => setProviderForm((current) => ({ ...current, name: event.target.value }))}
                        placeholder={t('providers.placeholderName')}
                      />
                    </label>
                    <fieldset className="detail-form-span">
                      <legend>{t('common.protocol')}</legend>
                      <div className="toggle-grid">
                        {protocolOptions.map((protocol) => (
                          <label className="checkbox-row checkbox-card" key={protocol}>
                            <input
                              type="radio"
                              name="provider-protocol"
                              checked={providerForm.protocol === protocol}
                              onChange={() => setProviderForm((current) => ({ ...current, protocol }))}
                            />
                            <span>{protocolLabel(protocol)}</span>
                          </label>
                        ))}
                      </div>
                    </fieldset>
                    <fieldset className="detail-form-span provider-baseurl-fieldset">
                      <legend>{t('providers.baseUrls')}</legend>
                      <div className="provider-baseurl-stack">
                        <div className="toggle-grid provider-strategy-grid">
                          <label className="checkbox-row checkbox-card">
                            <input
                              type="radio"
                              name="provider-baseurl-strategy"
                              checked={providerForm.baseUrlStrategy === 'ordered'}
                              onChange={() => setProviderForm((current) => ({ ...current, baseUrlStrategy: 'ordered' }))}
                            />
                            <span>{t('providers.baseUrlStrategyOrdered')}</span>
                          </label>
                          <label className="checkbox-row checkbox-card">
                            <input
                              type="radio"
                              name="provider-baseurl-strategy"
                              checked={providerForm.baseUrlStrategy === 'latency'}
                              onChange={() => setProviderForm((current) => ({ ...current, baseUrlStrategy: 'latency' }))}
                            />
                            <span>{t('providers.baseUrlStrategyLatency')}</span>
                          </label>
                        </div>
                        <p className="subtle">{t(providerForm.baseUrlStrategy === 'latency' ? 'providers.baseUrlStrategyLatencyHint' : 'providers.baseUrlStrategyOrderedHint')}</p>
                        {providerForm.baseUrls.map((baseUrl, index) => {
                          const normalizedBaseUrl = baseUrl.trim()
                          const pingResult = normalizedBaseUrl ? providerBaseUrlPings[normalizedBaseUrl] : undefined
                          return (
                            <div
                              className={`provider-baseurl-row ${draggingProviderBaseUrlIndex === index ? 'dragging' : ''}`}
                              key={index}
                              draggable={providerForm.baseUrls.length > 1}
                              onDragStart={(event) => onProviderBaseUrlDragStart(event, index)}
                              onDragOver={(event) => event.preventDefault()}
                              onDrop={(event) => onProviderBaseUrlDrop(event, index)}
                              onDragEnd={() => setDraggingProviderBaseUrlIndex(null)}
                            >
                              <span className="provider-baseurl-index">#{index + 1}</span>
                              <input
                                type="text"
                                value={baseUrl}
                                onChange={(event) => updateProviderBaseUrl(index, event.target.value)}
                                placeholder={t('providers.placeholderBaseUrl')}
                              />
                              <button type="button" onClick={() => moveProviderBaseUrl(index, -1)} disabled={index === 0} aria-label={t('providers.moveUp')}>
                                {t('providers.moveUpShort')}
                              </button>
                              <button
                                type="button"
                                onClick={() => moveProviderBaseUrl(index, 1)}
                                disabled={index === providerForm.baseUrls.length - 1}
                                aria-label={t('providers.moveDown')}
                              >
                                {t('providers.moveDownShort')}
                              </button>
                              <button
                                type="button"
                                onClick={() => void onPingProviderBaseUrl({
                                  id: providerForm.id.trim() || undefined,
                                  protocol: providerForm.protocol,
                                  baseUrl: normalizedBaseUrl,
                                  apiKey: providerForm.apiKey,
                                  headers: parseHeadersText(providerForm.headersText),
                                })}
                                disabled={!normalizedBaseUrl}
                              >
                                {t('providers.ping')}
                              </button>
                              <button
                                type="button"
                                className="danger ghost-danger"
                                onClick={() => removeProviderBaseUrl(index)}
                                disabled={providerForm.baseUrls.length === 1}
                              >
                                {t('actions.delete')}
                              </button>
                              <span className={`provider-baseurl-ping ${pingResult?.reachable ? 'ok' : pingResult?.error ? 'bad' : ''}`}>
                                {pingResult
                                  ? pingResult.reachable
                                    ? t('providers.pingLatency', { latency: pingResult.latencyMs })
                                    : (pingResult.error || t('providers.pingUnreachable'))
                                  : t('providers.pingIdle')}
                              </span>
                            </div>
                          )
                        })}
                        <div className="toolbar">
                          <button type="button" onClick={addProviderBaseUrl}>{t('providers.addBaseUrl')}</button>
                        </div>
                      </div>
                    </fieldset>
                    <label className="detail-form-span">
                      <span>{t('providers.apiKey')}</span>
                      <input
                        type="text"
                        value={providerForm.apiKey}
                        onChange={(event) => setProviderForm((current) => ({ ...current, apiKey: event.target.value }))}
                        placeholder={t('providers.placeholderApiKey')}
                      />
                    </label>
                    <label className="detail-form-span">
                      <span>{t('providers.headers')}</span>
                      <textarea
                        value={providerForm.headersText}
                        onChange={(event) => setProviderForm((current) => ({ ...current, headersText: event.target.value }))}
                        placeholder={t('providers.placeholderHeaders')}
                        rows={4}
                      />
                    </label>
                  </div>
                </section>

                <section className="detail-section">
                  <div className="detail-section-header">
                    <div>
                      <h4>{t('providers.detailTogglesTitle')}</h4>
                      <p className="subtle">{selectedProvider?.models?.join(', ') || t('providers.modelsNone')}</p>
                    </div>
                  </div>
                  <div className="toggle-grid">
                    <label className="checkbox-row checkbox-card">
                      <input
                        type="checkbox"
                        checked={providerForm.disabled}
                        onChange={(event) => setProviderForm((current) => ({ ...current, disabled: event.target.checked }))}
                      />
                      <span>{t('providers.saveDisabled')}</span>
                    </label>
                    <label className="checkbox-row checkbox-card">
                      <input
                        type="checkbox"
                        checked={providerForm.skipModels}
                        onChange={(event) => setProviderForm((current) => ({ ...current, skipModels: event.target.checked }))}
                      />
                      <span>{t('providers.skipModels')}</span>
                    </label>
                    <label className="checkbox-row checkbox-card detail-form-span">
                      <input
                        type="checkbox"
                        checked={providerForm.clearHeaders}
                        onChange={(event) => setProviderForm((current) => ({ ...current, clearHeaders: event.target.checked }))}
                      />
                      <span>{t('providers.clearHeaders')}</span>
                    </label>
                  </div>
                </section>

                <div className="toolbar detail-actions">
                  <button type="submit" className="primary">
                    {t('actions.save')}
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      if (selectedProvider) {
                        selectProviderDetail(selectedProvider)
                        return
                      }
                      resetProviderForm()
                    }}
                    >
                      {t('actions.reset')}
                    </button>
                </div>
              </form>
            </div>
          </div>
        ) : null}

        {activeModal === 'alias-target' ? (
          <div className="modal-backdrop" onClick={closeModal}>
            <div
              className="modal-card"
              role="dialog"
              aria-modal="true"
              aria-labelledby={aliasTargetTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onModalKeyDown}
            >
              <div className="subpanel-header">
                <h4 id={aliasTargetTitleId}>{t('aliases.bindTitle')}</h4>
                <button type="button" onClick={closeModal}>
                  {t('actions.close')}
                </button>
              </div>
              <form className="stack" onSubmit={(event) => void onBindTarget(event)}>
                <label>
                  <span>{t('aliases.aliasForBinding')}</span>
                  <input
                    type="text"
                    value={targetForm.alias}
                    onChange={onTargetAliasChange}
                    placeholder={t('aliases.placeholderAliasBinding')}
                  />
                </label>
                    <label>
                      <span>{t('aliases.providerId')}</span>
                  <div className="inline-pills bind-modal-pills">
                    <span className={protocolBadgeClass(targetProtocol)}>{protocolLabel(targetProtocol)}</span>
                    <span className="pill">{t('aliases.bindableProviders', { count: bindableProviders.length })}</span>
                  </div>
                    <select
                      value={targetForm.provider}
	                    onChange={(event) => onTargetProviderChange(event.target.value)}
                    >
                    <option value="">{t('aliases.placeholderProviderSelect')}</option>
                    {bindableProviders.map((provider) => (
                      <option key={provider.id} value={provider.id}>
                        {provider.id} · {protocolLabel(provider.protocol)}
                      </option>
                    ))}
                  </select>
                  <span className="subtle">
                    {bindableProviders.length > 0
                      ? `${t('common.protocol')}: ${protocolLabel(targetProtocol)}`
                      : t('aliases.noProvidersForProtocol', { protocol: protocolLabel(targetProtocol) })}
                  </span>
                </label>
                <label>
                  <span>{t('aliases.model')}</span>
	                  {bindableModels.length > 0 ? (
	                    <select value={targetForm.model} onChange={(event) => setTargetForm((current) => ({ ...current, model: event.target.value }))}>
	                      <option value="">{t('aliases.placeholderModelSelect')}</option>
	                      {bindableModels.map((model) => (
	                        <option key={model} value={model}>
	                          {model}
	                        </option>
	                      ))}
	                    </select>
	                  ) : (
	                    <input
	                      type="text"
	                      value={targetForm.model}
	                      onChange={(event) => setTargetForm((current) => ({ ...current, model: event.target.value }))}
	                      placeholder={t('aliases.placeholderModel')}
	                    />
	                  )}
	                  <span className="subtle">
	                    {bindableModels.length > 0
	                      ? t('aliases.providerModelsLoaded', { count: bindableModels.length })
	                      : t('aliases.providerModelsEmpty')}
	                  </span>
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={targetForm.disabled}
                    onChange={(event) => setTargetForm((current) => ({ ...current, disabled: event.target.checked }))}
                  />
                  <span>{t('aliases.targetDisabled')}</span>
                </label>
                <div className="toolbar">
                  <button type="submit" className="primary">{t('actions.bind')}</button>
                  <button
                    type="button"
                    onClick={() => setTargetForm((current) => ({
                      ...emptyTargetForm,
                      alias: current.alias,
                      provider: bindableProviders[0]?.id || '',
                    }))}
                  >
                    {t('actions.reset')}
                  </button>
                </div>
              </form>
            </div>
          </div>
        ) : null}

        {aliasDetailOpen ? (
          <div className="detail-backdrop" onClick={closeAliasDetail}>
            <div
              ref={aliasDetailRef}
              className="detail-sheet"
              role="dialog"
              aria-modal="true"
              aria-labelledby={aliasDetailTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onAliasDetailKeyDown}
            >
              <div className="panel-header detail-sheet-header">
                <div>
                  <h3 id={aliasDetailTitleId}>
                    {aliasDetailMode === 'create'
                      ? t('aliases.formCreateTitle')
                      : editingAliasId
                        ? t('aliases.formEditTitle', { alias: editingAliasId })
                        : t('aliases.listTitle')}
                  </h3>
                  <p className="subtle">{aliasStatus || t('aliases.detailHint')}</p>
                </div>
                <div className="toolbar">
                  <button type="button" onClick={() => openAliasTargetModal(aliasForm.alias || selectedAlias?.alias)}>
                    {t('actions.bind')}
                  </button>
                  {selectedAlias ? (
                    <button
                      type="button"
                      className="danger ghost-danger"
                      onClick={() => setConfirmIntent({ kind: 'delete-alias', alias: selectedAlias.alias })}
                    >
                      {t('actions.delete')}
                    </button>
                  ) : null}
                  <button type="button" onClick={closeAliasDetail}>
                    {t('actions.close')}
                  </button>
                </div>
              </div>

              <div className="stack-blocks">
                <section className="detail-hero detail-hero-grid">
                  <div className="detail-hero-card detail-hero-primary">
                    <span className="meta-label">{t('aliases.alias')}</span>
                    <strong>{aliasForm.displayName || aliasForm.alias || t('aliases.formCreateTitle')}</strong>
                    <p className="subtle detail-hero-subtle">{aliasForm.alias || '-'}</p>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('status.status')}</span>
                    <strong>{aliasForm.disabled ? t('status.disabled') : t('status.enabled')}</strong>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('common.protocol')}</span>
                    <strong><span className={protocolBadgeClass(aliasForm.protocol)}>{protocolLabel(aliasForm.protocol)}</span></strong>
                  </div>
                  <div className="detail-hero-card">
                    <span className="meta-label">{t('aliases.targets')}</span>
                    <strong>{t('aliases.targetsCount', { count: selectedAlias?.targets.length || 0 })}</strong>
                  </div>
                </section>

                <form className="stack-blocks" onSubmit={(event) => void onSaveAlias(event)}>
                  <section className="detail-section">
                    <div className="detail-section-header">
                      <div>
                        <h4>{t('aliases.detailBasicsTitle')}</h4>
                        <p className="subtle">{t('aliases.detailHint')}</p>
                      </div>
                    </div>
                    <div className="detail-form-grid">
                      <label>
                        <span>{t('aliases.alias')}</span>
                        <input
                          type="text"
                          value={aliasForm.alias}
                          onChange={(event) => setAliasForm((current) => ({ ...current, alias: event.target.value }))}
                          placeholder={t('aliases.placeholderAlias')}
                        />
                      </label>
                      <label>
                        <span>{t('aliases.displayName')}</span>
                        <input
                          type="text"
                          value={aliasForm.displayName}
                          onChange={(event) => setAliasForm((current) => ({ ...current, displayName: event.target.value }))}
                          placeholder={t('aliases.placeholderDisplayName')}
                        />
                      </label>
                      <fieldset className="detail-form-span">
                        <legend>{t('common.protocol')}</legend>
                        <div className="toggle-grid">
                          {protocolOptions.map((protocol) => (
                            <label className="checkbox-row checkbox-card" key={protocol}>
                              <input
                                type="radio"
                                name="alias-protocol"
                                checked={aliasForm.protocol === protocol}
                                onChange={() => setAliasForm((current) => ({ ...current, protocol }))}
                              />
                              <span>{protocolLabel(protocol)}</span>
                            </label>
                          ))}
                        </div>
                      </fieldset>
                    </div>
                    <div className="toggle-grid">
                      <label className="checkbox-row checkbox-card detail-form-span">
                        <input
                          type="checkbox"
                          checked={aliasForm.disabled}
                          onChange={(event) => setAliasForm((current) => ({ ...current, disabled: event.target.checked }))}
                        />
                        <span>{t('aliases.createDisabled')}</span>
                      </label>
                    </div>
                  </section>

                  <div className="toolbar detail-actions">
                    <button type="submit" className="primary">
                      {t('actions.save')}
                    </button>
                    <button
                      type="button"
                      onClick={() => {
                        if (selectedAlias) {
                          selectAliasDetail(selectedAlias)
                          return
                        }
                        resetAliasForm()
                      }}
                    >
                      {t('actions.reset')}
                    </button>
                  </div>
                </form>

                <section className="detail-section detail-targets-section">
                  <div className="detail-section-header">
                    <div>
                      <h4>{t('aliases.targets')}</h4>
                      <p className="subtle">{t('aliases.detailTargetsHint')}</p>
                    </div>
                    {selectedAlias ? <span className="subtle">{t('aliases.targetsCount', { count: selectedAlias.targets.length })}</span> : null}
                  </div>
                  <div className="target-list target-list-compact">
                    {!selectedAlias || selectedAlias.targets.length === 0 ? (
                      <article className="empty-card compact-empty detail-empty-card">
                        <div className="empty-illustration" aria-hidden="true">◎</div>
                        <h4>{t('aliases.noTargets')}</h4>
                        <p className="subtle">{t('aliases.emptyHint')}</p>
                      </article>
                    ) : null}
                    {selectedAlias?.targets.map((target, index) => (
                      <div
                        className={`target-card ${draggingAliasTargetIndex === index ? 'dragging' : ''}`}
                        key={`${selectedAlias.alias}-${target.provider}-${target.model}`}
                        draggable={selectedAlias.targets.length > 1}
                        onDragStart={(event) => onAliasTargetDragStart(event, index)}
                        onDragOver={(event) => event.preventDefault()}
                        onDrop={(event) => onAliasTargetDrop(event, index)}
                        onDragEnd={() => setDraggingAliasTargetIndex(null)}
                      >
                        <div className="target-card-main">
                          <span className="target-card-index">#{index + 1}</span>
                          <div className="target-card-copy">
                            <code>
                              {target.provider}/{target.model}
                            </code>
                            <span className="subtle target-card-subtitle">{selectedAlias.alias}</span>
                          </div>
                          <span className={`badge status-badge ${target.enabled ? 'live' : 'idle'}`}>
                            {target.enabled ? t('status.enabled') : t('status.disabled')}
                          </span>
                        </div>
                        <div className="toolbar toolbar-end">
                          <button type="button" onClick={() => void moveAliasTarget(index, -1)} disabled={index === 0} aria-label={t('aliases.moveTargetUp')}>
                            {t('aliases.moveTargetUpShort')}
                          </button>
                          <button
                            type="button"
                            onClick={() => void moveAliasTarget(index, 1)}
                            disabled={index === selectedAlias.targets.length - 1}
                            aria-label={t('aliases.moveTargetDown')}
                          >
                            {t('aliases.moveTargetDownShort')}
                          </button>
                          <button
                            type="button"
                            onClick={() =>
                              void onToggleTarget(selectedAlias.alias, target.provider, target.model, target.enabled)
                            }
                          >
                            {target.enabled ? t('actions.disable') : t('actions.enable')}
                          </button>
                          <button
                            type="button"
                            className="danger ghost-danger"
                            onClick={() =>
                              setConfirmIntent({
                                kind: 'unbind-target',
                                alias: selectedAlias.alias,
                                provider: target.provider,
                                model: target.model,
                              })
                            }
                          >
                            {t('actions.unbind')}
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </section>
              </div>
            </div>
          </div>
        ) : null}

        {logDetailOpen && selectedLogTrace ? (
          <div className="detail-backdrop" onClick={closeLogDetail}>
            <div
              ref={logDetailRef}
              className="detail-sheet"
              role="dialog"
              aria-modal="true"
              aria-labelledby={logDetailTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onLogDetailKeyDown}
            >
              <div className="panel-header detail-sheet-header">
                <div>
                  <h3 id={logDetailTitleId}>{t('log.detailTitle')}</h3>
                  <p className="subtle">{t('log.detailSubtitle')}</p>
                </div>
                <div className="toolbar">
                  <button type="button" onClick={closeLogDetail}>
                    {t('actions.close')}
                  </button>
                </div>
              </div>

              <div className="stack-blocks">
                <div className="trace-hero">
                  <div>
                    <span className="meta-label">{t('log.alias')}</span>
                    <strong>{selectedLogTrace.alias || selectedLogTrace.rawModel || '-'}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('log.finalRoute')}</span>
                    <strong>{tracePrimaryText(selectedLogTrace)}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('common.protocol')}</span>
                    <strong><span className={protocolBadgeClass(selectedLogTrace.protocol)}>{protocolLabel(selectedLogTrace.protocol)}</span></strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('log.status')}</span>
                    <strong>{selectedLogTrace.statusCode || '-'}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('log.totalTime')}</span>
                    <strong>{formatDuration(selectedLogTrace.durationMs)}</strong>
                  </div>
                </div>

                <div className="info-grid">
                  <div>
                    <dt>{t('log.startedAt')}</dt>
                    <dd>{formatDateTime(selectedLogTrace.startedAt)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.firstByte')}</dt>
                    <dd>{formatDuration(selectedLogTrace.firstByteMs)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.stream')}</dt>
                    <dd>{selectedLogTrace.stream ? t('status.yes') : t('status.no')}</dd>
                  </div>
                  <div>
                    <dt>{t('log.failover')}</dt>
                    <dd>{selectedLogTrace.failover ? t('status.yes') : t('status.no')}</dd>
                  </div>
                  <div>
                    <dt>{t('log.inputTokens')}</dt>
                    <dd>{formatTokenCount(selectedLogTrace.inputTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.outputTokens')}</dt>
                    <dd>{formatTokenCount(selectedLogTrace.outputTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.outputRate')}</dt>
                    <dd>{formatTokenRate(selectedLogTrace)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.reasoningTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.reasoningTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.cacheReadTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.cacheReadTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.cacheWriteTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.cacheWriteTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.cacheWrite1hTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.cacheWrite1hTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.rawInputTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.rawInputTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.rawOutputTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.rawOutputTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.rawTotalTokens')}</dt>
                    <dd>{formatUsageText(selectedLogTrace.usage?.rawTotalTokens)}</dd>
                  </div>
                  <div>
                    <dt>{t('log.usageSource')}</dt>
                    <dd>
                      {selectedLogTrace.usage?.source ? (
                        <span className={usageSourceBadgeClass(selectedLogTrace.usage.source)}>{usageSourceLabel(selectedLogTrace.usage.source)}</span>
                      ) : '-'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t('log.usagePrecision')}</dt>
                    <dd>
                      {selectedLogTrace.usage?.precision ? (
                        <span className={usagePrecisionBadgeClass(selectedLogTrace.usage.precision)}>{usagePrecisionLabel(selectedLogTrace.usage.precision)}</span>
                      ) : '-'}
                    </dd>
                  </div>
                  <div>
                    <dt>{t('log.usageNotes')}</dt>
                    <dd>{selectedLogTrace.usage?.notes?.length ? selectedLogTrace.usage.notes.join(', ') : '-'}</dd>
                  </div>
                </div>

                {selectedLogTrace.error ? <p className="tone-error">{selectedLogTrace.error}</p> : null}

                <div className="stack">
                  <div className="panel-header compact-header">
                    <h4>{t('log.chainTitle')}</h4>
                  </div>
                  <div className="chain-list">
                    {(selectedLogTrace.attempts || []).map((attempt) => (
                      <article className="chain-card" key={`${selectedLogTrace.id}-${attempt.attempt}-${attempt.provider}-${attempt.model}`}>
                        <div className="item-heading">
                          <strong>{t('log.attemptLabel', { attempt: attempt.attempt })}</strong>
                          <span className={`badge ${attempt.success ? 'live' : attempt.skipped ? 'outline' : 'idle'}`}>
                            {attempt.result || '-'}
                          </span>
                        </div>
                        <div className="item-grid">
                          <div>
                            <span className="meta-label">{t('log.provider')}</span>
                            <span>{attempt.provider || '-'}</span>
                          </div>
                          <div>
                            <span className="meta-label">{t('log.model')}</span>
                            <span>{attempt.model || '-'}</span>
                          </div>
                          <div>
                            <span className="meta-label">{t('log.status')}</span>
                            <span>{attempt.statusCode || '-'}</span>
                          </div>
                          <div>
                            <span className="meta-label">{t('log.totalTime')}</span>
                            <span>{formatDuration(attempt.durationMs)}</span>
                          </div>
                        </div>
                        {attempt.error ? <p className="subtle tone-error">{attempt.error}</p> : null}
                      </article>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          </div>
        ) : null}

        {networkDetailOpen && selectedNetworkTrace ? (
          <div className="detail-backdrop" onClick={closeNetworkDetail}>
            <div
              ref={networkDetailRef}
              className="detail-sheet"
              role="dialog"
              aria-modal="true"
              aria-labelledby={networkDetailTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onNetworkDetailKeyDown}
            >
              <div className="panel-header detail-sheet-header">
                <div>
                  <h3 id={networkDetailTitleId}>{t('network.detailTitle')}</h3>
                  <p className="subtle">{t('network.detailSubtitle')}</p>
                </div>
                <div className="toolbar">
                  <button type="button" onClick={closeNetworkDetail}>
                    {t('actions.close')}
                  </button>
                </div>
              </div>

              <div className="stack-blocks">
                <div className="trace-hero">
                  <div>
                    <span className="meta-label">{t('network.url')}</span>
                    <strong>{selectedNetworkTrace.finalUrl || '-'}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('common.protocol')}</span>
                    <strong><span className={protocolBadgeClass(selectedNetworkTrace.protocol)}>{protocolLabel(selectedNetworkTrace.protocol)}</span></strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('network.firstByte')}</span>
                    <strong>{formatDuration(selectedNetworkTrace.firstByteMs)}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('network.totalTime')}</span>
                    <strong>{formatDuration(selectedNetworkTrace.durationMs)}</strong>
                  </div>
                  <div>
                    <span className="meta-label">{t('network.statusCode')}</span>
                    <strong>{selectedNetworkTrace.statusCode || '-'}</strong>
                  </div>
                </div>

                <details className="details-toggle" open>
                  <summary>{t('network.requestHeaders')}</summary>
                  <pre className="details">{pretty(selectedNetworkTrace.requestHeaders || {})}</pre>
                </details>

                <details className="details-toggle" open>
                  <summary>{t('network.requestParams')}</summary>
                  <pre className="details">{pretty(selectedNetworkTrace.requestParams || {})}</pre>
                </details>

                <div className="stack">
                  {(selectedNetworkTrace.attempts || []).map((attempt) => (
                    <details className="details-toggle" key={`${selectedNetworkTrace.id}-net-${attempt.attempt}`}>
                      <summary>
                        {t('network.attemptTitle', {
                          attempt: attempt.attempt,
                          provider: attempt.provider || '-',
                          model: attempt.model || '-',
                        })}
                      </summary>
                      <div className="stack-blocks trace-detail-block">
                        <div className="info-grid">
                          <div>
                            <dt>{t('network.url')}</dt>
                            <dd>{attempt.url || '-'}</dd>
                          </div>
                          <div>
                            <dt>{t('network.statusCode')}</dt>
                            <dd>{attempt.statusCode || '-'}</dd>
                          </div>
                          <div>
                            <dt>{t('network.firstByte')}</dt>
                            <dd>{formatDuration(attempt.firstByteMs)}</dd>
                          </div>
                          <div>
                            <dt>{t('network.totalTime')}</dt>
                            <dd>{formatDuration(attempt.durationMs)}</dd>
                          </div>
                        </div>
                        <details className="details-toggle" open>
                          <summary>{t('network.requestHeaders')}</summary>
                          <pre className="details">{pretty(attempt.requestHeaders || {})}</pre>
                        </details>
                        <details className="details-toggle" open>
                          <summary>{t('network.requestParams')}</summary>
                          <pre className="details">{pretty(attempt.requestParams || {})}</pre>
                        </details>
                        <details className="details-toggle" open>
                          <summary>{t('network.responseHeaders')}</summary>
                          <pre className="details">{pretty(attempt.responseHeaders || {})}</pre>
                        </details>
                        {attempt.responseBody ? (
                          <details className="details-toggle" open>
                            <summary>{t('network.responseBody')}</summary>
                            <pre className="details">{attempt.responseBody}</pre>
                          </details>
                        ) : null}
                      </div>
                    </details>
                  ))}
                </div>
              </div>
            </div>
          </div>
        ) : null}

        {confirmIntent ? (
          <div className="modal-backdrop" onClick={closeConfirmDialog}>
            <div
              className="modal-card modal-card-confirm"
              role="alertdialog"
              aria-modal="true"
              aria-labelledby={confirmTitleId}
              tabIndex={-1}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={onConfirmKeyDown}
            >
              <div className="confirm-icon">!</div>
              <div className="stack">
                <div>
                  <h4 id={confirmTitleId}>{confirmTitle}</h4>
                  <p className="subtle">{confirmMessage}</p>
                </div>
                <div className="toolbar toolbar-end">
                  <button type="button" onClick={closeConfirmDialog}>
                    {t('actions.cancel')}
                  </button>
                  <button type="button" className="danger" onClick={() => void onConfirmAction()}>
                    {t('actions.confirm')}
                  </button>
                </div>
              </div>
            </div>
          </div>
        ) : null}
      </main>
    </div>
  )
}
