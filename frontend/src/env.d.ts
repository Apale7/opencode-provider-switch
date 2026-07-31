/// <reference types="vite/client" />

declare module '*.png' {
  const src: string
  export default src
}

declare module '*.svg' {
  const src: string
  export default src
}

declare global {
  interface Window {
    go?: {
      desktop?: {
        App?: {
          Meta: () => Promise<Record<string, string>>
          OpenExternalURL: (url: string) => Promise<void>
          Overview: () => Promise<import('./types').Overview>
          ExportConfig: () => Promise<import('./types').ConfigExportView>
          ImportConfig: (input: import('./types').ConfigImportInput) => Promise<import('./types').ConfigImportResult>
          Providers: () => Promise<import('./types').ProviderView[]>
          Aliases: () => Promise<import('./types').AliasView[]>
          SaveProvider: (input: import('./types').ProviderUpsertInput) => Promise<import('./types').ProviderSaveResult>
          RefreshProviderModels: (input: import('./types').ProviderRefreshModelsInput) => Promise<import('./types').ProviderSaveResult>
          PingProviderBaseURL: (input: import('./types').ProviderPingInput) => Promise<import('./types').ProviderPingResult>
          SetProviderState: (input: import('./types').ProviderStateInput) => Promise<import('./types').ProviderView>
          DeleteProvider: (id: string) => Promise<void>
          GetProviderPriority: () => Promise<import('./types').ProviderPriorityResult>
          SetProviderPriority: (input: import('./types').ProviderPriorityInput) => Promise<import('./types').ProviderPriorityResult>
          GetAutoAliasSettings: () => Promise<import('./types').AutoAliasSettingsResult>
          SetAutoAliasSettings: (input: import('./types').AutoAliasSettingsInput) => Promise<import('./types').AutoAliasSettingsResult>
          UpgradeAutoAlias: (input: import('./types').AliasLockInput) => Promise<import('./types').AliasView>
          ImportProviders: (input: import('./types').ProviderImportInput) => Promise<import('./types').ProviderImportResult>
          SaveAlias: (input: import('./types').AliasUpsertInput) => Promise<import('./types').AliasView>
          DeleteAlias: (alias: string) => Promise<void>
          BindTarget: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          SetTargetState: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          UnbindTarget: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          ReorderTargets: (input: import('./types').AliasTargetReorderInput) => Promise<import('./types').AliasView>
          ResetTargetOrder: (input: import('./types').AliasLockInput) => Promise<import('./types').AliasView>
          RewriteRules: () => Promise<import('./types').RequestRewriteRuleView[]>
          SaveRewriteRule: (input: import('./types').RequestRewriteRuleInput) => Promise<import('./types').RequestRewriteRuleView>
          SetRewriteRuleState: (input: import('./types').RequestRewriteRuleStateInput) => Promise<import('./types').RequestRewriteRuleView>
          DeleteRewriteRule: (input: import('./types').RequestRewriteRuleRemoveInput) => Promise<import('./types').RequestRewriteRuleRemoveResult>
          ReorderRewriteRules: (input: import('./types').RequestRewriteRuleReorderInput) => Promise<import('./types').RequestRewriteRuleReorderResult>
          DoctorRun: () => Promise<import('./types').DoctorRunResult>
          ProxyStatus: () => Promise<import('./types').ProxyStatusView>
          ProxySettings: () => Promise<import('./types').ProxySettingsView>
          RequestTraces: (limit: number) => Promise<import('./types').RequestTrace[]>
          TraceList: (input: import('./types').RequestTraceListInput) => Promise<import('./types').RequestTraceListResult>
          TraceDetail: (id: number) => Promise<import('./types').RequestTrace>
          ProviderHealth: (input: import('./types').ProviderHealthInput) => Promise<import('./types').ProviderHealthResult>
          StartProxy: () => Promise<import('./types').ProxyStatusView>
          SaveProxySettings: (input: import('./types').ProxySettingsView) => Promise<import('./types').ProxySettingsSaveResult>
          StopProxy: () => Promise<import('./types').ProxyStatusView>
          DesktopPrefs: () => Promise<import('./types').DesktopPrefsView>
          SavePrefs: (input: import('./types').DesktopPrefsView) => Promise<import('./types').DesktopPrefsSaveResult>
          PreviewSync: (input: import('./types').SyncInput) => Promise<import('./types').SyncPreview>
          ApplySync: (input: import('./types').SyncInput) => Promise<import('./types').SyncResult>
          /** Lifecycle/revision methods return the same envelope as HTTP (artifact 06). */
          GetConfigRevision: () => Promise<import('./types').ApiEnvelope<import('./types').ConfigRevisionView>>
          PreviewLifecycle: (
            input: import('./types').LifecyclePreviewInput,
          ) => Promise<import('./types').ApiEnvelope<import('./types').LifecyclePlanView>>
          ExecuteLifecycle: (
            input: import('./types').LifecycleExecuteInput,
          ) => Promise<import('./types').ApiEnvelope<import('./types').LifecycleExecuteResult>>
        }
      }
    }
  }
}

export {}
