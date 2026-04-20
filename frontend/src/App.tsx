import { ChangeEvent, FormEvent, KeyboardEvent, useCallback, useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  applySync,
  bindAliasTarget,
  deleteAlias,
  deleteProvider,
  exportConfig,
  getMeta,
  getOverview,
  getProxySettings,
  importConfig,
  importProviders,
  listRequestTraces,
  listAliases,
  listProviders,
  previewSync,
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
  AliasView,
  AliasUpsertInput,
  DesktopPrefsSaveResult,
  DesktopPrefsView,
  DoctorRunResult,
  LanguagePreference,
  Overview,
  ProviderImportInput,
  ProviderImportResult,
  ProviderSaveResult,
  ProviderProtocol,
  ProviderUpsertInput,
  ProviderView,
  ProxySettingsSaveResult,
  ProxySettingsView,
  RequestTrace,
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
  baseUrl: string
  apiKey: string
  headersText: string
  disabled: boolean
  skipModels: boolean
  clearHeaders: boolean
}

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
type ConfirmIntent =
  | { kind: 'delete-provider'; id: string }
  | { kind: 'delete-alias'; alias: string }
  | { kind: 'unbind-target'; alias: string; provider: string; model: string }

const tabs: TabKey[] = ['overview', 'providers', 'aliases', 'log', 'network', 'sync', 'settings']
const GITHUB_REPOSITORY_URL = 'https://github.com/Apale7/opencode-provider-switch'
const protocolOptions: ProviderProtocol[] = ['openai-responses']

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
  baseUrl: '',
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
	return protocol === 'openai-responses' ? 'OpenAI Responses' : protocol
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

function pretty(value: unknown): string {
  return JSON.stringify(value, null, 2)
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
    baseUrl: provider.baseUrl,
    apiKey: '',
    headersText: headersTextFromMap(provider.headers),
    disabled: provider.disabled,
    skipModels: false,
    clearHeaders: false,
  }
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

function formatDuration(value?: number): string {
  if (value == null) {
    return '-'
  }
  return `${value} ms`
}

function formatTokenCount(value?: number): string {
  if (value == null || value <= 0) {
    return '-'
  }
  return value.toLocaleString()
}

function formatTokenRate(trace: RequestTrace): string {
  if (!trace.outputTokens || trace.outputTokens <= 0 || !trace.durationMs || trace.durationMs <= 0) {
    return '-'
  }
  return `${((trace.outputTokens * 1000) / trace.durationMs).toFixed(1)} token/s`
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
  const haystack = [provider.id, provider.name || '', provider.baseUrl, provider.models?.join(' ') || '']
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
  const [providerImportForm, setProviderImportForm] = useState<ProviderImportInput>(emptyProviderImport)
  const [aliasStatus, setAliasStatus] = useState('')
  const [aliasForm, setAliasForm] = useState<AliasFormState>(emptyAliasForm)
  const [targetForm, setTargetForm] = useState<AliasTargetInput>(emptyTargetForm)
  const [requestTraces, setRequestTraces] = useState<RequestTrace[]>([])
  const [traceStatus, setTraceStatus] = useState('')
  const [selectedLogTraceId, setSelectedLogTraceId] = useState<number | null>(null)
  const [selectedNetworkTraceId, setSelectedNetworkTraceId] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
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
  const providerDetailRef = useRef<HTMLDivElement | null>(null)
  const aliasDetailRef = useRef<HTMLDivElement | null>(null)
  const logDetailRef = useRef<HTMLDivElement | null>(null)
  const networkDetailRef = useRef<HTMLDivElement | null>(null)

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
      const traces = await listRequestTraces()
      setRequestTraces(traces)
      setSelectedLogTraceId((current) => resolveSelectedTraceId(current, traces))
      setSelectedNetworkTraceId((current) => resolveSelectedTraceId(current, traces))
      setPrefsStatus(i18n.t('messages.fresh'))
      setProxySettingsStatus(i18n.t('messages.fresh'))
      setTraceStatus(i18n.t('messages.fresh'))
    } catch (error) {
      setPrefsStatus(formatError(error))
      setProxySettingsStatus(formatError(error))
      setTraceStatus(formatError(error))
    } finally {
      setLoading(false)
    }
  }, [])

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
  const targetProtocol = resolveAliasProtocol(targetAlias || selectedAlias)
  const bindableProviders = providers
    .filter((provider) => provider.protocol === targetProtocol)
    .sort((left, right) => left.id.localeCompare(right.id))
  const providerDetailOpen = providerDetailMode !== 'empty'
  const aliasDetailOpen = aliasDetailMode !== 'empty'
  const selectedLogTrace = requestTraces.find((trace) => trace.id === selectedLogTraceId) || null
  const selectedNetworkTrace = requestTraces.find((trace) => trace.id === selectedNetworkTraceId) || null
  const logDetailOpen = selectedLogTraceId !== null && selectedLogTrace !== null
  const networkDetailOpen = selectedNetworkTraceId !== null && selectedNetworkTrace !== null
  const proxyRunning = overview?.proxy.running ?? false
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
      try {
        const traces = await listRequestTraces()
        if (cancelled) {
          return
        }
        setRequestTraces(traces)
        setSelectedLogTraceId((current) => resolveSelectedTraceId(current, traces))
        setSelectedNetworkTraceId((current) => resolveSelectedTraceId(current, traces))
        setTraceStatus(i18n.t('messages.fresh'))
      } catch (error) {
        if (!cancelled) {
          setTraceStatus(formatError(error))
        }
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
  }, [activeTab])

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
  }

  function resetAliasForm() {
    setEditingAliasId('')
    setAliasForm(emptyAliasForm)
  }

  function selectProviderDetail(provider: ProviderView) {
    setSelectedProviderId(provider.id)
    setEditingProviderId(provider.id)
    setProviderForm(providerFormFromView(provider))
    setProviderDetailMode('edit')
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
    const currentAlias = aliases.find((item) => item.alias === aliasName) || null
    const nextProtocol = resolveAliasProtocol(currentAlias)
    const nextProvider = providers.find((provider) => provider.protocol === nextProtocol)?.id || ''
    setTargetForm({ ...emptyTargetForm, alias: aliasName, provider: nextProvider })
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
    setPrefsStatus(i18n.t('messages.running'))
    try {
      await startProxy()
      setPrefsStatus(i18n.t('messages.proxyStarted'))
      await refreshAll()
    } catch (error) {
      setPrefsStatus(formatError(error))
    }
  }

  async function onStopProxy() {
    setPrefsStatus(i18n.t('messages.running'))
    try {
      await stopProxy()
      setPrefsStatus(i18n.t('messages.proxyStopped'))
      await refreshAll()
    } catch (error) {
      setPrefsStatus(formatError(error))
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
        baseUrl: providerForm.baseUrl.trim(),
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
    const currentAlias = aliases.find((item) => item.alias === alias.trim()) || null
    const nextProtocol = resolveAliasProtocol(currentAlias || selectedAlias)
    setTargetForm((current) => {
      const nextProvider = current.provider && providers.some((provider) => provider.id === current.provider && provider.protocol === nextProtocol)
        ? current.provider
        : providers.find((provider) => provider.protocol === nextProtocol)?.id || ''
      return { ...current, alias, provider: nextProvider }
    })
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
                    doctorResult.report.issues.map((issue, index) => (
                      <div className="issue-card" key={`${index}-${issue.message}`}>
                        {issue.message}
                      </div>
                    ))
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
                        <p className="resource-card-subtitle">{provider.baseUrl}</p>
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
                        <span className="resource-meta-value">{provider.baseUrl}</span>
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
              <pre className="details">{typeof syncOutput === 'string' ? syncOutput || t('messages.noData') : pretty(syncOutput)}</pre>
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
                      doctorResult.report.issues.map((issue, index) => (
                        <div className="issue-card" key={`${index}-${issue.message}`}>
                          {issue.message}
                        </div>
                      ))
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
          <section className="tab-layout aliases-layout">
            <article className="panel panel-fill list-column">
              <div className="panel-header">
                <div>
                  <h3>{t('log.title')}</h3>
                  <p className="subtle">{traceStatus || t('log.subtitle')}</p>
                </div>
                <div className="list-header-actions">
                  <span className="subtle list-status-text">{t('log.count', { count: requestTraces.length })}</span>
                </div>
              </div>
              <div className="scroll-list compact-list trace-scroll-list">
                {requestTraces.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('log.empty')}</h4>
                    <p className="subtle">{t('log.emptyHint')}</p>
                  </article>
                ) : null}
                {requestTraces.map((trace) => (
                  <article
                    key={trace.id}
                    className={`resource-card ${logDetailOpen && selectedLogTrace?.id === trace.id ? 'active' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => setSelectedLogTraceId(trace.id)}
                    onKeyDown={(event) => onResourceCardKeyDown(event, () => setSelectedLogTraceId(trace.id))}
                  >
                    <div className="resource-card-top">
                      <div className="resource-card-heading">
                        <div className="resource-card-titlewrap">
                          <strong className="resource-card-title">{trace.alias || trace.rawModel || `#${trace.id}`}</strong>
                          <code className="resource-card-code">#{trace.id}</code>
                        </div>
                        <p className="resource-card-subtitle trace-card-subtitle">
                          <span>{tracePrimaryText(trace)}</span>
                          <span>{formatDuration(trace.durationMs)}</span>
                        </p>
                      </div>
                      <div className="resource-card-side">
                        <span className={`badge status-badge ${trace.success ? 'live' : 'idle'}`}>
                          {trace.success ? t('log.success') : t('log.failed')}
                        </span>
                      </div>
                    </div>
                    <div className="resource-card-meta">
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('common.protocol')}</span>
                        <span className={protocolBadgeClass(trace.protocol)}>{protocolLabel(trace.protocol)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.startedAt')}</span>
                        <span className="resource-meta-value">{formatDateTime(trace.startedAt)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.firstByte')}</span>
                        <span className="resource-meta-value">{formatDuration(trace.firstByteMs)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.chainTitle')}</span>
                        <span className="resource-meta-value">
                          {trace.failover
                            ? `${trace.attemptCount} · ${t('log.failover')}`
                            : `${trace.attemptCount}`}
                        </span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.outputTokens')}</span>
                        <span className="resource-meta-value">{formatTokenCount(trace.outputTokens)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.outputRate')}</span>
                        <span className="resource-meta-value">{formatTokenRate(trace)}</span>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'network' ? (
          <section className="tab-layout aliases-layout">
            <article className="panel panel-fill list-column">
              <div className="panel-header">
                <div>
                  <h3>{t('network.title')}</h3>
                  <p className="subtle">{traceStatus || t('network.subtitle')}</p>
                </div>
                <div className="list-header-actions">
                  <span className="subtle list-status-text">{t('network.count', { count: requestTraces.length })}</span>
                </div>
              </div>
              <div className="scroll-list compact-list trace-scroll-list">
                {requestTraces.length === 0 ? (
                  <article className="empty-card compact-empty">
                    <h4>{t('log.empty')}</h4>
                    <p className="subtle">{t('log.emptyHint')}</p>
                  </article>
                ) : null}
                {requestTraces.map((trace) => (
                  <article
                    key={trace.id}
                    className={`resource-card ${networkDetailOpen && selectedNetworkTrace?.id === trace.id ? 'active' : ''}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => setSelectedNetworkTraceId(trace.id)}
                    onKeyDown={(event) => onResourceCardKeyDown(event, () => setSelectedNetworkTraceId(trace.id))}
                  >
                    <div className="resource-card-top">
                      <div className="resource-card-heading">
                        <div className="resource-card-titlewrap">
                          <strong className="resource-card-title">#{trace.id}</strong>
                          <code className="resource-card-code">{trace.finalProvider || trace.alias || trace.rawModel || '-'}</code>
                        </div>
                        <p className="resource-card-subtitle trace-card-subtitle">
                          <span>{trace.finalUrl || tracePrimaryText(trace)}</span>
                          <span>{formatDuration(trace.firstByteMs)}</span>
                        </p>
                      </div>
                      <div className="resource-card-side">
                        <span className={`badge status-badge ${trace.success ? 'live' : 'idle'}`}>{trace.statusCode || '-'}</span>
                      </div>
                    </div>
                    <div className="resource-card-meta">
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('common.protocol')}</span>
                        <span className={protocolBadgeClass(trace.protocol)}>{protocolLabel(trace.protocol)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.startedAt')}</span>
                        <span className="resource-meta-value">{formatDateTime(trace.startedAt)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('network.totalTime')}</span>
                        <span className="resource-meta-value">{formatDuration(trace.durationMs)}</span>
                      </div>
                      <div className="resource-meta-item">
                        <span className="resource-meta-label">{t('log.chainTitle')}</span>
                        <span className="resource-meta-value">{trace.attemptCount}</span>
                      </div>
                    </div>
                  </article>
                ))}
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
                    <p className="subtle detail-hero-subtle">{selectedProvider?.baseUrl || providerForm.baseUrl || '-'}</p>
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
                    <label className="detail-form-span">
                      <span>{t('providers.baseUrl')}</span>
                      <input
                        type="text"
                        value={providerForm.baseUrl}
                        onChange={(event) => setProviderForm((current) => ({ ...current, baseUrl: event.target.value }))}
                        placeholder={t('providers.placeholderBaseUrl')}
                      />
                    </label>
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
                    onChange={(event) => setTargetForm((current) => ({ ...current, provider: event.target.value }))}
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
                  <input
                    type="text"
                    value={targetForm.model}
                    onChange={(event) => setTargetForm((current) => ({ ...current, model: event.target.value }))}
                    placeholder={t('aliases.placeholderModel')}
                  />
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
                    {selectedAlias?.targets.map((target) => (
                      <div className="target-card" key={`${selectedAlias.alias}-${target.provider}-${target.model}`}>
                        <div className="target-card-main">
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
