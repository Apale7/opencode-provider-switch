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
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new DoctorIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.message = source["message"];
	    }
	}
	export class DoctorReport {
	    ok: boolean;
	    issues: DoctorIssue[];
	    syncProtocol: string;
	    configPath: string;
	    providerCount: number;
	    aliasCount: number;
	    proxyBindAddress: string;
	    openCodeTargetPath: string;
	    openCodeTargetFound: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DoctorReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.issues = this.convertValues(source["issues"], DoctorIssue);
	        this.syncProtocol = source["syncProtocol"];
	        this.configPath = source["configPath"];
	        this.providerCount = source["providerCount"];
	        this.aliasCount = source["aliasCount"];
	        this.proxyBindAddress = source["proxyBindAddress"];
	        this.openCodeTargetPath = source["openCodeTargetPath"];
	        this.openCodeTargetFound = source["openCodeTargetFound"];
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
	export class ProviderView {
	    id: string;
	    name?: string;
	    protocol: string;
	    baseUrl: string;
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
	        this.apiKey = source["apiKey"];
	        this.headers = source["headers"];
	        this.disabled = source["disabled"];
	        this.skipModels = source["skipModels"];
	        this.clearHeaders = source["clearHeaders"];
	    }
	}
	
	export class ProxySettingsInput {
	    connectTimeoutMs: number;
	    responseHeaderTimeoutMs: number;
	    firstByteTimeoutMs: number;
	    requestReadTimeoutMs: number;
	    streamIdleTimeoutMs: number;
	
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
	    }
	}
	export class ProxySettingsView {
	    connectTimeoutMs: number;
	    responseHeaderTimeoutMs: number;
	    firstByteTimeoutMs: number;
	    requestReadTimeoutMs: number;
	    streamIdleTimeoutMs: number;
	
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
	export class RequestTrace {
	    id: number;
	    startedAt: string;
	    finishedAt?: string;
	    durationMs: number;
	    firstByteMs?: number;
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
	
	    static createFrom(source: any = {}) {
	        return new SyncInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.target = source["target"];
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.dryRun = source["dryRun"];
	    }
	}
	export class SyncPreview {
	    targetPath: string;
	    protocol: string;
	    aliasNames: string[];
	    setModel?: string;
	    setSmallModel?: string;
	    wouldChange: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.protocol = source["protocol"];
	        this.aliasNames = source["aliasNames"];
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
	        this.wouldChange = source["wouldChange"];
	    }
	}
	export class SyncResult {
	    targetPath: string;
	    protocol: string;
	    aliasNames: string[];
	    changed: boolean;
	    dryRun: boolean;
	    setModel?: string;
	    setSmallModel?: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.targetPath = source["targetPath"];
	        this.protocol = source["protocol"];
	        this.aliasNames = source["aliasNames"];
	        this.changed = source["changed"];
	        this.dryRun = source["dryRun"];
	        this.setModel = source["setModel"];
	        this.setSmallModel = source["setSmallModel"];
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

