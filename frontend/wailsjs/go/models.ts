export namespace app {
	
	export class AliasTargetInput {
	    alias: string;
	    provider: string;
	    model: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.alias = source["alias"];
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.disabled = source["disabled"];
	    }
	}
	export class AliasTargetRefInput {
	    provider: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetRefInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
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
	    model: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AliasTargetView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.provider = source["provider"];
	        this.model = source["model"];
	        this.enabled = source["enabled"];
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
	
	export class DoctorIssue {
	    code: string;
	    severity: string;
	    message: string;
	    protocol?: string;
	    providerKey?: string;
	    alias?: string;
	    path?: string;
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
	        this.code = source["code"];
	        this.severity = source["severity"];
	        this.message = source["message"];
	        this.protocol = source["protocol"];
	        this.providerKey = source["providerKey"];
	        this.alias = source["alias"];
	        this.path = source["path"];
	        this.directory = source["directory"];
	        this.expected = source["expected"];
	        this.actual = source["actual"];
	        this.actionHint = source["actionHint"];
	        this.autoFixAvailable = source["autoFixAvailable"];
	        this.details = source["details"];
	        this.relatedFields = source["relatedFields"];
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
	export class Overview {
	    configPath: string;
	    providerCount: number;
	    aliasCount: number;
	    availableAliases: string[];
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
	export class ProviderImportResult {
	    sourcePath: string;
	    imported: number;
	    skipped: number;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProviderImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.imported = source["imported"];
	        this.skipped = source["skipped"];
	        this.warnings = source["warnings"];
	    }
	}
	export class ProviderPingInput {
	    id?: string;
	    protocol?: string;
	    baseUrl: string;
	    apiKey?: string;
	    headers?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new ProviderPingInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
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
	export class ProviderRefreshModelsInput {
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderRefreshModelsInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	    }
	}
	export class ProviderView {
	    id: string;
	    name?: string;
	    protocol: string;
	    baseUrl: string;
	    baseUrls?: string[];
	    baseUrlStrategy: string;
	    apiKeySet: boolean;
	    apiKeyMasked?: string;
	    headers?: Record<string, string>;
	    models?: string[];
	    modelsSource?: string;
	    disabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.baseUrls = source["baseUrls"];
	        this.baseUrlStrategy = source["baseUrlStrategy"];
	        this.apiKeySet = source["apiKeySet"];
	        this.apiKeyMasked = source["apiKeyMasked"];
	        this.headers = source["headers"];
	        this.models = source["models"];
	        this.modelsSource = source["modelsSource"];
	        this.disabled = source["disabled"];
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
	    protocol: string;
	    baseUrl: string;
	    baseUrls?: string[];
	    baseUrlStrategy: string;
	    apiKey?: string;
	    headers?: Record<string, string>;
	    disabled: boolean;
	    skipModels: boolean;
	    clearHeaders: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderUpsertInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.protocol = source["protocol"];
	        this.baseUrl = source["baseUrl"];
	        this.baseUrls = source["baseUrls"];
	        this.baseUrlStrategy = source["baseUrlStrategy"];
	        this.apiKey = source["apiKey"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	        this.skipModels = source["skipModels"];
	        this.clearHeaders = source["clearHeaders"];
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
	
	
	export class TraceAttempt {
	    attempt: number;
	    provider?: string;
	    model?: string;
	    url?: string;
	    startedAt: string;
	    durationMs: number;
	    firstByteMs?: number;
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
	        this.model = source["model"];
	        this.url = source["url"];
	        this.startedAt = source["startedAt"];
	        this.durationMs = source["durationMs"];
	        this.firstByteMs = source["firstByteMs"];
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
	    usage?: TraceUsage;
	    inputTokens?: number;
	    outputTokens?: number;
	    protocol: string;
	    rawModel?: string;
	    alias?: string;
	    stream: boolean;
	    success: boolean;
	    statusCode?: number;
	    error?: string;
	    finalProvider?: string;
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
	        this.usage = this.convertValues(source["usage"], TraceUsage);
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.protocol = source["protocol"];
	        this.rawModel = source["rawModel"];
	        this.alias = source["alias"];
	        this.stream = source["stream"];
	        this.success = source["success"];
	        this.statusCode = source["statusCode"];
	        this.error = source["error"];
	        this.finalProvider = source["finalProvider"];
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
	    wouldChange: boolean;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	    fileSnapshot: OpenCodeFileSnapshot;
	    runtimeSnapshot: OpenCodeRuntimeSnapshot;
	    doctorIssues?: DoctorIssue[];
	    summary: OpenCodeReconciliationSummary;
	
	    static createFrom(source: any = {}) {
	        return new SyncPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.protocols = this.convertValues(source["protocols"], SyncedProviderView);
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.wouldChange = source["wouldChange"];
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	        this.fileSnapshot = this.convertValues(source["fileSnapshot"], OpenCodeFileSnapshot);
	        this.runtimeSnapshot = this.convertValues(source["runtimeSnapshot"], OpenCodeRuntimeSnapshot);
	        this.doctorIssues = this.convertValues(source["doctorIssues"], DoctorIssue);
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
	export class SyncResult {
	    targetPath: string;
	    protocols: SyncedProviderView[];
	    changed: boolean;
	    dryRun: boolean;
	    setModel?: string;
	    setSmallModel?: string;
	    runtimeBaseUrl?: string;
	    runtimeDirectory?: string;
	    fileSnapshot: OpenCodeFileSnapshot;
	    runtimeSnapshot: OpenCodeRuntimeSnapshot;
	    doctorIssues?: DoctorIssue[];
	    summary: OpenCodeReconciliationSummary;
	
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
	        this.runtimeBaseUrl = source["runtimeBaseUrl"];
	        this.runtimeDirectory = source["runtimeDirectory"];
	        this.fileSnapshot = this.convertValues(source["fileSnapshot"], OpenCodeFileSnapshot);
	        this.runtimeSnapshot = this.convertValues(source["runtimeSnapshot"], OpenCodeRuntimeSnapshot);
	        this.doctorIssues = this.convertValues(source["doctorIssues"], DoctorIssue);
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

