import { FormEvent, useCallback, useEffect, useState } from 'react'
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
import type {
  AliasTargetInput,
  AliasView,
  AliasUpsertInput,
  DesktopPrefsSaveResult,
  DesktopPrefsView,
  DoctorRunResult,
  Overview,
  ProviderImportInput,
  ProviderImportResult,
  ProviderSaveResult,
  ProviderUpsertInput,
  ProviderView,
  SyncInput,
  SyncPreview,
  SyncResult,
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

const emptyPrefs: DesktopPrefsView = {
  launchAtLogin: false,
  minimizeToTray: false,
  notifications: false,
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
      throw new Error(`invalid header ${JSON.stringify(line)}; use KEY=VALUE`)
    }
    const key = line.slice(0, index).trim().toLowerCase()
    const value = line.slice(index + 1).trim()
    if (!key) {
      throw new Error(`invalid header ${JSON.stringify(line)}; header name must not be empty`)
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

function providerSaveStatus(result: ProviderSaveResult): string {
  const base = `Saved provider ${result.provider.id}`
  const warnings = joinWarnings(result.warnings)
  return warnings ? `${base}. Warnings: ${warnings}` : base
}

function providerImportStatus(result: ProviderImportResult): string {
  const base = `Import done: imported=${result.imported}, skipped=${result.skipped}`
  const warnings = joinWarnings(result.warnings)
  return warnings ? `${base}. Warnings: ${warnings}` : base
}

function desktopPrefsSaveStatus(result: DesktopPrefsSaveResult): string {
  const warnings = joinWarnings(result.warnings)
  return warnings ? `Saved. Warnings: ${warnings}` : 'Saved'
}

export default function App() {
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

  const refreshAll = useCallback(async () => {
    setLoading(true)
    setPrefsStatus('Refreshing...')
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
      setPrefsStatus('Fresh')
    } catch (error) {
      setPrefsStatus(error instanceof Error ? error.message : String(error))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refreshAll()
  }, [refreshAll])

  async function onSavePrefs(event: FormEvent) {
    event.preventDefault()
    setPrefsStatus('Saving...')
    try {
      const saved = await saveDesktopPrefs(prefs)
      setPrefs(saved.prefs)
      await refreshAll()
      setPrefsStatus(desktopPrefsSaveStatus(saved))
    } catch (error) {
      setPrefsStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onRunDoctor() {
    setDoctorStatus('Running...')
    try {
      const result = await runDoctor()
      setDoctorResult(result)
      setDoctorStatus(result.error || 'Doctor OK')
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      setDoctorResult(null)
      setDoctorStatus(message)
    }
  }

  async function onStartProxy() {
    try {
      await startProxy()
      await refreshAll()
    } catch (error) {
      setPrefsStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onStopProxy() {
    try {
      await stopProxy()
      await refreshAll()
    } catch (error) {
      setPrefsStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onPreviewSync() {
    setSyncStatus('Previewing...')
    try {
      const result = await previewSync(syncInput)
      setSyncOutput(result)
      setSyncStatus(result.wouldChange ? 'Preview shows changes' : 'Preview shows no changes')
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      setSyncOutput(message)
      setSyncStatus(message)
    }
  }

  async function onApplySync(event: FormEvent) {
    event.preventDefault()
    setSyncStatus('Applying...')
    try {
      const result = await applySync(syncInput)
      setSyncOutput(result)
      setSyncStatus(result.changed ? 'Sync applied' : 'Already up to date')
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      setSyncOutput(message)
      setSyncStatus(message)
    }
  }

  async function onSaveProvider(event: FormEvent) {
    event.preventDefault()
    setProviderStatus('Saving provider...')
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
      setProviderForm(emptyProviderForm)
      setProviderStatus(providerSaveStatus(result))
      await refreshAll()
    } catch (error) {
      setProviderStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onImportProviders(event: FormEvent) {
    event.preventDefault()
    setProviderStatus('Importing providers...')
    try {
      const result = await importProviders({
        sourcePath: providerImportForm.sourcePath?.trim(),
        overwrite: providerImportForm.overwrite,
      })
      setProviderStatus(providerImportStatus(result))
      await refreshAll()
    } catch (error) {
      setProviderStatus(error instanceof Error ? error.message : String(error))
    }
  }

  function onEditProvider(provider: ProviderView) {
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
    setProviderStatus(`Editing provider ${provider.id}. Leave API key blank to keep current value.`)
  }

  async function onToggleProvider(provider: ProviderView) {
    setProviderStatus(`${provider.disabled ? 'Enabling' : 'Disabling'} provider ${provider.id}...`)
    try {
      await setProviderState({ id: provider.id, disabled: !provider.disabled })
      setProviderStatus(`${provider.disabled ? 'Enabled' : 'Disabled'} provider ${provider.id}`)
      await refreshAll()
    } catch (error) {
      setProviderStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onDeleteProvider(id: string) {
    setProviderStatus(`Deleting provider ${id}...`)
    try {
      await deleteProvider(id)
      setProviderStatus(`Deleted provider ${id}`)
      await refreshAll()
    } catch (error) {
      setProviderStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onSaveAlias(event: FormEvent) {
    event.preventDefault()
    setAliasStatus('Saving alias...')
    try {
      const input: AliasUpsertInput = {
        alias: aliasForm.alias.trim(),
        displayName: aliasForm.displayName.trim(),
        disabled: aliasForm.disabled,
      }
      await saveAlias(input)
      setAliasForm(emptyAliasForm)
      setAliasStatus(`Saved alias ${input.alias}`)
      await refreshAll()
    } catch (error) {
      setAliasStatus(error instanceof Error ? error.message : String(error))
    }
  }

  function onEditAlias(alias: AliasView) {
    setAliasForm({
      alias: alias.alias,
      displayName: alias.displayName || '',
      disabled: !alias.enabled,
    })
    setTargetForm((current) => ({ ...current, alias: alias.alias }))
    setAliasStatus(`Editing alias ${alias.alias}`)
  }

  async function onDeleteAlias(alias: string) {
    setAliasStatus(`Deleting alias ${alias}...`)
    try {
      await deleteAlias(alias)
      setAliasStatus(`Deleted alias ${alias}`)
      await refreshAll()
    } catch (error) {
      setAliasStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onBindTarget(event: FormEvent) {
    event.preventDefault()
    setAliasStatus('Binding target...')
    try {
      const input: AliasTargetInput = {
        alias: targetForm.alias.trim(),
        provider: targetForm.provider.trim(),
        model: targetForm.model.trim(),
        disabled: targetForm.disabled,
      }
      await bindAliasTarget(input)
      setTargetForm((current) => ({ ...emptyTargetForm, alias: current.alias }))
      setAliasStatus(`Bound ${input.provider}/${input.model} to ${input.alias}`)
      await refreshAll()
    } catch (error) {
      setAliasStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onUnbindTarget(alias: string, provider: string, model: string) {
    setAliasStatus(`Removing ${provider}/${model} from ${alias}...`)
    try {
      await unbindAliasTarget({ alias, provider, model, disabled: false })
      setAliasStatus(`Removed ${provider}/${model} from ${alias}`)
      await refreshAll()
    } catch (error) {
      setAliasStatus(error instanceof Error ? error.message : String(error))
    }
  }

  async function onToggleTarget(alias: string, provider: string, model: string, currentlyDisabled: boolean) {
    setAliasStatus(`${currentlyDisabled ? 'Enabling' : 'Disabling'} ${provider}/${model} on ${alias}...`)
    try {
      await setAliasTargetState({ alias, provider, model, disabled: !currentlyDisabled })
      setAliasStatus(`${currentlyDisabled ? 'Enabled' : 'Disabled'} ${provider}/${model} on ${alias}`)
      await refreshAll()
    } catch (error) {
      setAliasStatus(error instanceof Error ? error.message : String(error))
    }
  }

  const stats = overview
    ? [
        ['Providers', String(overview.providerCount)],
        ['Aliases', String(overview.aliasCount)],
        ['Routable aliases', String(overview.availableAliases.length)],
        ['Proxy', overview.proxy.running ? 'Running' : 'Idle'],
      ]
    : []

  return (
    <div className="shell">
      <header className="hero">
        <div>
          <p className="eyebrow">ocswitch desktop</p>
          <h1>Native control panel</h1>
          <p className="subtle">
            {meta.version || 'dev'} · {meta.shell} shell · {overview?.configPath || 'loading config'}
          </p>
        </div>
        <div className="hero-actions">
          <button type="button" className="primary" onClick={() => void refreshAll()} disabled={loading}>
            Refresh
          </button>
          <button type="button" onClick={() => void onRunDoctor()}>
            Run doctor
          </button>
        </div>
      </header>

      <main className="grid">
        <section className="panel overview-panel">
          <div className="panel-header">
            <h2>Overview</h2>
            <span className={`badge ${overview?.proxy.running ? 'live' : 'idle'}`}>
              {overview?.proxy.running ? 'Proxy running' : 'Proxy idle'}
            </span>
          </div>
          <div className="stats">
            {stats.map(([label, value]) => (
              <div className="stat" key={label}>
                <span className="stat-label">{label}</span>
                <span className="stat-value">{value}</span>
              </div>
            ))}
          </div>
          <div className="toolbar">
            <button type="button" className="primary" onClick={() => void onStartProxy()}>
              Start proxy
            </button>
            <button type="button" onClick={() => void onStopProxy()}>
              Stop proxy
            </button>
          </div>
          <pre className="details">{overview ? pretty(overview) : 'Loading overview...'}</pre>
        </section>

        <section className="panel">
          <div className="panel-header">
            <h2>Desktop prefs</h2>
            <span className="subtle">{prefsStatus}</span>
          </div>
          <form className="stack" onSubmit={(event) => void onSavePrefs(event)}>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={prefs.launchAtLogin}
                onChange={(event) => setPrefs((current) => ({ ...current, launchAtLogin: event.target.checked }))}
              />
              <span>Launch at login</span>
            </label>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={prefs.minimizeToTray}
                onChange={(event) => setPrefs((current) => ({ ...current, minimizeToTray: event.target.checked }))}
              />
              <span>Minimize to tray / close to background</span>
            </label>
            <label className="checkbox-row">
              <input
                type="checkbox"
                checked={prefs.notifications}
                onChange={(event) => setPrefs((current) => ({ ...current, notifications: event.target.checked }))}
              />
              <span>Native notifications</span>
            </label>
            <button type="submit" className="primary">
              Save settings
            </button>
          </form>
        </section>

        <section className="panel">
          <div className="panel-header">
            <h2>OpenCode sync</h2>
            <span className="subtle">{syncStatus}</span>
          </div>
          <form className="stack" onSubmit={(event) => void onApplySync(event)}>
            <label>
              <span>Target path</span>
              <input
                type="text"
                value={syncInput.target || ''}
                onChange={(event) => setSyncInput((current) => ({ ...current, target: event.target.value }))}
                placeholder="Use default OpenCode config"
              />
            </label>
            <label>
              <span>model</span>
              <input
                type="text"
                value={syncInput.setModel || ''}
                onChange={(event) => setSyncInput((current) => ({ ...current, setModel: event.target.value }))}
                placeholder="ocswitch/<alias>"
              />
            </label>
            <label>
              <span>small_model</span>
              <input
                type="text"
                value={syncInput.setSmallModel || ''}
                onChange={(event) => setSyncInput((current) => ({ ...current, setSmallModel: event.target.value }))}
                placeholder="ocswitch/<alias>"
              />
            </label>
            <div className="toolbar">
              <button type="button" onClick={() => void onPreviewSync()}>
                Preview
              </button>
              <button type="submit" className="primary">
                Apply sync
              </button>
            </div>
          </form>
          <pre className="details">{typeof syncOutput === 'string' ? syncOutput : pretty(syncOutput)}</pre>
        </section>

        <section className="panel">
          <div className="panel-header">
            <h2>Doctor report</h2>
            <span className={`subtle ${doctorResult?.error ? 'tone-error' : 'tone-ok'}`}>{doctorStatus}</span>
          </div>
          <pre className="details">{doctorResult ? pretty(doctorResult) : 'Run doctor to inspect config and OpenCode wiring.'}</pre>
        </section>

        <section className="panel wide">
          <div className="panel-header">
            <h2>Providers</h2>
            <span className="subtle">{providerStatus || `${providers.length} total`}</span>
          </div>
          <div className="split-layout">
            <div className="stack">
              <form className="stack" onSubmit={(event) => void onSaveProvider(event)}>
                <label>
                  <span>Provider id</span>
                  <input
                    type="text"
                    value={providerForm.id}
                    onChange={(event) => setProviderForm((current) => ({ ...current, id: event.target.value }))}
                    placeholder="su8"
                  />
                </label>
                <label>
                  <span>Display name</span>
                  <input
                    type="text"
                    value={providerForm.name}
                    onChange={(event) => setProviderForm((current) => ({ ...current, name: event.target.value }))}
                    placeholder="SU8"
                  />
                </label>
                <label>
                  <span>Base URL</span>
                  <input
                    type="text"
                    value={providerForm.baseUrl}
                    onChange={(event) => setProviderForm((current) => ({ ...current, baseUrl: event.target.value }))}
                    placeholder="https://example.com/v1"
                  />
                </label>
                <label>
                  <span>API key</span>
                  <input
                    type="text"
                    value={providerForm.apiKey}
                    onChange={(event) => setProviderForm((current) => ({ ...current, apiKey: event.target.value }))}
                    placeholder="Leave blank to keep existing key when editing"
                  />
                </label>
                <label>
                  <span>Headers</span>
                  <textarea
                    value={providerForm.headersText}
                    onChange={(event) => setProviderForm((current) => ({ ...current, headersText: event.target.value }))}
                    placeholder={'X-Token=abc\nX-Workspace=my-team'}
                    rows={4}
                  />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={providerForm.disabled}
                    onChange={(event) => setProviderForm((current) => ({ ...current, disabled: event.target.checked }))}
                  />
                  <span>Save as disabled</span>
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={providerForm.skipModels}
                    onChange={(event) => setProviderForm((current) => ({ ...current, skipModels: event.target.checked }))}
                  />
                  <span>Skip /v1/models discovery</span>
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={providerForm.clearHeaders}
                    onChange={(event) => setProviderForm((current) => ({ ...current, clearHeaders: event.target.checked }))}
                  />
                  <span>Clear saved headers before update</span>
                </label>
                <div className="toolbar">
                  <button type="submit" className="primary">
                    Save provider
                  </button>
                  <button type="button" onClick={() => setProviderForm(emptyProviderForm)}>
                    Reset
                  </button>
                </div>
              </form>

              <form className="stack" onSubmit={(event) => void onImportProviders(event)}>
                <label>
                  <span>Import from OpenCode config</span>
                  <input
                    type="text"
                    value={providerImportForm.sourcePath || ''}
                    onChange={(event) => setProviderImportForm((current) => ({ ...current, sourcePath: event.target.value }))}
                    placeholder="Leave blank to use global OpenCode config"
                  />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={providerImportForm.overwrite}
                    onChange={(event) => setProviderImportForm((current) => ({ ...current, overwrite: event.target.checked }))}
                  />
                  <span>Overwrite existing providers</span>
                </label>
                <button type="submit">Import providers</button>
              </form>
            </div>

            <div className="list">
              {providers.length === 0 ? <p className="subtle">No providers configured yet.</p> : null}
              {providers.map((provider) => (
                <article className="item-card" key={provider.id}>
                  <div>
                    <strong>{provider.name || provider.id}</strong>
                    <br />
                    <code>{provider.baseUrl}</code>
                  </div>
                  <div className="item-meta">
                    API key: {provider.apiKeyMasked || 'not set'}
                    <br />
                    Headers: {headersTextFromMap(provider.headers) || 'none'}
                    <br />
                    Models: {provider.models?.join(', ') || 'none'}
                    <br />
                    Status: {provider.disabled ? 'disabled' : 'enabled'}
                  </div>
                  <div className="toolbar">
                    <button type="button" onClick={() => onEditProvider(provider)}>
                      Edit
                    </button>
                    <button type="button" onClick={() => void onToggleProvider(provider)}>
                      {provider.disabled ? 'Enable' : 'Disable'}
                    </button>
                    <button type="button" onClick={() => void onDeleteProvider(provider.id)}>
                      Delete
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section className="panel wide">
          <div className="panel-header">
            <h2>Aliases</h2>
            <span className="subtle">{aliasStatus || `${aliases.length} total`}</span>
          </div>
          <div className="split-layout">
            <div className="stack">
              <form className="stack" onSubmit={(event) => void onSaveAlias(event)}>
                <label>
                  <span>Alias</span>
                  <input
                    type="text"
                    value={aliasForm.alias}
                    onChange={(event) => setAliasForm((current) => ({ ...current, alias: event.target.value }))}
                    placeholder="gpt-5.4"
                  />
                </label>
                <label>
                  <span>Display name</span>
                  <input
                    type="text"
                    value={aliasForm.displayName}
                    onChange={(event) => setAliasForm((current) => ({ ...current, displayName: event.target.value }))}
                    placeholder="GPT 5.4"
                  />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={aliasForm.disabled}
                    onChange={(event) => setAliasForm((current) => ({ ...current, disabled: event.target.checked }))}
                  />
                  <span>Create or keep disabled</span>
                </label>
                <div className="toolbar">
                  <button type="submit" className="primary">
                    Save alias
                  </button>
                  <button type="button" onClick={() => setAliasForm(emptyAliasForm)}>
                    Reset
                  </button>
                </div>
              </form>

              <form className="stack" onSubmit={(event) => void onBindTarget(event)}>
                <label>
                  <span>Alias for binding</span>
                  <input
                    type="text"
                    value={targetForm.alias}
                    onChange={(event) => setTargetForm((current) => ({ ...current, alias: event.target.value }))}
                    placeholder="Existing alias or new alias"
                  />
                </label>
                <label>
                  <span>Provider id</span>
                  <input
                    type="text"
                    value={targetForm.provider}
                    onChange={(event) => setTargetForm((current) => ({ ...current, provider: event.target.value }))}
                    placeholder="su8"
                  />
                </label>
                <label>
                  <span>Model</span>
                  <input
                    type="text"
                    value={targetForm.model}
                    onChange={(event) => setTargetForm((current) => ({ ...current, model: event.target.value }))}
                    placeholder="gpt-5.4"
                  />
                </label>
                <label className="checkbox-row">
                  <input
                    type="checkbox"
                    checked={targetForm.disabled}
                    onChange={(event) => setTargetForm((current) => ({ ...current, disabled: event.target.checked }))}
                  />
                  <span>Add target disabled</span>
                </label>
                <div className="toolbar">
                  <button type="submit">Bind target</button>
                  <button type="button" onClick={() => setTargetForm(emptyTargetForm)}>
                    Reset target form
                  </button>
                </div>
              </form>
            </div>

            <div className="list">
              {aliases.length === 0 ? <p className="subtle">No aliases configured yet.</p> : null}
              {aliases.map((alias) => (
                <article className="item-card" key={alias.alias}>
                  <div>
                    <strong>{alias.displayName || alias.alias}</strong>
                    <br />
                    <code>{alias.alias}</code>
                  </div>
                  <div className="item-meta">
                    Targets: {alias.availableTargetCount}/{alias.targetCount} routable
                    <br />
                    Status: {alias.enabled ? 'enabled' : 'disabled'}
                  </div>
                  <div className="toolbar">
                    <button type="button" onClick={() => onEditAlias(alias)}>
                      Edit
                    </button>
                    <button type="button" onClick={() => setTargetForm((current) => ({ ...current, alias: alias.alias }))}>
                      Use in bind form
                    </button>
                    <button type="button" onClick={() => void onDeleteAlias(alias.alias)}>
                      Delete
                    </button>
                  </div>
                  <div className="list compact-list">
                    {alias.targets.length === 0 ? <p className="subtle">No targets bound.</p> : null}
                    {alias.targets.map((target) => (
                      <div className="item-card nested-card" key={`${alias.alias}-${target.provider}-${target.model}`}>
                        <div>
                          <code>
                            {target.provider}/{target.model}
                          </code>
                          {!target.enabled ? ' (disabled)' : ''}
                        </div>
                        <div className="toolbar">
                          <button
                            type="button"
                            onClick={() =>
                              void onToggleTarget(alias.alias, target.provider, target.model, !target.enabled)
                            }
                          >
                            {target.enabled ? 'Disable' : 'Enable'}
                          </button>
                          <button
                            type="button"
                            onClick={() =>
                              void onUnbindTarget(alias.alias, target.provider, target.model)
                            }
                          >
                            Unbind
                          </button>
                        </div>
                      </div>
                    ))}
                  </div>
                </article>
              ))}
            </div>
          </div>
        </section>
      </main>
    </div>
  )
}
