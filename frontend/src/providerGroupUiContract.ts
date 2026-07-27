/**
 * Frozen UI contract for Provider Groups (Step 7.3 to Step 8).
 *
 * Step 8 pages, locale files, and styles must consume only the i18n keys and
 * CSS classes exported here. Keys are identifiers only - no locale copy.
 *
 * Scope notes:
 * - Covers Provider shared settings, Group list/detail/CRUD, refresh models,
 *   ping, upstream-key three-state edit + mask hints, disabled state,
 *   Provider -> Group -> Model alias selection, delete/rename lifecycle planning
 *   hints, error/empty/loading states, and narrow-layout containers.
 * - Upstream API keys only (ocswitch -> provider). Do not reuse client proxy
 *   API Key labels, routes, or DTO concepts.
 */

// ---------------------------------------------------------------------------
// i18n keys - full paths under providers.groups.*
// ---------------------------------------------------------------------------

/**
 * Named map of every `providers.groups.*` key Step 8 is allowed to use.
 * Locale JSON must implement exactly this set (en + zh-CN).
 */
export const PROVIDER_GROUP_I18N_KEYS = {
  // Provider shared settings (connection-level, not group-scoped)
  sharedSettingsTitle: 'providers.groups.sharedSettingsTitle',
  sharedSettingsSubtitle: 'providers.groups.sharedSettingsSubtitle',
  sharedSettingsHint: 'providers.groups.sharedSettingsHint',
  sharedBaseUrl: 'providers.groups.sharedBaseUrl',
  sharedBaseUrls: 'providers.groups.sharedBaseUrls',
  sharedBaseUrlStrategy: 'providers.groups.sharedBaseUrlStrategy',
  sharedHeaders: 'providers.groups.sharedHeaders',
  sharedAutoAlias: 'providers.groups.sharedAutoAlias',
  sharedProviderDisabled: 'providers.groups.sharedProviderDisabled',
  sharedProviderDisabledHint: 'providers.groups.sharedProviderDisabledHint',

  // Group list
  listTitle: 'providers.groups.listTitle',
  listSubtitle: 'providers.groups.listSubtitle',
  listCount: 'providers.groups.listCount',
  listEmpty: 'providers.groups.listEmpty',
  listEmptyHint: 'providers.groups.listEmptyHint',
  listLoading: 'providers.groups.listLoading',
  listError: 'providers.groups.listError',
  listNoMatches: 'providers.groups.listNoMatches',
  listNoMatchesHint: 'providers.groups.listNoMatchesHint',
  listSelectHint: 'providers.groups.listSelectHint',
  listCardId: 'providers.groups.listCardId',
  listCardName: 'providers.groups.listCardName',
  listCardProtocol: 'providers.groups.listCardProtocol',
  listCardModels: 'providers.groups.listCardModels',
  listCardUpstreamKeys: 'providers.groups.listCardUpstreamKeys',
  listCardStatus: 'providers.groups.listCardStatus',
  listCardDefaultBadge: 'providers.groups.listCardDefaultBadge',
  providerGroupCount: 'providers.groups.providerGroupCount',
  providerGroupAvailableCount: 'providers.groups.providerGroupAvailableCount',

  // Group detail / form
  detailTitle: 'providers.groups.detailTitle',
  detailSubtitle: 'providers.groups.detailSubtitle',
  detailEmpty: 'providers.groups.detailEmpty',
  sectionIdentityStatus: 'providers.groups.sectionIdentityStatus',
  sectionProtocolModels: 'providers.groups.sectionProtocolModels',
  sectionUpstreamKeys: 'providers.groups.sectionUpstreamKeys',
  detailEmptyHint: 'providers.groups.detailEmptyHint',
  formCreateTitle: 'providers.groups.formCreateTitle',
  formEditTitle: 'providers.groups.formEditTitle',
  fieldId: 'providers.groups.fieldId',
  fieldIdHint: 'providers.groups.fieldIdHint',
  fieldName: 'providers.groups.fieldName',
  fieldNameHint: 'providers.groups.fieldNameHint',
  fieldProtocol: 'providers.groups.fieldProtocol',
  fieldDisabled: 'providers.groups.fieldDisabled',
  fieldDisabledHint: 'providers.groups.fieldDisabledHint',
  fieldModels: 'providers.groups.fieldModels',
  fieldModelsSource: 'providers.groups.fieldModelsSource',
  placeholderId: 'providers.groups.placeholderId',
  placeholderName: 'providers.groups.placeholderName',
  placeholderModel: 'providers.groups.placeholderModel',
  placeholderProtocol: 'providers.groups.placeholderProtocol',

  // CRUD actions
  actionAdd: 'providers.groups.actionAdd',
  actionCreate: 'providers.groups.actionCreate',
  actionSave: 'providers.groups.actionSave',
  actionReset: 'providers.groups.actionReset',
  actionCancel: 'providers.groups.actionCancel',
  actionDelete: 'providers.groups.actionDelete',
  actionEnable: 'providers.groups.actionEnable',
  actionDisable: 'providers.groups.actionDisable',
  actionEdit: 'providers.groups.actionEdit',
  actionRefreshModels: 'providers.groups.actionRefreshModels',
  actionPing: 'providers.groups.actionPing',

  // CRUD / operation status
  statusLoading: 'providers.groups.statusLoading',
  statusSaving: 'providers.groups.statusSaving',
  statusSaved: 'providers.groups.statusSaved',
  statusCreating: 'providers.groups.statusCreating',
  statusCreated: 'providers.groups.statusCreated',
  statusDeleting: 'providers.groups.statusDeleting',
  statusDeleted: 'providers.groups.statusDeleted',
  statusEnabling: 'providers.groups.statusEnabling',
  statusDisabling: 'providers.groups.statusDisabling',
  statusEnabled: 'providers.groups.statusEnabled',
  statusDisabled: 'providers.groups.statusDisabled',
  statusRefreshingModels: 'providers.groups.statusRefreshingModels',
  statusRefreshedModels: 'providers.groups.statusRefreshedModels',
  statusPinging: 'providers.groups.statusPinging',
  statusPingOk: 'providers.groups.statusPingOk',
  statusPingFail: 'providers.groups.statusPingFail',
  statusPingIdle: 'providers.groups.statusPingIdle',
  statusUnsaved: 'providers.groups.statusUnsaved',

  // Models
  modelsNone: 'providers.groups.modelsNone',
  modelsCount: 'providers.groups.modelsCount',
  modelsSourceLabel: 'providers.groups.modelsSourceLabel',
  modelsSourceDiscovered: 'providers.groups.modelsSourceDiscovered',
  modelsSourceConfigured: 'providers.groups.modelsSourceConfigured',
  modelsSourceEmpty: 'providers.groups.modelsSourceEmpty',
  modelsRefreshHint: 'providers.groups.modelsRefreshHint',

  // Upstream keys - three-state edit + mask (NOT client proxy API keys)
  upstreamKeysLabel: 'providers.groups.upstreamKeysLabel',
  upstreamKeysHint: 'providers.groups.upstreamKeysHint',
  upstreamKeysMaskHint: 'providers.groups.upstreamKeysMaskHint',
  upstreamKeysCurrent: 'providers.groups.upstreamKeysCurrent',
  upstreamKeysNotSet: 'providers.groups.upstreamKeysNotSet',
  upstreamKeysCount: 'providers.groups.upstreamKeysCount',
  upstreamKeysSummaryItem: 'providers.groups.upstreamKeysSummaryItem',
  upstreamKeysDraftCount: 'providers.groups.upstreamKeysDraftCount',
  upstreamKeysSavedMode: 'providers.groups.upstreamKeysSavedMode',
  upstreamKeysPendingMode: 'providers.groups.upstreamKeysPendingMode',
  upstreamKeysClearMode: 'providers.groups.upstreamKeysClearMode',
  upstreamKeysReplace: 'providers.groups.upstreamKeysReplace',
  upstreamKeysReplaceHint: 'providers.groups.upstreamKeysReplaceHint',
  upstreamKeysPendingReplace: 'providers.groups.upstreamKeysPendingReplace',
  upstreamKeysPendingClear: 'providers.groups.upstreamKeysPendingClear',
  upstreamKeysKeep: 'providers.groups.upstreamKeysKeep',
  upstreamKeysKeepHint: 'providers.groups.upstreamKeysKeepHint',
  upstreamKeysEmptyHint: 'providers.groups.upstreamKeysEmptyHint',
  upstreamKeysAdd: 'providers.groups.upstreamKeysAdd',
  upstreamKeysClear: 'providers.groups.upstreamKeysClear',
  upstreamKeysCancelEdit: 'providers.groups.upstreamKeysCancelEdit',
  placeholderUpstreamKey: 'providers.groups.placeholderUpstreamKey',
  placeholderUpstreamKeyAdditional: 'providers.groups.placeholderUpstreamKeyAdditional',
  errorMaskedKeyRejected: 'providers.groups.errorMaskedKeyRejected',

  // Disabled state
  badgeEnabled: 'providers.groups.badgeEnabled',
  badgeDisabled: 'providers.groups.badgeDisabled',
  groupDisabledHint: 'providers.groups.groupDisabledHint',
  providerAndGroupDisabledHint: 'providers.groups.providerAndGroupDisabledHint',
  disabledBlocksRouting: 'providers.groups.disabledBlocksRouting',

  // Alias target: Provider -> Group -> Model
  aliasSelectorTitle: 'providers.groups.aliasSelectorTitle',
  aliasSelectorSubtitle: 'providers.groups.aliasSelectorSubtitle',
  aliasSelectorProvider: 'providers.groups.aliasSelectorProvider',
  aliasSelectorGroup: 'providers.groups.aliasSelectorGroup',
  aliasSelectorModel: 'providers.groups.aliasSelectorModel',
  aliasSelectorProviderPlaceholder: 'providers.groups.aliasSelectorProviderPlaceholder',
  aliasSelectorGroupPlaceholder: 'providers.groups.aliasSelectorGroupPlaceholder',
  aliasSelectorModelPlaceholder: 'providers.groups.aliasSelectorModelPlaceholder',
  aliasSelectorNoProviders: 'providers.groups.aliasSelectorNoProviders',
  aliasSelectorNoGroups: 'providers.groups.aliasSelectorNoGroups',
  aliasSelectorNoCompatibleGroups: 'providers.groups.aliasSelectorNoCompatibleGroups',
  aliasSelectorNoModels: 'providers.groups.aliasSelectorNoModels',
  aliasSelectorProtocolMismatch: 'providers.groups.aliasSelectorProtocolMismatch',
  aliasSelectorGroupDisabled: 'providers.groups.aliasSelectorGroupDisabled',
  aliasSelectorProviderDisabled: 'providers.groups.aliasSelectorProviderDisabled',
  aliasTargetSummary: 'providers.groups.aliasTargetSummary',
  aliasTargetUnavailable: 'providers.groups.aliasTargetUnavailable',

  // Delete / rename reference planning (lifecycle)
  lifecycleDeleteTitle: 'providers.groups.lifecycleDeleteTitle',
  lifecycleDeleteHint: 'providers.groups.lifecycleDeleteHint',
  lifecycleRenameTitle: 'providers.groups.lifecycleRenameTitle',
  lifecycleRenameHint: 'providers.groups.lifecycleRenameHint',
  lifecycleIdChangeTitle: 'providers.groups.lifecycleIdChangeTitle',
  lifecycleIdChangeHint: 'providers.groups.lifecycleIdChangeHint',
  lifecycleLoading: 'providers.groups.lifecycleLoading',
  lifecycleReady: 'providers.groups.lifecycleReady',
  lifecycleNotExecutable: 'providers.groups.lifecycleNotExecutable',
  lifecycleBlockers: 'providers.groups.lifecycleBlockers',
  lifecycleChoices: 'providers.groups.lifecycleChoices',
  lifecycleAutomatic: 'providers.groups.lifecycleAutomatic',
  lifecyclePreserved: 'providers.groups.lifecyclePreserved',
  lifecycleExecute: 'providers.groups.lifecycleExecute',
  lifecycleCancel: 'providers.groups.lifecycleCancel',
  lifecycleRefresh: 'providers.groups.lifecycleRefresh',
  lifecycleExecuted: 'providers.groups.lifecycleExecuted',
  lifecycleApplyFailed: 'providers.groups.lifecycleApplyFailed',
  lifecycleRestartPending: 'providers.groups.lifecycleRestartPending',
  lifecycleDanglingTarget: 'providers.groups.lifecycleDanglingTarget',
  lifecycleReferencedByAliases: 'providers.groups.lifecycleReferencedByAliases',
  lifecycleReferencedByRewrite: 'providers.groups.lifecycleReferencedByRewrite',

  // Error / empty / loading
  errorLoad: 'providers.groups.errorLoad',
  errorSave: 'providers.groups.errorSave',
  errorDelete: 'providers.groups.errorDelete',
  errorRefreshModels: 'providers.groups.errorRefreshModels',
  errorPing: 'providers.groups.errorPing',
  errorValidation: 'providers.groups.errorValidation',
  errorRequiredId: 'providers.groups.errorRequiredId',
  errorDuplicateId: 'providers.groups.errorDuplicateId',
  errorRequiredProtocol: 'providers.groups.errorRequiredProtocol',
  emptyStateTitle: 'providers.groups.emptyStateTitle',
  emptyStateHint: 'providers.groups.emptyStateHint',
  loadingStateTitle: 'providers.groups.loadingStateTitle',
  loadingStateHint: 'providers.groups.loadingStateHint',

  // Narrow-layout / a11y labels for containers
  layoutLabel: 'providers.groups.layoutLabel',
  listRegionLabel: 'providers.groups.listRegionLabel',
  detailRegionLabel: 'providers.groups.detailRegionLabel',
  sharedRegionLabel: 'providers.groups.sharedRegionLabel',
  aliasSelectorRegionLabel: 'providers.groups.aliasSelectorRegionLabel',
  narrowStackHint: 'providers.groups.narrowStackHint',
} as const

/** Union of every frozen `providers.groups.*` i18n key. */
export type ProviderGroupI18nKey = (typeof PROVIDER_GROUP_I18N_KEYS)[keyof typeof PROVIDER_GROUP_I18N_KEYS]

/** Ordered readonly list of all i18n keys (locale parity checks). */
export const PROVIDER_GROUP_I18N_KEY_LIST: readonly ProviderGroupI18nKey[] = Object.freeze(
  Object.values(PROVIDER_GROUP_I18N_KEYS),
)

// ---------------------------------------------------------------------------
// CSS classes - provider-group-* only
// ---------------------------------------------------------------------------

/**
 * Named map of every `provider-group-*` class Step 8 styles/pages may use.
 * styles.css must implement this set; pages must not invent extra group classes.
 */
export const PROVIDER_GROUP_CSS_CLASSES = {
  // Top-level layout / shared settings
  layout: 'provider-group-layout',
  layoutNarrow: 'provider-group-layout-narrow',
  sharedPanel: 'provider-group-shared-panel',
  sharedForm: 'provider-group-shared-form',
  sharedHeader: 'provider-group-shared-header',
  sharedBody: 'provider-group-shared-body',

  // Split: list + detail
  split: 'provider-group-split',
  splitNarrow: 'provider-group-split-narrow',
  list: 'provider-group-list',
  listHeader: 'provider-group-list-header',
  listBody: 'provider-group-list-body',
  listItem: 'provider-group-list-item',
  listItemActive: 'provider-group-list-item-active',
  listItemDisabled: 'provider-group-list-item-disabled',
  listItemMeta: 'provider-group-list-item-meta',
  listItemActions: 'provider-group-list-item-actions',
  listEmpty: 'provider-group-list-empty',
  listLoading: 'provider-group-list-loading',
  listError: 'provider-group-list-error',

  // Detail / form
  detail: 'provider-group-detail',
  detailHeader: 'provider-group-detail-header',
  detailBody: 'provider-group-detail-body',
  detailToolbar: 'provider-group-detail-toolbar',
  detailSection: 'provider-group-detail-section',
  formSection: 'provider-group-form-section',
  formSectionTitle: 'provider-group-form-section-title',
  detailEmpty: 'provider-group-detail-empty',
  form: 'provider-group-form',
  formGrid: 'provider-group-form-grid',
  formField: 'provider-group-form-field',
  formFieldFull: 'provider-group-form-field-full',
  formActions: 'provider-group-form-actions',
  formFieldset: 'provider-group-form-fieldset',
  formFieldsetLegend: 'provider-group-form-fieldset-legend',

  // Protocol / models
  protocolField: 'provider-group-protocol-field',
  modelsField: 'provider-group-models-field',
  modelsList: 'provider-group-models-list',
  modelsEmpty: 'provider-group-models-empty',
  modelsActions: 'provider-group-models-actions',
  modelsSource: 'provider-group-models-source',

  // Upstream keys three-state editor
  upstreamKeysFieldset: 'provider-group-upstream-keys-fieldset',
  upstreamKeysHeader: 'provider-group-upstream-keys-header',
  upstreamKeysSummary: 'provider-group-upstream-keys-summary',
  upstreamKeysSummaryMasked: 'provider-group-upstream-keys-summary-masked',
  upstreamKeysActions: 'provider-group-upstream-keys-actions',
  upstreamKeysEditList: 'provider-group-upstream-keys-edit-list',
  upstreamKeysEditRow: 'provider-group-upstream-keys-edit-row',
  upstreamKeysModeSaved: 'provider-group-upstream-keys-mode-saved',
  upstreamKeysModePending: 'provider-group-upstream-keys-mode-pending',
  upstreamKeysModeClear: 'provider-group-upstream-keys-mode-clear',
  upstreamKeysHint: 'provider-group-upstream-keys-hint',
  upstreamKeysMaskHint: 'provider-group-upstream-keys-mask-hint',

  // Status / badges / banners
  statusBadge: 'provider-group-status-badge',
  statusBadgeEnabled: 'provider-group-status-badge-enabled',
  statusBadgeDisabled: 'provider-group-status-badge-disabled',
  statusBanner: 'provider-group-status-banner',
  statusBannerError: 'provider-group-status-banner-error',
  statusBannerLoading: 'provider-group-status-banner-loading',
  statusBannerSuccess: 'provider-group-status-banner-success',
  emptyState: 'provider-group-empty-state',
  loadingState: 'provider-group-loading-state',
  errorState: 'provider-group-error-state',

  // Cards (provider list summary of groups)
  card: 'provider-group-card',
  cardMeta: 'provider-group-card-meta',
  cardFooter: 'provider-group-card-footer',
  cardCount: 'provider-group-card-count',

  // Alias three-level selector
  aliasSelector: 'provider-group-alias-selector',
  aliasSelectorRow: 'provider-group-alias-selector-row',
  aliasSelectorProvider: 'provider-group-alias-selector-provider',
  aliasSelectorGroup: 'provider-group-alias-selector-group',
  aliasSelectorModel: 'provider-group-alias-selector-model',
  aliasSelectorHint: 'provider-group-alias-selector-hint',
  aliasTargetChip: 'provider-group-alias-target-chip',
  aliasTargetChipUnavailable: 'provider-group-alias-target-chip-unavailable',

  // Lifecycle planning panel
  lifecyclePanel: 'provider-group-lifecycle-panel',
  lifecycleHeader: 'provider-group-lifecycle-header',
  lifecycleSection: 'provider-group-lifecycle-section',
  lifecycleBlockers: 'provider-group-lifecycle-blockers',
  lifecycleChoices: 'provider-group-lifecycle-choices',
  lifecycleActions: 'provider-group-lifecycle-actions',
  lifecycleLoading: 'provider-group-lifecycle-loading',

  // Narrow-screen containers
  narrowStack: 'provider-group-narrow-stack',
  narrowSplit: 'provider-group-narrow-split',
  narrowToolbar: 'provider-group-narrow-toolbar',
  narrowScroll: 'provider-group-narrow-scroll',
  narrowFieldStack: 'provider-group-narrow-field-stack',
  narrowActions: 'provider-group-narrow-actions',
} as const

/** Union of every frozen `provider-group-*` CSS class. */
export type ProviderGroupCssClass = (typeof PROVIDER_GROUP_CSS_CLASSES)[keyof typeof PROVIDER_GROUP_CSS_CLASSES]

/** Ordered readonly list of all CSS classes (style parity checks). */
export const PROVIDER_GROUP_CSS_CLASS_LIST: readonly ProviderGroupCssClass[] = Object.freeze(
  Object.values(PROVIDER_GROUP_CSS_CLASSES),
)

// ---------------------------------------------------------------------------
// Guards (optional runtime helpers for Step 8 / tests)
// ---------------------------------------------------------------------------

const i18nKeySet: ReadonlySet<string> = new Set(PROVIDER_GROUP_I18N_KEY_LIST)
const cssClassSet: ReadonlySet<string> = new Set(PROVIDER_GROUP_CSS_CLASS_LIST)

/** Type predicate: value is a frozen providers.groups.* key. */
export function isProviderGroupI18nKey(value: string): value is ProviderGroupI18nKey {
  return i18nKeySet.has(value)
}

/** Type predicate: value is a frozen provider-group-* class. */
export function isProviderGroupCssClass(value: string): value is ProviderGroupCssClass {
  return cssClassSet.has(value)
}
