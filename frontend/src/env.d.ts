/// <reference types="vite/client" />

declare global {
  interface Window {
    go?: {
      desktop?: {
        App?: {
          Meta: () => Promise<Record<string, string>>
          Overview: () => Promise<import('./types').Overview>
          Providers: () => Promise<import('./types').ProviderView[]>
          Aliases: () => Promise<import('./types').AliasView[]>
          SaveProvider: (input: import('./types').ProviderUpsertInput) => Promise<import('./types').ProviderSaveResult>
          SetProviderState: (input: import('./types').ProviderStateInput) => Promise<import('./types').ProviderView>
          DeleteProvider: (id: string) => Promise<void>
          ImportProviders: (input: import('./types').ProviderImportInput) => Promise<import('./types').ProviderImportResult>
          SaveAlias: (input: import('./types').AliasUpsertInput) => Promise<import('./types').AliasView>
          DeleteAlias: (alias: string) => Promise<void>
          BindTarget: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          SetTargetState: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          UnbindTarget: (input: import('./types').AliasTargetInput) => Promise<import('./types').AliasView>
          DoctorRun: () => Promise<import('./types').DoctorRunResult>
          ProxyStatus: () => Promise<import('./types').ProxyStatusView>
          StartProxy: () => Promise<import('./types').ProxyStatusView>
          StopProxy: () => Promise<import('./types').ProxyStatusView>
          DesktopPrefs: () => Promise<import('./types').DesktopPrefsView>
          SavePrefs: (input: import('./types').DesktopPrefsView) => Promise<import('./types').DesktopPrefsSaveResult>
          PreviewSync: (input: import('./types').SyncInput) => Promise<import('./types').SyncPreview>
          ApplySync: (input: import('./types').SyncInput) => Promise<import('./types').SyncResult>
        }
      }
    }
  }
}

export {}
