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
  streamPrecommitBufferMs: number
  excludeFirstTokenLatencyFromRate: boolean
  failoverStatusCodes: number[]
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

export type RequestRewriteOperation = {
  op: 'set' | 'delete' | 'append' | 'insert'
  path: string
  value?: unknown
  index?: number
}

export type RequestRewriteRuleView = {
  name: string
  alias?: string
  providers?: string[]
  enabled: boolean
  override: boolean
  ops?: RequestRewriteOperation[]
  legacy?: boolean
  warnings?: string[]
}

export type RequestRewriteRuleInput = {
  name: string
  alias?: string
  providers?: string[]
  enabled: boolean
  override: boolean
  ops?: RequestRewriteOperation[]
}

export type RequestRewriteRuleStateInput = {
  name: string
  enabled: boolean
}

export type RequestRewriteRuleRemoveInput = {
  name: string
}

export type RequestRewriteRuleReorderInput = {
  names: string[]
}

export type RequestRewriteRuleRemoveResult = {
  ok: boolean
}

export type RequestRewriteRuleReorderResult = {
  rules: RequestRewriteRuleView[]
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
  apiKeyIndex?: number
  apiKeyMasked?: string
  startedAt: string
  durationMs: number
  firstByteMs?: number
  firstTokenMs?: number
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
  firstTokenMs?: number
  usage?: TraceUsage
  inputTokens?: number
  outputTokens?: number
  generatedOutputTokens?: number
  protocol: ProviderProtocol
  rawModel?: string
  alias?: string
  stream: boolean
  success: boolean
  statusCode?: number
  error?: string
  errorCode?: string
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
  startedFrom?: string
  startedTo?: string
}

export type TraceStats = {
  success: number
  failover: number
  failed: number
}

export type RequestTraceDetailInput = {
  id: number
}

export type RequestTraceListResult = {
  items: RequestTrace[]
  total: number
  page: number
  pageSize: number
  availableAliases?: string[]
  availableFailoverCounts?: number[]
  availableStatusCodes?: number[]
  stats: TraceStats
}

export type ProviderHealthInput = {
  aliases?: string[]
  providers?: string[]
  startedFrom?: string
  startedTo?: string
}

export type ProviderHealthSummary = {
  requestCount: number
  attemptCount: number
  success: number
  failed: number
  failover: number
  retryableFailures: number
  rateLimited: number
  upstream5xx: number
  timeouts: number
  transportErrors: number
  streamErrors: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  cacheReadTokens: number
  cacheHitRate: number
  firstByteP50Ms?: number
  firstByteP95Ms?: number
  durationP50Ms?: number
  durationP95Ms?: number
  sampledProviders: number
  lowSampleProviders: number
}

export type ProviderHealthAlias = {
  alias: string
  model?: string
  role: string
  targetIndex: number
  attempts: number
  success: number
}

export type ProviderHealthView = {
  provider: string
  name?: string
  protocol?: ProviderProtocol
  role: string
  configured: boolean
  disabled?: boolean
  sampleLevel: string
  requestCount: number
  attemptCount: number
  primaryAttempts: number
  backupAttempts: number
  success: number
  finalSuccess: number
  terminalFailures: number
  retryableFailures: number
  skipped: number
  rateLimited: number
  upstream5xx: number
  upstream4xx: number
  timeouts: number
  transportErrors: number
  streamErrors: number
  emptyResponses: number
  otherFailures: number
  failoverInvolved: number
  inputTokens: number
  outputTokens: number
  totalTokens: number
  cacheReadTokens: number
  cacheHitRate: number
  firstByteP50Ms?: number
  firstByteP95Ms?: number
  durationP50Ms?: number
  durationP95Ms?: number
  observedSuccessRate: number
  retryableFailureRate: number
  aliases?: ProviderHealthAlias[]
}

export type ProviderHealthResult = {
  summary: ProviderHealthSummary
  providers: ProviderHealthView[]
  availableAliases?: string[]
  availableProviders?: string[]
  warnings?: string[]
}

export type Overview = {
  configPath: string
  providerCount: number
  aliasCount: number
  availableAliases: string[]
  traceStore: TraceStoreStatus
  proxy: ProxyStatusView
  desktop: DesktopPrefsView
}

export type TraceStoreStatus = {
  mode: string
  path?: string
  error?: string
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
  apiKeyCount: number
  apiKeysMasked?: string[]
  headers?: Record<string, string>
  models?: string[]
  modelsSource?: string
  disabled: boolean
  autoAliasEnabled: boolean
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
  apiKeys?: string[]
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
  apiKeys?: string[]
  clearApiKeys: boolean
  headers?: Record<string, string>
  disabled: boolean
  skipModels: boolean
  clearHeaders: boolean
  autoAliasEnabled?: boolean
}

export type AutoAliasSettingsInput = {
  enabled: boolean
}

export type AutoAliasSettingsResult = {
  enabled: boolean
}

export type AliasLockInput = {
  name: string
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
  providers?: ProviderView[]
}

export type AliasTargetView = {
  provider: string
  model: string
  enabled: boolean
  autoGenerated?: boolean
  available?: boolean
  reason?: string
  code?: string
  allowedActions?: string[]
}

export type AliasView = {
  alias: string
  displayName?: string
  protocol: ProviderProtocol
  enabled: boolean
  targetCount: number
  availableTargetCount: number
  targets: AliasTargetView[]
  autoGenerated?: boolean
  locked?: boolean
}

export type ProviderPriorityInput = {
  orderedIds: string[]
}

export type ProviderPriorityResult = {
  orderedIds: string[]
  warnings?: string[]
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

export type DiagnosticEntityRef = {
  kind: string
  key: string
  path?: string
}

export type DiagnosticTargetRef = {
  kind: string
  key: string
  path?: string | null
}

export type DoctorIssue = {
  schemaVersion?: number
  code: string
  severity: string
  path?: string
  source?: DiagnosticEntityRef
  target?: DiagnosticTargetRef
  reason?: string
  allowedActions?: string[]
  params?: Record<string, unknown>
  message?: string
  protocol?: string
  providerKey?: string
  alias?: string
  directory?: string
  expected?: string
  actual?: string
  actionHint?: string
  autoFixAvailable?: boolean
  details?: string[]
  relatedFields?: string[]
}

/** Opaque path-scoped config digest used by lifecycle APIs. */
export type ConfigRevision = string

export type LifecycleOperation = {
  kind: string
  payload: unknown
}

export type LifecycleSelection = {
  choiceId: string
  optionId: string
  params?: Record<string, unknown>
}

export type LifecycleChange = {
  id: string
  kind: string
  source: string
  entity: string
  reasonCode: string
  path?: string
  params?: Record<string, unknown>
}

export type LifecycleIssue = {
  id: string
  code: string
  disposition: string
  path?: string
  params?: Record<string, unknown>
}

export type LifecycleChoiceOption = {
  id: string
  params?: Record<string, unknown>
}

export type LifecycleChoice = {
  id: string
  code: string
  path?: string
  params?: Record<string, unknown>
  options: LifecycleChoiceOption[]
}

export type LifecycleRuntimeImpact = {
  providerRemoved?: boolean
  aliasRemoved?: boolean
  routingChanged?: boolean
}

export type LifecycleExternalRefs = {
  openCodeModel?: string
  openCodeSmallModel?: string
}

export type LifecyclePreviewInput = {
  revision: ConfigRevision
  operation: LifecycleOperation
  selections?: LifecycleSelection[]
  preparationToken?: string
  externalOpenCode?: LifecycleExternalRefs
}

export type LifecycleExecuteInput = {
  revision: ConfigRevision
  planToken: string
  operation: LifecycleOperation
  selections?: LifecycleSelection[]
  preparationToken?: string
}

export type LifecyclePlanView = {
  contractVersion: string
  plannerVersion: string
  baseRevision: ConfigRevision
  candidateRevision?: ConfigRevision
  operationKind: string
  executable: boolean
  noOp: boolean
  planToken?: string
  expiresAt?: string
  requestedChanges: LifecycleChange[]
  automaticChanges: LifecycleChange[]
  selectedChanges: LifecycleChange[]
  blockers: LifecycleIssue[]
  choices: LifecycleChoice[]
  preservedIssues: LifecycleIssue[]
  runtimeImpact: LifecycleRuntimeImpact
}

export type LifecycleExecuteResult = {
  contractVersion: string
  baseRevision: ConfigRevision
  committedRevision: ConfigRevision
  runtimeRevision?: ConfigRevision
  persisted: boolean
  writePerformed: boolean
  changed: boolean
  noOp: boolean
  candidateAlreadyPresent: boolean
  runtimeApplied: boolean
  pendingRestart: boolean
  runtimeState: string
  issues?: LifecycleIssue[]
  plan: LifecyclePlanView
}

export type ConfigRevisionView = {
  revision: ConfigRevision
}

/** Stable multi-client outcome field (artifact 06). */
export type TransportOutcome = {
  code: string
  params: Record<string, unknown>
  retryable: boolean
}

/** Canonical HTTP/Wails transport envelope. */
export type ApiEnvelope<T = unknown> = {
  ok: boolean
  data?: T
  error?: string
  outcome: TransportOutcome
}

/**
 * Typed transport failure that preserves full envelope/data.
 * React adapters throw this instead of discarding outcome on non-2xx.
 */
export class TransportError<T = unknown> extends Error {
  readonly ok: boolean
  readonly outcome: TransportOutcome
  readonly data?: T
  readonly httpStatus?: number

  constructor(envelope: ApiEnvelope<T>, httpStatus?: number) {
    super(envelope.error || envelope.outcome?.code || 'request failed')
    this.name = 'TransportError'
    this.ok = envelope.ok
    this.outcome = envelope.outcome || { code: 'internal_error', params: {}, retryable: false }
    this.data = envelope.data
    this.httpStatus = httpStatus
  }

  get code(): string {
    return this.outcome.code
  }

  get params(): Record<string, unknown> {
    return this.outcome.params || {}
  }

  get retryable(): boolean {
    return !!this.outcome.retryable
  }
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
  copyOnly?: boolean
  runtimeBaseUrl?: string
  runtimeDirectory?: string
}

/**
 * One OpenCode model field diff produced by the backend sync preview.
 * Existing user values are authoritative when status is `conflict`.
 */
export type SyncDiffEntry = {
  path: string
  userValue?: unknown
  proposedValue?: unknown
  status: 'new' | 'changed' | 'unchanged' | 'conflict' | 'failed'
  conflictNote?: string
  autoDetected: boolean
}

/** Summary counts for sync diff entries. */
export type DiffSummary = {
  total: number
  new: number
  changed: number
  unchanged: number
  conflict: number
  failed: number
}

/** Per-alias OpenCode sync preview with provider metadata and field-level diffs. */
export type AliasSyncPreview = {
  aliasName: string
  protocol: ProviderProtocol
  providerKey: string
  entries: SyncDiffEntry[]
  summary: DiffSummary
}

export type SyncPreview = {
  targetPath: string
  protocols: SyncedProviderView[]
  setModel?: string
  setSmallModel?: string
  content?: string
  wouldChange: boolean
  runtimeBaseUrl?: string
  runtimeDirectory?: string
  fileSnapshot: OpenCodeFileSnapshot
  runtimeSnapshot: OpenCodeRuntimeSnapshot
  doctorIssues?: DoctorIssue[]
  summary: OpenCodeReconciliationSummary
  aliasPreviews?: AliasSyncPreview[]
  overallSummary?: DiffSummary
}

export type SyncResult = {
  targetPath: string
  protocols: SyncedProviderView[]
  changed: boolean
  dryRun: boolean
  setModel?: string
  setSmallModel?: string
  content?: string
  runtimeBaseUrl?: string
  runtimeDirectory?: string
  fileSnapshot: OpenCodeFileSnapshot
  runtimeSnapshot: OpenCodeRuntimeSnapshot
  doctorIssues?: DoctorIssue[]
  summary: OpenCodeReconciliationSummary
  aliasPreviews?: AliasSyncPreview[]
  overallSummary?: DiffSummary
}

export type MetaView = {
  version: string
  shell: string
  url?: string
  capabilities?: ShellCapabilities
}

export type ShellCapabilities = {
  desktopPrefs: boolean
  openCodeDirectSync: boolean
  proxyControl: boolean
  transportEnvelopeVersion?: number
  lifecycleContractVersion?: number
}
