import { FormEvent, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  applySync,
  bindAliasTarget,
  deleteAlias,
  deleteProvider,
  getMeta,
  getOverview,
  importProviders,
  listAliases,
  listProviders,
  previewSync,
  runDoctor,
  saveAlias,
  saveDesktopPrefs,
  saveProvider,
  setAliasTargetState,
  setProviderState,
  startProxy,
  stopProxy,
  unbindAliasTarget,
} from './api'
import i18n, { resolveLanguagePreference } from './i18n'
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
  ProviderUpsertInput,
  ProviderView,
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
  disabled: boolean
}

type TabKey = 'overview' | 'providers' | 'aliases' | 'sync' | 'settings'
type FilterState = 'all' | 'enabled' | 'disabled'
type ResolvedTheme = 'light' | 'dark'

const tabs: TabKey[] = ['overview', 'providers', 'aliases', 'sync', 'settings']

const emptyPrefs: DesktopPrefsView = {
  launchAtLogin: false,
  minimizeToTray: false,
  notifications: false,
  theme: 'system',
  language: 'system',
}

const emptySync: SyncInput = {
  target: '',
  setModel: '',
  setSmallModel: '',
}

const emptyProviderForm: ProviderFormState = {
  id: '',
  name: '',
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
  disabled: false,
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
  const [prefsStatus, setPrefsStatus] = useState('')
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
  const [loading, setLoading] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('overview')
  const [providerQuery, setProviderQuery] = useState('')
  const [providerFilter, setProviderFilter] = useState<FilterState>('all')
  const [aliasQuery, setAliasQuery] = useState('')
  const [editingProviderId, setEditingProviderId] = useState('')
  const [editingAliasId, setEditingAliasId] = useState('')
  const [systemTheme, setSystemTheme] = useState<ResolvedTheme>('dark')
  const [systemLanguage, setSystemLanguage] = useState('en-US')

  const refreshAll = useCallback(async () => {
    setLoading(true)
    setPrefsStatus(i18n.t('messages.refreshing'))
    try {
      const [metaData, overviewData, providerData, aliasData] = await Promise.all([
        getMeta(),
        getOverview(),
        listProviders(),
        listAliases(),
      ])
      setMeta(metaData)
      setOverview(overviewData)
      setProviders(providerData)
      setAliases(aliasData)
      setPrefs(overviewData.desktop)
      setPrefsStatus(i18n.t('messages.fresh'))
    } catch (error) {
      setPrefsStatus(formatError(error))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshAll()
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
        baseUrl: providerForm.baseUrl.trim(),
        apiKey: providerForm.apiKey,
        headers: parseHeadersText(providerForm.headersText),
        disabled: providerForm.disabled,
        skipModels: providerForm.skipModels,
        clearHeaders: providerForm.clearHeaders,
      }
      const result = await saveProvider(input)
      resetProviderForm()
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
      setProviderStatus(providerImportStatus(result))
      await refreshAll()
    } catch (error) {
      setProviderStatus(formatError(error))
    }
  }

  function onEditProvider(provider: ProviderView) {
    setEditingProviderId(provider.id)
    setProviderForm({
      id: provider.id,
      name: provider.name || '',
      baseUrl: provider.baseUrl,
      apiKey: '',
      headersText: headersTextFromMap(provider.headers),
      disabled: provider.disabled,
      skipModels: false,
      clearHeaders: false,
    })
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
      if (editingProviderId === id) {
        resetProviderForm()
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
        disabled: aliasForm.disabled,
      }
      await saveAlias(input)
      resetAliasForm()
      setAliasStatus(i18n.t('aliases.statusSaved', { alias: input.alias }))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
  }

  function onEditAlias(alias: AliasView) {
    setEditingAliasId(alias.alias)
    setAliasForm({
      alias: alias.alias,
      displayName: alias.displayName || '',
      disabled: !alias.enabled,
    })
    setTargetForm((current) => ({ ...current, alias: alias.alias }))
    setAliasStatus(i18n.t('aliases.statusEditing', { alias: alias.alias }))
  }

  async function onDeleteAlias(alias: string) {
    setAliasStatus(i18n.t('aliases.statusDeleting', { alias }))
    try {
      await deleteAlias(alias)
      if (editingAliasId === alias) {
        resetAliasForm()
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
      setAliasStatus(i18n.t('aliases.statusBound', input))
      await refreshAll()
    } catch (error) {
      setAliasStatus(formatError(error))
    }
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

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="sidebar-brand">
          <p className="eyebrow">{t('app.eyebrow')}</p>
          <h1>{t('app.title')}</h1>
          <p className="subtle">
            {t('app.subtitle', {
              version: meta.version || t('app.dev'),
              shell: meta.shell,
              path: overview?.configPath || t('app.loadingConfig'),
            })}
          </p>
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
              <span className="nav-description">{t(`nav.${tab}.description`)}</span>
            </button>
          ))}
        </nav>

        <section className="sidebar-panel">
          <div className="sidebar-panel-header">
            <span>{t('overview.statsTitle')}</span>
            <span className={`badge ${overview?.proxy.running ? 'live' : 'idle'}`}>
              {overview?.proxy.running ? t('status.proxyRunning') : t('status.proxyIdle')}
            </span>
          </div>
          <div className="sidebar-stats">
            {stats.length === 0 ? <span className="subtle">{t('messages.loading')}</span> : null}
            {stats.map(([label, value]) => (
              <div className="sidebar-stat" key={label}>
                <span className="stat-label">{t(label)}</span>
                <strong>{value}</strong>
              </div>
            ))}
          </div>
        </section>
      </aside>

      <main className="workspace">
        <header className="workspace-header">
          <div>
            <p className="section-kicker">{t(`nav.${activeTab}.title`)}</p>
            <h2>{t(`nav.${activeTab}.title`)}</h2>
            <p className="subtle">{t(`nav.${activeTab}.description`)}</p>
          </div>
          <div className="hero-actions">
            <button type="button" className="primary" onClick={() => void refreshAll()} disabled={loading}>
              {t('actions.refresh')}
            </button>
            <button type="button" onClick={() => void onRunDoctor()}>
              {t('actions.runDoctor')}
            </button>
          </div>
        </header>

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
                <button type="button" className="primary" onClick={() => void onStartProxy()}>
                  {t('actions.startProxy')}
                </button>
                <button type="button" onClick={() => void onStopProxy()}>
                  {t('actions.stopProxy')}
                </button>
                <button type="button" onClick={() => void onRunDoctor()}>
                  {t('actions.runDoctor')}
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
                  <p className="subtle">{overview?.configPath || t('messages.loading')}</p>
                </div>
              </div>
              <dl className="info-grid">
                <div>
                  <dt>{t('overview.version')}</dt>
                  <dd>{meta.version || t('app.dev')}</dd>
                </div>
                <div>
                  <dt>{t('overview.shell')}</dt>
                  <dd>{meta.shell}</dd>
                </div>
                <div>
                  <dt>{t('overview.configPath')}</dt>
                  <dd>{overview?.configPath || t('app.loadingConfig')}</dd>
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
                <pre className="details">{overview ? pretty(overview) : t('messages.loading')}</pre>
              </details>
            </article>
          </section>
        ) : null}

        {activeTab === 'providers' ? (
          <section className="tab-layout">
            <article className="panel panel-full panel-fill">
              <div className="panel-header">
                <div>
                  <h3>{t('providers.title')}</h3>
                  <p className="subtle">{t('providers.subtitle')}</p>
                </div>
                <span className="subtle">
                  {providerStatus ||
                    t('providers.listCount', { shown: filteredProviders.length, total: providers.length })}
                </span>
              </div>

              <div className="split-view">
                <div className="editor-column stack-blocks">
                  <section className="subpanel">
                    <div className="subpanel-header">
                      <h4>
                        {editingProviderId
                          ? t('providers.formEditTitle', { id: editingProviderId })
                          : t('providers.formCreateTitle')}
                      </h4>
                      <button type="button" onClick={resetProviderForm}>
                        {editingProviderId ? t('actions.newProvider') : t('actions.reset')}
                      </button>
                    </div>
                    <form className="stack" onSubmit={(event) => void onSaveProvider(event)}>
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
                      <label>
                        <span>{t('providers.baseUrl')}</span>
                        <input
                          type="text"
                          value={providerForm.baseUrl}
                          onChange={(event) =>
                            setProviderForm((current) => ({ ...current, baseUrl: event.target.value }))
                          }
                          placeholder={t('providers.placeholderBaseUrl')}
                        />
                      </label>
                      <label>
                        <span>{t('providers.apiKey')}</span>
                        <input
                          type="text"
                          value={providerForm.apiKey}
                          onChange={(event) => setProviderForm((current) => ({ ...current, apiKey: event.target.value }))}
                          placeholder={t('providers.placeholderApiKey')}
                        />
                      </label>
                      <label>
                        <span>{t('providers.headers')}</span>
                        <textarea
                          value={providerForm.headersText}
                          onChange={(event) =>
                            setProviderForm((current) => ({ ...current, headersText: event.target.value }))
                          }
                          placeholder={t('providers.placeholderHeaders')}
                          rows={4}
                        />
                      </label>
                      <label className="checkbox-row">
                        <input
                          type="checkbox"
                          checked={providerForm.disabled}
                          onChange={(event) =>
                            setProviderForm((current) => ({ ...current, disabled: event.target.checked }))
                          }
                        />
                        <span>{t('providers.saveDisabled')}</span>
                      </label>
                      <label className="checkbox-row">
                        <input
                          type="checkbox"
                          checked={providerForm.skipModels}
                          onChange={(event) =>
                            setProviderForm((current) => ({ ...current, skipModels: event.target.checked }))
                          }
                        />
                        <span>{t('providers.skipModels')}</span>
                      </label>
                      <label className="checkbox-row">
                        <input
                          type="checkbox"
                          checked={providerForm.clearHeaders}
                          onChange={(event) =>
                            setProviderForm((current) => ({ ...current, clearHeaders: event.target.checked }))
                          }
                        />
                        <span>{t('providers.clearHeaders')}</span>
                      </label>
                      <div className="toolbar">
                        <button type="submit" className="primary">
                          {t('actions.save')}
                        </button>
                        <button type="button" onClick={resetProviderForm}>
                          {t('actions.reset')}
                        </button>
                      </div>
                    </form>
                  </section>

                  <section className="subpanel">
                    <div className="subpanel-header">
                      <h4>{t('providers.importTitle')}</h4>
                    </div>
                    <form className="stack" onSubmit={(event) => void onImportProviders(event)}>
                      <label>
                        <span>{t('providers.sourcePath')}</span>
                        <input
                          type="text"
                          value={providerImportForm.sourcePath || ''}
                          onChange={(event) =>
                            setProviderImportForm((current) => ({ ...current, sourcePath: event.target.value }))
                          }
                          placeholder={t('providers.placeholderSourcePath')}
                        />
                      </label>
                      <label className="checkbox-row">
                        <input
                          type="checkbox"
                          checked={providerImportForm.overwrite}
                          onChange={(event) =>
                            setProviderImportForm((current) => ({ ...current, overwrite: event.target.checked }))
                          }
                        />
                        <span>{t('providers.overwrite')}</span>
                      </label>
                      <button type="submit">{t('actions.import')}</button>
                    </form>
                  </section>
                </div>

                <div className="list-column">
                  <div className="list-toolbar">
                    <label>
                      <span>{t('providers.search')}</span>
                      <input
                        type="text"
                        value={providerQuery}
                        onChange={(event) => setProviderQuery(event.target.value)}
                        placeholder={t('providers.searchPlaceholder')}
                      />
                    </label>
                    <label>
                      <span>{t('providers.filter')}</span>
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

                  <div className="scroll-list">
                    {providers.length === 0 ? <p className="subtle">{t('providers.empty')}</p> : null}
                    {providers.length > 0 && filteredProviders.length === 0 ? (
                      <p className="subtle">{t('providers.noMatches')}</p>
                    ) : null}
                    {filteredProviders.map((provider) => (
                      <article className="item-card" key={provider.id}>
                        <div className="item-heading">
                          <div>
                            <strong>{provider.name || provider.id}</strong>
                            <code>{provider.id}</code>
                          </div>
                          <span className={`badge ${provider.disabled ? 'idle' : 'live'}`}>
                            {provider.disabled ? t('status.disabled') : t('status.enabled')}
                          </span>
                        </div>
                        <div className="item-grid">
                          <div>
                            <span className="meta-label">{t('providers.baseUrl')}</span>
                            <code>{provider.baseUrl}</code>
                          </div>
                          <div>
                            <span className="meta-label">{t('providers.apiKeyMasked')}</span>
                            <span>{provider.apiKeyMasked || t('providers.apiKeyNotSet')}</span>
                          </div>
                          <div>
                            <span className="meta-label">{t('providers.headers')}</span>
                            <span>{headersTextFromMap(provider.headers) || t('providers.headersNone')}</span>
                          </div>
                          <div>
                            <span className="meta-label">{t('providers.models')}</span>
                            <span>{provider.models?.join(', ') || t('providers.modelsNone')}</span>
                          </div>
                        </div>
                        {provider.modelsSource ? (
                          <p className="subtle small-text">
                            {t('providers.sourceLabel')}: {provider.modelsSource}
                          </p>
                        ) : null}
                        <div className="toolbar">
                          <button type="button" onClick={() => onEditProvider(provider)}>
                            {t('actions.edit')}
                          </button>
                          <button type="button" onClick={() => void onToggleProvider(provider)}>
                            {provider.disabled ? t('actions.enable') : t('actions.disable')}
                          </button>
                          <button type="button" onClick={() => void onDeleteProvider(provider.id)}>
                            {t('actions.delete')}
                          </button>
                        </div>
                      </article>
                    ))}
                  </div>
                </div>
              </div>
            </article>
          </section>
        ) : null}

        {activeTab === 'aliases' ? (
          <section className="tab-layout">
            <article className="panel panel-full panel-fill">
              <div className="panel-header">
                <div>
                  <h3>{t('aliases.title')}</h3>
                  <p className="subtle">{t('aliases.subtitle')}</p>
                </div>
                <span className="subtle">
                  {aliasStatus || t('aliases.listCount', { shown: filteredAliases.length, total: aliases.length })}
                </span>
              </div>

              <div className="split-view">
                <div className="editor-column stack-blocks">
                  <section className="subpanel">
                    <div className="subpanel-header">
                      <h4>
                        {editingAliasId
                          ? t('aliases.formEditTitle', { alias: editingAliasId })
                          : t('aliases.formCreateTitle')}
                      </h4>
                      <button type="button" onClick={resetAliasForm}>
                        {editingAliasId ? t('actions.newAlias') : t('actions.reset')}
                      </button>
                    </div>
                    <form className="stack" onSubmit={(event) => void onSaveAlias(event)}>
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
                          onChange={(event) =>
                            setAliasForm((current) => ({ ...current, displayName: event.target.value }))
                          }
                          placeholder={t('aliases.placeholderDisplayName')}
                        />
                      </label>
                      <label className="checkbox-row">
                        <input
                          type="checkbox"
                          checked={aliasForm.disabled}
                          onChange={(event) =>
                            setAliasForm((current) => ({ ...current, disabled: event.target.checked }))
                          }
                        />
                        <span>{t('aliases.createDisabled')}</span>
                      </label>
                      <div className="toolbar">
                        <button type="submit" className="primary">
                          {t('actions.save')}
                        </button>
                        <button type="button" onClick={resetAliasForm}>
                          {t('actions.reset')}
                        </button>
                      </div>
                    </form>
                  </section>

                  <section className="subpanel">
                    <div className="subpanel-header">
                      <h4>{t('aliases.bindTitle')}</h4>
                    </div>
                    <form className="stack" onSubmit={(event) => void onBindTarget(event)}>
                      <label>
                        <span>{t('aliases.aliasForBinding')}</span>
                        <input
                          type="text"
                          value={targetForm.alias}
                          onChange={(event) => setTargetForm((current) => ({ ...current, alias: event.target.value }))}
                          placeholder={t('aliases.placeholderAliasBinding')}
                        />
                      </label>
                      <label>
                        <span>{t('aliases.providerId')}</span>
                        <input
                          type="text"
                          value={targetForm.provider}
                          onChange={(event) =>
                            setTargetForm((current) => ({ ...current, provider: event.target.value }))
                          }
                          placeholder={t('aliases.placeholderProviderId')}
                        />
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
                          onChange={(event) =>
                            setTargetForm((current) => ({ ...current, disabled: event.target.checked }))
                          }
                        />
                        <span>{t('aliases.targetDisabled')}</span>
                      </label>
                      <div className="toolbar">
                        <button type="submit">{t('actions.bind')}</button>
                        <button type="button" onClick={() => setTargetForm(emptyTargetForm)}>
                          {t('actions.reset')}
                        </button>
                      </div>
                    </form>
                  </section>
                </div>

                <div className="list-column">
                  <div className="list-toolbar list-toolbar-single">
                    <label>
                      <span>{t('aliases.search')}</span>
                      <input
                        type="text"
                        value={aliasQuery}
                        onChange={(event) => setAliasQuery(event.target.value)}
                        placeholder={t('aliases.searchPlaceholder')}
                      />
                    </label>
                  </div>

                  <div className="scroll-list">
                    {aliases.length === 0 ? <p className="subtle">{t('aliases.empty')}</p> : null}
                    {aliases.length > 0 && filteredAliases.length === 0 ? (
                      <p className="subtle">{t('aliases.noMatches')}</p>
                    ) : null}
                    {filteredAliases.map((alias) => (
                      <article className="item-card" key={alias.alias}>
                        <div className="item-heading">
                          <div>
                            <strong>{alias.displayName || alias.alias}</strong>
                            <code>{alias.alias}</code>
                          </div>
                          <span className={`badge ${alias.enabled ? 'live' : 'idle'}`}>
                            {alias.enabled ? t('status.enabled') : t('status.disabled')}
                          </span>
                        </div>
                        <p className="subtle small-text">
                          {t('aliases.routable', {
                            available: alias.availableTargetCount,
                            total: alias.targetCount,
                          })}
                        </p>
                        <div className="toolbar">
                          <button type="button" onClick={() => onEditAlias(alias)}>
                            {t('actions.edit')}
                          </button>
                          <button
                            type="button"
                            onClick={() => setTargetForm((current) => ({ ...current, alias: alias.alias }))}
                          >
                            {t('actions.useInBindForm')}
                          </button>
                          <button type="button" onClick={() => void onDeleteAlias(alias.alias)}>
                            {t('actions.delete')}
                          </button>
                        </div>

                        <div className="target-list">
                          {alias.targets.length === 0 ? <p className="subtle">{t('aliases.noTargets')}</p> : null}
                          {alias.targets.map((target) => (
                            <div className="target-card" key={`${alias.alias}-${target.provider}-${target.model}`}>
                              <div>
                                <code>
                                  {target.provider}/{target.model}
                                </code>
                              </div>
                              <div className="toolbar">
                                <button
                                  type="button"
                                  onClick={() =>
                                    void onToggleTarget(alias.alias, target.provider, target.model, target.enabled)
                                  }
                                >
                                  {target.enabled ? t('actions.disable') : t('actions.enable')}
                                </button>
                                <button
                                  type="button"
                                  onClick={() => void onUnbindTarget(alias.alias, target.provider, target.model)}
                                >
                                  {t('actions.unbind')}
                                </button>
                              </div>
                            </div>
                          ))}
                        </div>
                      </article>
                    ))}
                  </div>
                </div>
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

        {activeTab === 'settings' ? (
          <section className="tab-layout settings-layout">
            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('settings.title')}</h3>
                  <p className="subtle">{t('settings.subtitle')}</p>
                </div>
                <span className="subtle">{prefsStatus}</span>
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
                      <span>{t('settings.language')}</span>
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

                <div className="toolbar">
                  <button type="submit" className="primary">
                    {t('actions.save')}
                  </button>
                </div>
              </form>
            </article>

            <article className="panel">
              <div className="panel-header">
                <div>
                  <h3>{t('settings.aboutTitle')}</h3>
                </div>
              </div>
              <dl className="info-grid">
                <div>
                  <dt>{t('overview.version')}</dt>
                  <dd>{meta.version || t('app.dev')}</dd>
                </div>
                <div>
                  <dt>{t('overview.shell')}</dt>
                  <dd>{meta.shell}</dd>
                </div>
                <div>
                  <dt>{t('overview.configPath')}</dt>
                  <dd>{overview?.configPath || t('app.loadingConfig')}</dd>
                </div>
                <div>
                  <dt>{t('settings.resolvedTheme')}</dt>
                  <dd>{t(`settings.theme${resolvedTheme === 'dark' ? 'Dark' : 'Light'}`)}</dd>
                </div>
              </dl>
            </article>
          </section>
        ) : null}
      </main>
    </div>
  )
}
