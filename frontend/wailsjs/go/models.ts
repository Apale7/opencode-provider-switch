export namespace app {
	
	export class TransportOutcome {
	    code: string;
	    params: Record<string, any>;
	    retryable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TransportOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.params = source["params"];
	        this.retryable = source["retryable"];
	    }
	}
	export class APIEnvelope {
	    ok: boolean;
	    data?: any;
	    error?: string;
	    outcome: TransportOutcome;
	
	    static createFrom(source: any = {}) {
	        return new APIEnvelope(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.data = source["data"];
	        this.error = source["error"];
	        this.outcome = this.convertValues(source["outcome"], TransportOutcome);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AliasLockInput {
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new AliasLockInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	    }
	}
	export class DiffSummaryView {
	    total: number;
	    new: number;
	    changed: number;
	    unchanged: number;
	    conflict: number;
	    failed: number;
	
	    static createFrom(source: any = {}) {
	        return new DiffSummaryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.new = source["new"];
	        this.changed = source["changed"];
	        this.unchanged = source["unchanged"];
	        this.conflict = source["conflict"];
	        this.failed = source["failed"];
	    }
	}
	export class SyncDiffEntryView {
	    path: string;
	    userValue?: any;
	    proposedValue?: any;
	    status: string;
	    conflictNote?: string;
	    autoDetected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncDiffEntryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.userValue = source["userValue"];
	        this.proposedValue = source["proposedValue"];
	        this.status = source["status"];
	        this.conflictNote = source["conflictNote"];
	        this.autoDetected = source["autoDetected"];
	    }
	}
	export class AliasSyncPreviewView {
	    aliasName: string;
	    protocol: string;
	    providerKey: string;
	    entries: SyncDiffEntryView[];
	    summary: DiffSummaryView;
	
	    static createFrom(source: any = {}) {
	        return new AliasSyncPreviewView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aliasName = source["aliasName"];
	        this.protocol = source["protocol"];
	        this.providerKey = source["providerKey"];
	        this.entries = this.convertValues(source["entries"], SyncDiffEntryView);
	        this.summary = this.convertValues(source["summary"], DiffSummaryView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AliasTargetInput {
	    alias: string;
	    provider: string;
	    group: string;
	    model: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.provider = source["provider"];
	        this.group = source["group"];
	        this.model = source["model"];
	        this.disabled = source["disabled"];
	    }
	}
	export class AliasTargetRefInput {
	    provider: string;
	    group: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetRefInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.group = source["group"];
	        this.model = source["model"];
	    }
	}
	export class AliasTargetReorderInput {
	    alias: string;
	    targets: AliasTargetRefInput[];
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetReorderInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.targets = this.convertValues(source["targets"], AliasTargetRefInput);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AliasTargetView {
	    provider: string;
	    group: string;
	    model: string;
	    enabled: boolean;
	    autoGenerated: boolean;
	    available: boolean;
	    reason?: string;
	    code?: string;
	    allowedActions?: string[];
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.group = source["group"];
	        this.model = source["model"];
	        this.enabled = source["enabled"];
	        this.autoGenerated = source["autoGenerated"];
	        this.available = source["available"];
	        this.reason = source["reason"];
	        this.code = source["code"];
	        this.allowedActions = source["allowedActions"];
	    }
	}
	export class AliasUpsertInput {
	    alias: string;
	    displayName?: string;
	    protocol: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AliasUpsertInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.displayName = source["displayName"];
	        this.protocol = source["protocol"];
	        this.disabled = source["disabled"];
	    }
	}
	export class AliasView {
	    alias: string;
	    displayName?: string;
	    protocol: string;
	    enabled: boolean;
	    targetCount: number;
	    availableTargetCount: number;
	    targets: AliasTargetView[];
	    autoGenerated: boolean;
	    locked: boolean;
	    targetOrderMode: string;
	    catalogMatch: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AliasView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.displayName = source["displayName"];
	        this.protocol = source["protocol"];
	        this.enabled = source["enabled"];
	        this.targetCount = source["targetCount"];
	        this.availableTargetCount = source["availableTargetCount"];
	        this.targets = this.convertValues(source["targets"], AliasTargetView);
	        this.autoGenerated = source["autoGenerated"];
	        this.locked = source["locked"];
	        this.targetOrderMode = source["targetOrderMode"];
	        this.catalogMatch = source["catalogMatch"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class AutoAliasSettingsInput {
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AutoAliasSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	    }
	}
	export class AutoAliasSettingsResult {
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AutoAliasSettingsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	    }
	}
	export class ConfigExportView {
	    configPath: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigExportView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configPath = source["configPath"];
	        this.content = source["content"];
	    }
	}
	export class ConfigImportInput {
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigImportInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	    }
	}
	export class ConfigImportResult {
	    configPath: string;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configPath = source["configPath"];
	        this.warnings = source["warnings"];
	    }
	}
	export class DesktopPrefsInput {
	    launchAtLogin: boolean;
	    autoStartProxy: boolean;
	    minimizeToTray: boolean;
	    notifications: boolean;
	    theme: string;
	    language: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopPrefsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.launchAtLogin = source["launchAtLogin"];
	        this.autoStartProxy = source["autoStartProxy"];
	        this.minimizeToTray = source["minimizeToTray"];
	        this.notifications = source["notifications"];
	        this.theme = source["theme"];
	        this.language = source["language"];
	    }
	}
	export class DesktopPrefsView {
	    launchAtLogin: boolean;
	    autoStartProxy: boolean;
	    minimizeToTray: boolean;
	    notifications: boolean;
	    theme: string;
	    language: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopPrefsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.launchAtLogin = source["launchAtLogin"];
	        this.autoStartProxy = source["autoStartProxy"];
	        this.minimizeToTray = source["minimizeToTray"];
	        this.notifications = source["notifications"];
	        this.theme = source["theme"];
	        this.language = source["language"];
	    }
	}
	export class DesktopPrefsSaveResult {
	    prefs: DesktopPrefsView;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DesktopPrefsSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prefs = this.convertValues(source["prefs"], DesktopPrefsView);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class DiagnosticEntityRef {
	    kind: string;
	    key: string;
	    path?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticEntityRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.key = source["key"];
	        this.path = source["path"];
	    }
	}
	export class DiagnosticTargetRef {
	    kind: string;
	    key: string;
	    path?: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticTargetRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.key = source["key"];
	        this.path = source["path"];
	    }
	}
	
	export class DoctorIssue {
	    schemaVersion?: number;
	    code: string;
	    severity: string;
	    path?: string;
	    source?: DiagnosticEntityRef;
	    target?: DiagnosticTargetRef;
	    reason?: string;
	    allowedActions?: string[];
	    params?: Record<string, any>;
	    message?: string;
	    protocol?: string;
	    providerKey?: string;
	    alias?: string;
	    directory?: string;
	    expected?: string;
	    actual?: string;
	    actionHint?: string;
	    autoFixAvailable?: boolean;
	    details?: string[];
	    relatedFields?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DoctorIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemaVersion = source["schemaVersion"];
	        this.code = source["code"];
	        this.severity = source["severity"];
	        this.path = source["path"];
	        this.source = this.convertValues(source["source"], DiagnosticEntityRef);
	        this.target = this.convertValues(source["target"], DiagnosticTargetRef);
	        this.reason = source["reason"];
	        this.allowedActions = source["allowedActions"];
	        this.params = source["params"];
	        this.message = source["message"];
	        this.protocol = source["protocol"];
	        this.providerKey = source["providerKey"];
	        this.alias = source["alias"];
	        this.directory = source["directory"];
	        this.expected = source["expected"];
	        this.actual = source["actual"];
	        this.actionHint = source["actionHint"];
	        this.autoFixAvailable = source["autoFixAvailable"];
	        this.details = source["details"];
	        this.relatedFields = source["relatedFields"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenCodeReconciliationSummary {
	    availableAliases?: string[];
	    missingProviders?: string[];
	    invalidDefaultModels?: string[];
	    catalogMismatches?: string[];
	    fileOnlyProviders?: string[];
	    runtimeOnlyProviders?: string[];
	    runtimeReachable: boolean;
	    fileSnapshotAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeReconciliationSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.availableAliases = source["availableAliases"];
	        this.missingProviders = source["missingProviders"];
	        this.invalidDefaultModels = source["invalidDefaultModels"];
	        this.catalogMismatches = source["catalogMismatches"];
	        this.fileOnlyProviders = source["fileOnlyProviders"];
	        this.runtimeOnlyProviders = source["runtimeOnlyProviders"];
	        this.runtimeReachable = source["runtimeReachable"];
	        this.fileSnapshotAvailable = source["fileSnapshotAvailable"];
	    }
	}
	export class OpenCodeRuntimeModelSnapshot {
	    id: string;
	    name?: string;
	    providerId?: string;
	    providerNpm?: string;
	    rawJson?: string;
	    extraFieldKeys?: string[];
	    optionKeys?: string[];
	    experimental?: boolean;
	    reasoning?: boolean;
	    toolCall?: boolean;
	    temperature?: boolean;
	    attachment?: boolean;
	    contextLimit?: number;
	    outputLimit?: number;
	    releaseDate?: string;
	    status?: string;
	    inputModalities?: string[];
	    outputModalities?: string[];
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeRuntimeModelSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.providerId = source["providerId"];
	        this.providerNpm = source["providerNpm"];
	        this.rawJson = source["rawJson"];
	        this.extraFieldKeys = source["extraFieldKeys"];
	        this.optionKeys = source["optionKeys"];
	        this.experimental = source["experimental"];
	        this.reasoning = source["reasoning"];
	        this.toolCall = source["toolCall"];
	        this.temperature = source["temperature"];
	        this.attachment = source["attachment"];
	        this.contextLimit = source["contextLimit"];
	        this.outputLimit = source["outputLimit"];
	        this.releaseDate = source["releaseDate"];
	        this.status = source["status"];
	        this.inputModalities = source["inputModalities"];
	        this.outputModalities = source["outputModalities"];
	    }
	}
	export class OpenCodeRuntimeProviderSnapshot {
	    id: string;
	    name?: string;
	    api?: string;
	    npm?: string;
	    env?: string[];
	    modelIds?: string[];
	    models?: OpenCodeRuntimeModelSnapshot[];
	    extraFieldKeys?: string[];
	    rawJson?: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeRuntimeProviderSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.api = source["api"];
	        this.npm = source["npm"];
	        this.env = source["env"];
	        this.modelIds = source["modelIds"];
	        this.models = this.convertValues(source["models"], OpenCodeRuntimeModelSnapshot);
	        this.extraFieldKeys = source["extraFieldKeys"];
	        this.rawJson = source["rawJson"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenCodeRuntimeSnapshot {
	    baseUrl: string;
	    directory?: string;
	    reachable: boolean;
	    configLoaded: boolean;
	    providersLoaded: boolean;
	    defaultModel?: string;
	    smallModel?: string;
	    providerKeys?: string[];
	    defaultProviderModels?: Record<string, string>;
	    providers?: OpenCodeRuntimeProviderSnapshot[];
	    errorCode?: string;
	    errorMessage?: string;
	    httpStatus?: number;
	    rawConfigJson?: string;
	    rawProvidersJson?: string;
	    configExtraFieldKeys?: string[];
	    providerExtraFieldMap?: Record<string, Array<string>>;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeRuntimeSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.directory = source["directory"];
	        this.reachable = source["reachable"];
	        this.configLoaded = source["configLoaded"];
	        this.providersLoaded = source["providersLoaded"];
	        this.defaultModel = source["defaultModel"];
	        this.smallModel = source["smallModel"];
	        this.providerKeys = source["providerKeys"];
	        this.defaultProviderModels = source["defaultProviderModels"];
	        this.providers = this.convertValues(source["providers"], OpenCodeRuntimeProviderSnapshot);
	        this.errorCode = source["errorCode"];
	        this.errorMessage = source["errorMessage"];
	        this.httpStatus = source["httpStatus"];
	        this.rawConfigJson = source["rawConfigJson"];
	        this.rawProvidersJson = source["rawProvidersJson"];
	        this.configExtraFieldKeys = source["configExtraFieldKeys"];
	        this.providerExtraFieldMap = source["providerExtraFieldMap"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenCodeProviderSnapshot {
	    key: string;
	    name?: string;
	    npm?: string;
	    protocol?: string;
	    baseUrl?: string;
	    modelAliases?: string[];
	    missingFields?: string[];
	    unknownFieldKeys?: string[];
	    rawJsonFragment?: string;
	    contractConfigured: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeProviderSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.npm = source["npm"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.modelAliases = source["modelAliases"];
	        this.missingFields = source["missingFields"];
	        this.unknownFieldKeys = source["unknownFieldKeys"];
	        this.rawJsonFragment = source["rawJsonFragment"];
	        this.contractConfigured = source["contractConfigured"];
	    }
	}
	export class OpenCodeFileSnapshot {
	    targetPath: string;
	    exists: boolean;
	    schema?: string;
	    defaultModel?: string;
	    smallModel?: string;
	    providerKeys?: string[];
	    expectedProtocols?: string[];
	    syncedProviders?: OpenCodeProviderSnapshot[];
	    unknownTopLevelKeys?: string[];
	    parseError?: string;
	    defaultModelRoutable: boolean;
	    smallModelRoutable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpenCodeFileSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.exists = source["exists"];
	        this.schema = source["schema"];
	        this.defaultModel = source["defaultModel"];
	        this.smallModel = source["smallModel"];
	        this.providerKeys = source["providerKeys"];
	        this.expectedProtocols = source["expectedProtocols"];
	        this.syncedProviders = this.convertValues(source["syncedProviders"], OpenCodeProviderSnapshot);
	        this.unknownTopLevelKeys = source["unknownTopLevelKeys"];
	        this.parseError = source["parseError"];
	        this.defaultModelRoutable = source["defaultModelRoutable"];
	        this.smallModelRoutable = source["smallModelRoutable"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DoctorReport {
	    ok: boolean;
	    issues: DoctorIssue[];
	    syncProtocols: string[];
	    configPath: string;
	    providerCount: number;
	    aliasCount: number;
	    proxyBindAddress: string;
	    openCodeTargetPath: string;
	    openCodeTargetFound: boolean;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	    fileSnapshot: OpenCodeFileSnapshot;
	    runtimeSnapshot: OpenCodeRuntimeSnapshot;
	    summary: OpenCodeReconciliationSummary;
	
	    static createFrom(source: any = {}) {
	        return new DoctorReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.issues = this.convertValues(source["issues"], DoctorIssue);
	        this.syncProtocols = source["syncProtocols"];
	        this.configPath = source["configPath"];
	        this.providerCount = source["providerCount"];
	        this.aliasCount = source["aliasCount"];
	        this.proxyBindAddress = source["proxyBindAddress"];
	        this.openCodeTargetPath = source["openCodeTargetPath"];
	        this.openCodeTargetFound = source["openCodeTargetFound"];
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	        this.fileSnapshot = this.convertValues(source["fileSnapshot"], OpenCodeFileSnapshot);
	        this.runtimeSnapshot = this.convertValues(source["runtimeSnapshot"], OpenCodeRuntimeSnapshot);
	        this.summary = this.convertValues(source["summary"], OpenCodeReconciliationSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DoctorRunResult {
	    report: DoctorReport;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DoctorRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.report = this.convertValues(source["report"], DoctorReport);
	        this.error = source["error"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LifecycleExecuteInput {
	    revision: string;
	    planToken: string;
	    operation: lifecycle.Operation;
	    selections: lifecycle.Selection[];
	    externalOpenCode?: lifecycle.ExternalRefs;
	    preparationToken?: string;
	
	    static createFrom(source: any = {}) {
	        return new LifecycleExecuteInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.planToken = source["planToken"];
	        this.operation = this.convertValues(source["operation"], lifecycle.Operation);
	        this.selections = this.convertValues(source["selections"], lifecycle.Selection);
	        this.externalOpenCode = this.convertValues(source["externalOpenCode"], lifecycle.ExternalRefs);
	        this.preparationToken = source["preparationToken"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LifecyclePreviewInput {
	    revision: string;
	    operation: lifecycle.Operation;
	    selections: lifecycle.Selection[];
	    preparationToken?: string;
	    externalOpenCode?: lifecycle.ExternalRefs;
	
	    static createFrom(source: any = {}) {
	        return new LifecyclePreviewInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.revision = source["revision"];
	        this.operation = this.convertValues(source["operation"], lifecycle.Operation);
	        this.selections = this.convertValues(source["selections"], lifecycle.Selection);
	        this.preparationToken = source["preparationToken"];
	        this.externalOpenCode = this.convertValues(source["externalOpenCode"], lifecycle.ExternalRefs);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	
	
	export class ProxyStatusView {
	    running: boolean;
	    bindAddress: string;
	    startedAt?: string;
	    lastError?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxyStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.bindAddress = source["bindAddress"];
	        this.startedAt = source["startedAt"];
	        this.lastError = source["lastError"];
	    }
	}
	export class TraceStoreStatus {
	    mode: string;
	    path?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new TraceStoreStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.path = source["path"];
	        this.error = source["error"];
	    }
	}
	export class Overview {
	    configPath: string;
	    providerCount: number;
	    aliasCount: number;
	    availableAliases: string[];
	    traceStore: TraceStoreStatus;
	    proxy: ProxyStatusView;
	    desktop: DesktopPrefsView;
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.configPath = source["configPath"];
	        this.providerCount = source["providerCount"];
	        this.aliasCount = source["aliasCount"];
	        this.availableAliases = source["availableAliases"];
	        this.traceStore = this.convertValues(source["traceStore"], TraceStoreStatus);
	        this.proxy = this.convertValues(source["proxy"], ProxyStatusView);
	        this.desktop = this.convertValues(source["desktop"], DesktopPrefsView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderGroupInput {
	    id: string;
	    name?: string;
	    nameChanged?: boolean;
	    protocol: string;
	    apiKeysChanged: boolean;
	    apiKeys?: string[];
	    models?: string[];
	    disabled?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.nameChanged = source["nameChanged"];
	        this.protocol = source["protocol"];
	        this.apiKeysChanged = source["apiKeysChanged"];
	        this.apiKeys = source["apiKeys"];
	        this.models = source["models"];
	        this.disabled = source["disabled"];
	    }
	}
	export class ProviderGroupCreateInput {
	    providerId: string;
	    group: ProviderGroupInput;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupCreateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.group = this.convertValues(source["group"], ProviderGroupInput);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderGroupDeleteInput {
	    providerId: string;
	    groupId: string;
	    selections?: lifecycle.Selection[];
	    expectedRevision?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupDeleteInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.groupId = source["groupId"];
	        this.selections = this.convertValues(source["selections"], lifecycle.Selection);
	        this.expectedRevision = source["expectedRevision"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ProviderGroupPingInput {
	    providerId: string;
	    groupId: string;
	    baseUrl?: string;
	    protocol?: string;
	    apiKey?: string;
	    apiKeys?: string[];
	    headers?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupPingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.groupId = source["groupId"];
	        this.baseUrl = source["baseUrl"];
	        this.protocol = source["protocol"];
	        this.apiKey = source["apiKey"];
	        this.apiKeys = source["apiKeys"];
	        this.headers = source["headers"];
	    }
	}
	export class ProviderGroupRefreshModelsInput {
	    providerId: string;
	    groupId: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupRefreshModelsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.groupId = source["groupId"];
	    }
	}
	export class ProviderGroupSelectorInput {
	    provider: string;
	    group: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupSelectorInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.group = source["group"];
	    }
	}
	export class ProviderGroupSelectorView {
	    provider: string;
	    group: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupSelectorView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.group = source["group"];
	    }
	}
	export class ProviderGroupUpdateInput {
	    providerId: string;
	    groupId: string;
	    group: ProviderGroupInput;
	    selections?: lifecycle.Selection[];
	    expectedRevision?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupUpdateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.groupId = source["groupId"];
	        this.group = this.convertValues(source["group"], ProviderGroupInput);
	        this.selections = this.convertValues(source["selections"], lifecycle.Selection);
	        this.expectedRevision = source["expectedRevision"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderGroupView {
	    id: string;
	    name?: string;
	    protocol: string;
	    apiKeyCount: number;
	    apiKeysMasked?: string[];
	    models?: string[];
	    modelsSource?: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderGroupView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.apiKeyCount = source["apiKeyCount"];
	        this.apiKeysMasked = source["apiKeysMasked"];
	        this.models = source["models"];
	        this.modelsSource = source["modelsSource"];
	        this.disabled = source["disabled"];
	    }
	}
	export class ProviderHealthAlias {
	    alias: string;
	    model?: string;
	    role: string;
	    targetIndex: number;
	    attempts: number;
	    success: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthAlias(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.model = source["model"];
	        this.role = source["role"];
	        this.targetIndex = source["targetIndex"];
	        this.attempts = source["attempts"];
	        this.success = source["success"];
	    }
	}
	export class ProviderHealthInput {
	    aliases?: string[];
	    providers?: string[];
	    startedFrom?: string;
	    startedTo?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aliases = source["aliases"];
	        this.providers = source["providers"];
	        this.startedFrom = source["startedFrom"];
	        this.startedTo = source["startedTo"];
	    }
	}
	export class ProviderHealthView {
	    provider: string;
	    group?: string;
	    name?: string;
	    protocol?: string;
	    role: string;
	    configured: boolean;
	    disabled?: boolean;
	    sampleLevel: string;
	    requestCount: number;
	    attemptCount: number;
	    primaryAttempts: number;
	    backupAttempts: number;
	    success: number;
	    finalSuccess: number;
	    terminalFailures: number;
	    retryableFailures: number;
	    skipped: number;
	    rateLimited: number;
	    upstream5xx: number;
	    upstream4xx: number;
	    timeouts: number;
	    transportErrors: number;
	    streamErrors: number;
	    emptyResponses: number;
	    otherFailures: number;
	    failoverInvolved: number;
	    inputTokens: number;
	    outputTokens: number;
	    totalTokens: number;
	    cacheReadTokens: number;
	    cacheHitRate: number;
	    firstByteP50Ms?: number;
	    firstByteP95Ms?: number;
	    durationP50Ms?: number;
	    durationP95Ms?: number;
	    observedSuccessRate: number;
	    retryableFailureRate: number;
	    aliases?: ProviderHealthAlias[];
	    groups?: ProviderHealthView[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.group = source["group"];
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.role = source["role"];
	        this.configured = source["configured"];
	        this.disabled = source["disabled"];
	        this.sampleLevel = source["sampleLevel"];
	        this.requestCount = source["requestCount"];
	        this.attemptCount = source["attemptCount"];
	        this.primaryAttempts = source["primaryAttempts"];
	        this.backupAttempts = source["backupAttempts"];
	        this.success = source["success"];
	        this.finalSuccess = source["finalSuccess"];
	        this.terminalFailures = source["terminalFailures"];
	        this.retryableFailures = source["retryableFailures"];
	        this.skipped = source["skipped"];
	        this.rateLimited = source["rateLimited"];
	        this.upstream5xx = source["upstream5xx"];
	        this.upstream4xx = source["upstream4xx"];
	        this.timeouts = source["timeouts"];
	        this.transportErrors = source["transportErrors"];
	        this.streamErrors = source["streamErrors"];
	        this.emptyResponses = source["emptyResponses"];
	        this.otherFailures = source["otherFailures"];
	        this.failoverInvolved = source["failoverInvolved"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.totalTokens = source["totalTokens"];
	        this.cacheReadTokens = source["cacheReadTokens"];
	        this.cacheHitRate = source["cacheHitRate"];
	        this.firstByteP50Ms = source["firstByteP50Ms"];
	        this.firstByteP95Ms = source["firstByteP95Ms"];
	        this.durationP50Ms = source["durationP50Ms"];
	        this.durationP95Ms = source["durationP95Ms"];
	        this.observedSuccessRate = source["observedSuccessRate"];
	        this.retryableFailureRate = source["retryableFailureRate"];
	        this.aliases = this.convertValues(source["aliases"], ProviderHealthAlias);
	        this.groups = this.convertValues(source["groups"], ProviderHealthView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderHealthSummary {
	    requestCount: number;
	    attemptCount: number;
	    success: number;
	    failed: number;
	    failover: number;
	    retryableFailures: number;
	    rateLimited: number;
	    upstream5xx: number;
	    timeouts: number;
	    transportErrors: number;
	    streamErrors: number;
	    inputTokens: number;
	    outputTokens: number;
	    totalTokens: number;
	    cacheReadTokens: number;
	    cacheHitRate: number;
	    firstByteP50Ms?: number;
	    firstByteP95Ms?: number;
	    durationP50Ms?: number;
	    durationP95Ms?: number;
	    sampledProviders: number;
	    lowSampleProviders: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestCount = source["requestCount"];
	        this.attemptCount = source["attemptCount"];
	        this.success = source["success"];
	        this.failed = source["failed"];
	        this.failover = source["failover"];
	        this.retryableFailures = source["retryableFailures"];
	        this.rateLimited = source["rateLimited"];
	        this.upstream5xx = source["upstream5xx"];
	        this.timeouts = source["timeouts"];
	        this.transportErrors = source["transportErrors"];
	        this.streamErrors = source["streamErrors"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.totalTokens = source["totalTokens"];
	        this.cacheReadTokens = source["cacheReadTokens"];
	        this.cacheHitRate = source["cacheHitRate"];
	        this.firstByteP50Ms = source["firstByteP50Ms"];
	        this.firstByteP95Ms = source["firstByteP95Ms"];
	        this.durationP50Ms = source["durationP50Ms"];
	        this.durationP95Ms = source["durationP95Ms"];
	        this.sampledProviders = source["sampledProviders"];
	        this.lowSampleProviders = source["lowSampleProviders"];
	    }
	}
	export class ProviderHealthResult {
	    summary: ProviderHealthSummary;
	    providers: ProviderHealthView[];
	    availableAliases?: string[];
	    availableProviders?: string[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.summary = this.convertValues(source["summary"], ProviderHealthSummary);
	        this.providers = this.convertValues(source["providers"], ProviderHealthView);
	        this.availableAliases = source["availableAliases"];
	        this.availableProviders = source["availableProviders"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class ProviderImportInput {
	    sourcePath?: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderImportInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.overwrite = source["overwrite"];
	    }
	}
	export class ProviderView {
	    id: string;
	    name?: string;
	    baseUrl: string;
	    baseUrls?: string[];
	    baseUrlStrategy: string;
	    headers?: Record<string, string>;
	    disabled: boolean;
	    autoAliasEnabled: boolean;
	    groups?: ProviderGroupView[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.baseUrls = source["baseUrls"];
	        this.baseUrlStrategy = source["baseUrlStrategy"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	        this.autoAliasEnabled = source["autoAliasEnabled"];
	        this.groups = this.convertValues(source["groups"], ProviderGroupView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderImportResult {
	    sourcePath: string;
	    imported: number;
	    skipped: number;
	    warnings?: string[];
	    providers?: ProviderView[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.warnings = source["warnings"];
	        this.providers = this.convertValues(source["providers"], ProviderView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderPingInput {
	    id?: string;
	    group?: string;
	    protocol?: string;
	    baseUrl: string;
	    apiKey?: string;
	    apiKeys?: string[];
	    headers?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ProviderPingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.group = source["group"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.apiKeys = source["apiKeys"];
	        this.headers = source["headers"];
	    }
	}
	export class ProviderPingResult {
	    id: string;
	    baseUrl: string;
	    latencyMs: number;
	    reachable: boolean;
	    statusCode?: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderPingResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.baseUrl = source["baseUrl"];
	        this.latencyMs = source["latencyMs"];
	        this.reachable = source["reachable"];
	        this.statusCode = source["statusCode"];
	        this.error = source["error"];
	    }
	}
	export class ProviderPriorityInput {
	    orderedIds: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderPriorityInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.orderedIds = source["orderedIds"];
	    }
	}
	export class ProviderPriorityResult {
	    orderedIds: string[];
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderPriorityResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.orderedIds = source["orderedIds"];
	        this.warnings = source["warnings"];
	    }
	}
	export class ProviderRefreshModelsInput {
	    id: string;
	    group?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderRefreshModelsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.group = source["group"];
	    }
	}
	export class ProviderSaveResult {
	    provider: ProviderView;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = this.convertValues(source["provider"], ProviderView);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProviderStateInput {
	    id: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderStateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.disabled = source["disabled"];
	    }
	}
	export class ProviderUpsertInput {
	    id: string;
	    name?: string;
	    baseUrl: string;
	    baseUrls?: string[];
	    baseUrlStrategy: string;
	    headers?: Record<string, string>;
	    disabled: boolean;
	    clearHeaders: boolean;
	    autoAliasEnabled?: boolean;
	    defaultGroup?: ProviderGroupInput;
	
	    static createFrom(source: any = {}) {
	        return new ProviderUpsertInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.baseUrls = source["baseUrls"];
	        this.baseUrlStrategy = source["baseUrlStrategy"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	        this.clearHeaders = source["clearHeaders"];
	        this.autoAliasEnabled = source["autoAliasEnabled"];
	        this.defaultGroup = this.convertValues(source["defaultGroup"], ProviderGroupInput);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ProxyRoutingSettingsInput {
	    strategy: string;
	    params?: number[];
	
	    static createFrom(source: any = {}) {
	        return new ProxyRoutingSettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategy = source["strategy"];
	        this.params = source["params"];
	    }
	}
	export class RoutingStrategyParamSpec {
	    key: string;
	    type: string;
	    required: boolean;
	    defaultValue?: any;
	    description?: string;
	    enum?: string[];
	    min?: number;
	    max?: number;
	
	    static createFrom(source: any = {}) {
	        return new RoutingStrategyParamSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.type = source["type"];
	        this.required = source["required"];
	        this.defaultValue = source["defaultValue"];
	        this.description = source["description"];
	        this.enum = source["enum"];
	        this.min = source["min"];
	        this.max = source["max"];
	    }
	}
	export class RoutingStrategyDescriptor {
	    name: string;
	    displayName: string;
	    description?: string;
	    defaults?: Record<string, any>;
	    parameters?: RoutingStrategyParamSpec[];
	
	    static createFrom(source: any = {}) {
	        return new RoutingStrategyDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.displayName = source["displayName"];
	        this.description = source["description"];
	        this.defaults = source["defaults"];
	        this.parameters = this.convertValues(source["parameters"], RoutingStrategyParamSpec);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxyRoutingSettingsView {
	    strategy: string;
	    params?: Record<string, any>;
	    descriptors?: RoutingStrategyDescriptor[];
	
	    static createFrom(source: any = {}) {
	        return new ProxyRoutingSettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategy = source["strategy"];
	        this.params = source["params"];
	        this.descriptors = this.convertValues(source["descriptors"], RoutingStrategyDescriptor);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxySettingsInput {
	    connectTimeoutMs: number;
	    responseHeaderTimeoutMs: number;
	    firstByteTimeoutMs: number;
	    requestReadTimeoutMs: number;
	    streamIdleTimeoutMs: number;
	    streamPrecommitBufferMs: number;
	    excludeFirstTokenLatencyFromRate?: boolean;
	    failoverStatusCodes: number[];
	    routing: ProxyRoutingSettingsInput;
	
	    static createFrom(source: any = {}) {
	        return new ProxySettingsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connectTimeoutMs = source["connectTimeoutMs"];
	        this.responseHeaderTimeoutMs = source["responseHeaderTimeoutMs"];
	        this.firstByteTimeoutMs = source["firstByteTimeoutMs"];
	        this.requestReadTimeoutMs = source["requestReadTimeoutMs"];
	        this.streamIdleTimeoutMs = source["streamIdleTimeoutMs"];
	        this.streamPrecommitBufferMs = source["streamPrecommitBufferMs"];
	        this.excludeFirstTokenLatencyFromRate = source["excludeFirstTokenLatencyFromRate"];
	        this.failoverStatusCodes = source["failoverStatusCodes"];
	        this.routing = this.convertValues(source["routing"], ProxyRoutingSettingsInput);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxySettingsView {
	    connectTimeoutMs: number;
	    responseHeaderTimeoutMs: number;
	    firstByteTimeoutMs: number;
	    requestReadTimeoutMs: number;
	    streamIdleTimeoutMs: number;
	    streamPrecommitBufferMs: number;
	    excludeFirstTokenLatencyFromRate: boolean;
	    failoverStatusCodes: number[];
	    routing: ProxyRoutingSettingsView;
	
	    static createFrom(source: any = {}) {
	        return new ProxySettingsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connectTimeoutMs = source["connectTimeoutMs"];
	        this.responseHeaderTimeoutMs = source["responseHeaderTimeoutMs"];
	        this.firstByteTimeoutMs = source["firstByteTimeoutMs"];
	        this.requestReadTimeoutMs = source["requestReadTimeoutMs"];
	        this.streamIdleTimeoutMs = source["streamIdleTimeoutMs"];
	        this.streamPrecommitBufferMs = source["streamPrecommitBufferMs"];
	        this.excludeFirstTokenLatencyFromRate = source["excludeFirstTokenLatencyFromRate"];
	        this.failoverStatusCodes = source["failoverStatusCodes"];
	        this.routing = this.convertValues(source["routing"], ProxyRoutingSettingsView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProxySettingsSaveResult {
	    settings: ProxySettingsView;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProxySettingsSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], ProxySettingsView);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class RequestRewriteRuleInput {
	    name: string;
	    alias?: string;
	    providerGroups?: ProviderGroupSelectorInput[];
	    enabled: boolean;
	    override: boolean;
	    ops?: config.RequestRewriteOperation[];
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.alias = source["alias"];
	        this.providerGroups = this.convertValues(source["providerGroups"], ProviderGroupSelectorInput);
	        this.enabled = source["enabled"];
	        this.override = source["override"];
	        this.ops = this.convertValues(source["ops"], config.RequestRewriteOperation);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestRewriteRuleRemoveInput {
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleRemoveInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	    }
	}
	export class RequestRewriteRuleRemoveResult {
	    ok: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleRemoveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	    }
	}
	export class RequestRewriteRuleReorderInput {
	    names: string[];
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleReorderInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.names = source["names"];
	    }
	}
	export class RequestRewriteRuleView {
	    name: string;
	    alias?: string;
	    providerGroups?: ProviderGroupSelectorView[];
	    enabled: boolean;
	    override: boolean;
	    ops?: config.RequestRewriteOperation[];
	    legacy?: boolean;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.alias = source["alias"];
	        this.providerGroups = this.convertValues(source["providerGroups"], ProviderGroupSelectorView);
	        this.enabled = source["enabled"];
	        this.override = source["override"];
	        this.ops = this.convertValues(source["ops"], config.RequestRewriteOperation);
	        this.legacy = source["legacy"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestRewriteRuleReorderResult {
	    rules: RequestRewriteRuleView[];
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleReorderResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rules = this.convertValues(source["rules"], RequestRewriteRuleView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestRewriteRuleStateInput {
	    name: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteRuleStateInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.enabled = source["enabled"];
	    }
	}
	
	export class TraceAttempt {
	    attempt: number;
	    provider?: string;
	    group?: string;
	    model?: string;
	    url?: string;
	    apiKeyIndex?: number;
	    apiKeyMasked?: string;
	    startedAt: string;
	    durationMs: number;
	    firstByteMs?: number;
	    firstTokenMs?: number;
	    statusCode?: number;
	    success: boolean;
	    retryable: boolean;
	    skipped: boolean;
	    result?: string;
	    error?: string;
	    requestHeaders?: Record<string, string>;
	    requestParams?: any;
	    responseHeaders?: Record<string, string>;
	    responseBody?: string;
	
	    static createFrom(source: any = {}) {
	        return new TraceAttempt(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attempt = source["attempt"];
	        this.provider = source["provider"];
	        this.group = source["group"];
	        this.model = source["model"];
	        this.url = source["url"];
	        this.apiKeyIndex = source["apiKeyIndex"];
	        this.apiKeyMasked = source["apiKeyMasked"];
	        this.startedAt = source["startedAt"];
	        this.durationMs = source["durationMs"];
	        this.firstByteMs = source["firstByteMs"];
	        this.firstTokenMs = source["firstTokenMs"];
	        this.statusCode = source["statusCode"];
	        this.success = source["success"];
	        this.retryable = source["retryable"];
	        this.skipped = source["skipped"];
	        this.result = source["result"];
	        this.error = source["error"];
	        this.requestHeaders = source["requestHeaders"];
	        this.requestParams = source["requestParams"];
	        this.responseHeaders = source["responseHeaders"];
	        this.responseBody = source["responseBody"];
	    }
	}
	export class TraceUsage {
	    rawInputTokens?: number;
	    rawOutputTokens?: number;
	    rawTotalTokens?: number;
	    inputTokens?: number;
	    outputTokens?: number;
	    reasoningTokens?: number;
	    cacheReadTokens?: number;
	    cacheWriteTokens?: number;
	    cacheWrite1hTokens?: number;
	    source?: string;
	    precision?: string;
	    notes?: string[];
	
	    static createFrom(source: any = {}) {
	        return new TraceUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rawInputTokens = source["rawInputTokens"];
	        this.rawOutputTokens = source["rawOutputTokens"];
	        this.rawTotalTokens = source["rawTotalTokens"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.reasoningTokens = source["reasoningTokens"];
	        this.cacheReadTokens = source["cacheReadTokens"];
	        this.cacheWriteTokens = source["cacheWriteTokens"];
	        this.cacheWrite1hTokens = source["cacheWrite1hTokens"];
	        this.source = source["source"];
	        this.precision = source["precision"];
	        this.notes = source["notes"];
	    }
	}
	export class RequestTrace {
	    id: number;
	    startedAt: string;
	    finishedAt?: string;
	    durationMs: number;
	    firstByteMs?: number;
	    firstTokenMs?: number;
	    usage?: TraceUsage;
	    inputTokens?: number;
	    outputTokens?: number;
	    generatedOutputTokens?: number;
	    protocol: string;
	    rawModel?: string;
	    alias?: string;
	    stream: boolean;
	    success: boolean;
	    statusCode?: number;
	    errorCode?: string;
	    error?: string;
	    finalProvider?: string;
	    finalGroup?: string;
	    finalModel?: string;
	    finalUrl?: string;
	    failover: boolean;
	    attemptCount: number;
	    requestHeaders?: Record<string, string>;
	    requestParams?: any;
	    attempts: TraceAttempt[];
	
	    static createFrom(source: any = {}) {
	        return new RequestTrace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.durationMs = source["durationMs"];
	        this.firstByteMs = source["firstByteMs"];
	        this.firstTokenMs = source["firstTokenMs"];
	        this.usage = this.convertValues(source["usage"], TraceUsage);
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.generatedOutputTokens = source["generatedOutputTokens"];
	        this.protocol = source["protocol"];
	        this.rawModel = source["rawModel"];
	        this.alias = source["alias"];
	        this.stream = source["stream"];
	        this.success = source["success"];
	        this.statusCode = source["statusCode"];
	        this.errorCode = source["errorCode"];
	        this.error = source["error"];
	        this.finalProvider = source["finalProvider"];
	        this.finalGroup = source["finalGroup"];
	        this.finalModel = source["finalModel"];
	        this.finalUrl = source["finalUrl"];
	        this.failover = source["failover"];
	        this.attemptCount = source["attemptCount"];
	        this.requestHeaders = source["requestHeaders"];
	        this.requestParams = source["requestParams"];
	        this.attempts = this.convertValues(source["attempts"], TraceAttempt);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RequestTraceListInput {
	    page: number;
	    pageSize: number;
	    aliases?: string[];
	    failoverCounts?: number[];
	    statusCodes?: number[];
	    startedFrom?: string;
	    startedTo?: string;
	
	    static createFrom(source: any = {}) {
	        return new RequestTraceListInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.page = source["page"];
	        this.pageSize = source["pageSize"];
	        this.aliases = source["aliases"];
	        this.failoverCounts = source["failoverCounts"];
	        this.statusCodes = source["statusCodes"];
	        this.startedFrom = source["startedFrom"];
	        this.startedTo = source["startedTo"];
	    }
	}
	export class TraceStats {
	    success: number;
	    failover: number;
	    failed: number;
	
	    static createFrom(source: any = {}) {
	        return new TraceStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.failover = source["failover"];
	        this.failed = source["failed"];
	    }
	}
	export class RequestTraceListResult {
	    items: RequestTrace[];
	    total: number;
	    page: number;
	    pageSize: number;
	    availableAliases?: string[];
	    availableFailoverCounts?: number[];
	    availableStatusCodes?: number[];
	    stats: TraceStats;
	
	    static createFrom(source: any = {}) {
	        return new RequestTraceListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], RequestTrace);
	        this.total = source["total"];
	        this.page = source["page"];
	        this.pageSize = source["pageSize"];
	        this.availableAliases = source["availableAliases"];
	        this.availableFailoverCounts = source["availableFailoverCounts"];
	        this.availableStatusCodes = source["availableStatusCodes"];
	        this.stats = this.convertValues(source["stats"], TraceStats);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class Service {
	
	
	    static createFrom(source: any = {}) {
	        return new Service(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	
	export class SyncInput {
	    target?: string;
	    setModel?: string;
	    setSmallModel?: string;
	    dryRun: boolean;
	    copyOnly?: boolean;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.dryRun = source["dryRun"];
	        this.copyOnly = source["copyOnly"];
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	    }
	}
	export class SyncedProviderView {
	    key: string;
	    protocol: string;
	    aliasNames: string[];
	
	    static createFrom(source: any = {}) {
	        return new SyncedProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.protocol = source["protocol"];
	        this.aliasNames = source["aliasNames"];
	    }
	}
	export class SyncPreview {
	    targetPath: string;
	    protocols: SyncedProviderView[];
	    setModel?: string;
	    setSmallModel?: string;
	    content?: string;
	    wouldChange: boolean;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	    fileSnapshot: OpenCodeFileSnapshot;
	    runtimeSnapshot: OpenCodeRuntimeSnapshot;
	    doctorIssues?: DoctorIssue[];
	    summary: OpenCodeReconciliationSummary;
	    aliasPreviews?: AliasSyncPreviewView[];
	    overallSummary?: DiffSummaryView;
	
	    static createFrom(source: any = {}) {
	        return new SyncPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.protocols = this.convertValues(source["protocols"], SyncedProviderView);
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.content = source["content"];
	        this.wouldChange = source["wouldChange"];
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	        this.fileSnapshot = this.convertValues(source["fileSnapshot"], OpenCodeFileSnapshot);
	        this.runtimeSnapshot = this.convertValues(source["runtimeSnapshot"], OpenCodeRuntimeSnapshot);
	        this.doctorIssues = this.convertValues(source["doctorIssues"], DoctorIssue);
	        this.summary = this.convertValues(source["summary"], OpenCodeReconciliationSummary);
	        this.aliasPreviews = this.convertValues(source["aliasPreviews"], AliasSyncPreviewView);
	        this.overallSummary = this.convertValues(source["overallSummary"], DiffSummaryView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncResult {
	    targetPath: string;
	    protocols: SyncedProviderView[];
	    changed: boolean;
	    dryRun: boolean;
	    setModel?: string;
	    setSmallModel?: string;
	    content?: string;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	    fileSnapshot: OpenCodeFileSnapshot;
	    runtimeSnapshot: OpenCodeRuntimeSnapshot;
	    doctorIssues?: DoctorIssue[];
	    summary: OpenCodeReconciliationSummary;
	    aliasPreviews?: AliasSyncPreviewView[];
	    overallSummary?: DiffSummaryView;
	
	    static createFrom(source: any = {}) {
	        return new SyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.protocols = this.convertValues(source["protocols"], SyncedProviderView);
	        this.changed = source["changed"];
	        this.dryRun = source["dryRun"];
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.content = source["content"];
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	        this.fileSnapshot = this.convertValues(source["fileSnapshot"], OpenCodeFileSnapshot);
	        this.runtimeSnapshot = this.convertValues(source["runtimeSnapshot"], OpenCodeRuntimeSnapshot);
	        this.doctorIssues = this.convertValues(source["doctorIssues"], DoctorIssue);
	        this.summary = this.convertValues(source["summary"], OpenCodeReconciliationSummary);
	        this.aliasPreviews = this.convertValues(source["aliasPreviews"], AliasSyncPreviewView);
	        this.overallSummary = this.convertValues(source["overallSummary"], DiffSummaryView);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	
	
	

}

export namespace config {
	
	export class RequestRewriteOperation {
	    op: string;
	    path: string;
	    index?: number;
	
	    static createFrom(source: any = {}) {
	        return new RequestRewriteOperation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.op = source["op"];
	        this.path = source["path"];
	        this.index = source["index"];
	    }
	}

}

export namespace desktop {
	
	export class Bindings {
	
	
	    static createFrom(source: any = {}) {
	        return new Bindings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}

}

export namespace lifecycle {
	
	export class ExternalRefs {
	    OpenCodeModel: string;
	    OpenCodeSmallModel: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalRefs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.OpenCodeModel = source["OpenCodeModel"];
	        this.OpenCodeSmallModel = source["OpenCodeSmallModel"];
	    }
	}
	export class Operation {
	    kind: string;
	    payload: number[];
	
	    static createFrom(source: any = {}) {
	        return new Operation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.payload = source["payload"];
	    }
	}
	export class Selection {
	    choiceId: string;
	    optionId: string;
	    params?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new Selection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.choiceId = source["choiceId"];
	        this.optionId = source["optionId"];
	        this.params = source["params"];
	    }
	}

}
