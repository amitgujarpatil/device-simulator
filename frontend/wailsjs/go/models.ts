export namespace apiclient {
	
	export class Collection {
	    id: string;
	    workspaceId: string;
	    parentId: string;
	    name: string;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new Collection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.parentId = source["parentId"];
	        this.name = source["name"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class EnvVariable {
	    id: string;
	    envId: string;
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvVariable(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.envId = source["envId"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Environment {
	    id: string;
	    workspaceId: string;
	    name: string;
	    isActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.name = source["name"];
	        this.isActive = source["isActive"];
	    }
	}
	export class FormFile {
	    key: string;
	    fileName: string;
	    mimeType: string;
	    base64: string;
	
	    static createFrom(source: any = {}) {
	        return new FormFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.fileName = source["fileName"];
	        this.mimeType = source["mimeType"];
	        this.base64 = source["base64"];
	    }
	}
	export class HistoryEntry {
	    id: string;
	    requestId: string;
	    workspaceId: string;
	    method: string;
	    url: string;
	    status: number;
	    statusText: string;
	    headers: {[key: string]: string};
	    body: string;
	    durationMs: number;
	    sizeBytes: number;
	    timestamp: number;
	
	    static createFrom(source: any = {}) {
	        return new HistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.requestId = source["requestId"];
	        this.workspaceId = source["workspaceId"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.durationMs = source["durationMs"];
	        this.sizeBytes = source["sizeBytes"];
	        this.timestamp = source["timestamp"];
	    }
	}
	export class KVPair {
	    key: string;
	    value: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new KVPair(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.enabled = source["enabled"];
	    }
	}
	export class SavedRequest {
	    id: string;
	    collectionId: string;
	    workspaceId: string;
	    name: string;
	    method: string;
	    url: string;
	    params: KVPair[];
	    headers: KVPair[];
	    bodyType: string;
	    bodyContent: string;
	    authType: string;
	    authData: {[key: string]: string};
	    sortOrder: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new SavedRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.collectionId = source["collectionId"];
	        this.workspaceId = source["workspaceId"];
	        this.name = source["name"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.params = this.convertValues(source["params"], KVPair);
	        this.headers = this.convertValues(source["headers"], KVPair);
	        this.bodyType = source["bodyType"];
	        this.bodyContent = source["bodyContent"];
	        this.authType = source["authType"];
	        this.authData = source["authData"];
	        this.sortOrder = source["sortOrder"];
	        this.updatedAt = source["updatedAt"];
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
	export class SendPayload {
	    method: string;
	    url: string;
	    headers: KVPair[];
	    bodyType: string;
	    bodyContent: string;
	    formPairs: KVPair[];
	    formFiles: FormFile[];
	    timeoutSec: number;
	
	    static createFrom(source: any = {}) {
	        return new SendPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = this.convertValues(source["headers"], KVPair);
	        this.bodyType = source["bodyType"];
	        this.bodyContent = source["bodyContent"];
	        this.formPairs = this.convertValues(source["formPairs"], KVPair);
	        this.formFiles = this.convertValues(source["formFiles"], FormFile);
	        this.timeoutSec = source["timeoutSec"];
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
	export class SendResponse {
	    status: number;
	    statusText: string;
	    headers: {[key: string]: string};
	    body: string;
	    durationMs: number;
	    sizeBytes: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new SendResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.statusText = source["statusText"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.durationMs = source["durationMs"];
	        this.sizeBytes = source["sizeBytes"];
	        this.error = source["error"];
	    }
	}
	export class Workspace {
	    id: string;
	    name: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
	    }
	}

}

export namespace main {
	
	export class MQTTClientOpts {
	    protocol: string;
	    host: string;
	    port: number;
	    clientId: string;
	    username: string;
	    password: string;
	    keepAlive: number;
	    cleanSession: boolean;
	    tlsMode: string;
	    caFile: string;
	    certFile: string;
	    keyFile: string;
	    autoReconnect: boolean;
	    protocolVersion: number;
	
	    static createFrom(source: any = {}) {
	        return new MQTTClientOpts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.protocol = source["protocol"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.clientId = source["clientId"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.keepAlive = source["keepAlive"];
	        this.cleanSession = source["cleanSession"];
	        this.tlsMode = source["tlsMode"];
	        this.caFile = source["caFile"];
	        this.certFile = source["certFile"];
	        this.keyFile = source["keyFile"];
	        this.autoReconnect = source["autoReconnect"];
	        this.protocolVersion = source["protocolVersion"];
	    }
	}
	export class MQTTClientState {
	    connected: boolean;
	    broker?: string;
	    clientId?: string;
	    subs: {[key: string]: number};
	
	    static createFrom(source: any = {}) {
	        return new MQTTClientState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.broker = source["broker"];
	        this.clientId = source["clientId"];
	        this.subs = source["subs"];
	    }
	}

}

export namespace mongoclient {
	
	export class CollStats {
	    count: number;
	    storageSize: number;
	    avgDocSize: number;
	
	    static createFrom(source: any = {}) {
	        return new CollStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.count = source["count"];
	        this.storageSize = source["storageSize"];
	        this.avgDocSize = source["avgDocSize"];
	    }
	}
	export class CollectionMeta {
	    name: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new CollectionMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.count = source["count"];
	    }
	}
	export class Connection {
	    id: string;
	    label: string;
	    uri: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.uri = source["uri"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class FieldStat {
	    path: string;
	    type: string;
	    frequency: number;
	    nullPct: number;
	
	    static createFrom(source: any = {}) {
	        return new FieldStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.type = source["type"];
	        this.frequency = source["frequency"];
	        this.nullPct = source["nullPct"];
	    }
	}
	export class FindResult {
	    docs: number[][];
	    total: number;
	    skip: number;
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new FindResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.docs = source["docs"];
	        this.total = source["total"];
	        this.skip = source["skip"];
	        this.limit = source["limit"];
	    }
	}
	export class ImportResult {
	    inserted: number;
	    updated: number;
	    failed: number;
	    errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inserted = source["inserted"];
	        this.updated = source["updated"];
	        this.failed = source["failed"];
	        this.errors = source["errors"];
	    }
	}
	export class Index {
	    name: string;
	    keys: number[];
	    unique: boolean;
	    sparse: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Index(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.keys = source["keys"];
	        this.unique = source["unique"];
	        this.sparse = source["sparse"];
	    }
	}
	export class QueryEntry {
	    id: string;
	    connId: string;
	    db: string;
	    coll: string;
	    filter: string;
	    sort: string;
	    proj: string;
	    ranAt: number;
	
	    static createFrom(source: any = {}) {
	        return new QueryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.connId = source["connId"];
	        this.db = source["db"];
	        this.coll = source["coll"];
	        this.filter = source["filter"];
	        this.sort = source["sort"];
	        this.proj = source["proj"];
	        this.ranAt = source["ranAt"];
	    }
	}
	export class RawResult {
	    docs: number[][];
	    count: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new RawResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.docs = source["docs"];
	        this.count = source["count"];
	        this.message = source["message"];
	    }
	}

}

export namespace simulator {
	
	export class Config {
	    srcImei: string;
	    tgtImei: string;
	    fromMs: number;
	    untilMs: number;
	    historyEndMs: number;
	    apiBase: string;
	    apiToken: string;
	    userToken: string;
	    sessionToken: string;
	    uploadUrl: string;
	    mqttProtocol: string;
	    mqttBroker: string;
	    mqttPort: number;
	    tlsCerts: string;
	    mqttUsername: string;
	    fallbackMqttProtocol: string;
	    fallbackMqttBroker: string;
	    fallbackMqttPort: number;
	    fallbackTlsCerts: string;
	    encryptEnabled: boolean;
	    aesVersion: number;
	    rsaKey: string;
	    batchSize: number;
	    batchUploadDelayMs: number;
	    gpsL1IntervalMs: number;
	    obdAccumIntervalMs: number;
	    normalModeIntervalMs: number;
	    apiPageSize: number;
	    apiRequestDelay: number;
	    outputDir: string;
	    dryRun: boolean;
	    mode: string;
	    enrichL1: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.srcImei = source["srcImei"];
	        this.tgtImei = source["tgtImei"];
	        this.fromMs = source["fromMs"];
	        this.untilMs = source["untilMs"];
	        this.historyEndMs = source["historyEndMs"];
	        this.apiBase = source["apiBase"];
	        this.apiToken = source["apiToken"];
	        this.userToken = source["userToken"];
	        this.sessionToken = source["sessionToken"];
	        this.uploadUrl = source["uploadUrl"];
	        this.mqttProtocol = source["mqttProtocol"];
	        this.mqttBroker = source["mqttBroker"];
	        this.mqttPort = source["mqttPort"];
	        this.tlsCerts = source["tlsCerts"];
	        this.mqttUsername = source["mqttUsername"];
	        this.fallbackMqttProtocol = source["fallbackMqttProtocol"];
	        this.fallbackMqttBroker = source["fallbackMqttBroker"];
	        this.fallbackMqttPort = source["fallbackMqttPort"];
	        this.fallbackTlsCerts = source["fallbackTlsCerts"];
	        this.encryptEnabled = source["encryptEnabled"];
	        this.aesVersion = source["aesVersion"];
	        this.rsaKey = source["rsaKey"];
	        this.batchSize = source["batchSize"];
	        this.batchUploadDelayMs = source["batchUploadDelayMs"];
	        this.gpsL1IntervalMs = source["gpsL1IntervalMs"];
	        this.obdAccumIntervalMs = source["obdAccumIntervalMs"];
	        this.normalModeIntervalMs = source["normalModeIntervalMs"];
	        this.apiPageSize = source["apiPageSize"];
	        this.apiRequestDelay = source["apiRequestDelay"];
	        this.outputDir = source["outputDir"];
	        this.dryRun = source["dryRun"];
	        this.mode = source["mode"];
	        this.enrichL1 = source["enrichL1"];
	    }
	}
	export class RegionConfig {
	    mqttProtocol: string;
	    mqttBroker: string;
	    mqttPort: number;
	    tlsCerts: string;
	    tgtImei: string;
	    mqttUsername: string;
	
	    static createFrom(source: any = {}) {
	        return new RegionConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mqttProtocol = source["mqttProtocol"];
	        this.mqttBroker = source["mqttBroker"];
	        this.mqttPort = source["mqttPort"];
	        this.tlsCerts = source["tlsCerts"];
	        this.tgtImei = source["tgtImei"];
	        this.mqttUsername = source["mqttUsername"];
	    }
	}

}

export namespace utilities {
	
	export class CertFormats {
	    singleLine: string;
	    jsonString: string;
	    base64DER: string;
	
	    static createFrom(source: any = {}) {
	        return new CertFormats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.singleLine = source["singleLine"];
	        this.jsonString = source["jsonString"];
	        this.base64DER = source["base64DER"];
	    }
	}
	export class CertInfo {
	    subject: string;
	    issuer: string;
	    commonName: string;
	    issuerCommonName: string;
	    organization: string[];
	    notBefore: string;
	    notAfter: string;
	    isExpired: boolean;
	    daysUntilExpiry: number;
	    serialNumber: string;
	    version: number;
	    isCA: boolean;
	    dnsNames: string[];
	    ipAddresses: string[];
	    emailAddresses: string[];
	    keyUsage: string[];
	    extKeyUsage: string[];
	    publicKeyAlgo: string;
	    signatureAlgo: string;
	    fingerprintSHA1: string;
	    fingerprintSHA256: string;
	
	    static createFrom(source: any = {}) {
	        return new CertInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.subject = source["subject"];
	        this.issuer = source["issuer"];
	        this.commonName = source["commonName"];
	        this.issuerCommonName = source["issuerCommonName"];
	        this.organization = source["organization"];
	        this.notBefore = source["notBefore"];
	        this.notAfter = source["notAfter"];
	        this.isExpired = source["isExpired"];
	        this.daysUntilExpiry = source["daysUntilExpiry"];
	        this.serialNumber = source["serialNumber"];
	        this.version = source["version"];
	        this.isCA = source["isCA"];
	        this.dnsNames = source["dnsNames"];
	        this.ipAddresses = source["ipAddresses"];
	        this.emailAddresses = source["emailAddresses"];
	        this.keyUsage = source["keyUsage"];
	        this.extKeyUsage = source["extKeyUsage"];
	        this.publicKeyAlgo = source["publicKeyAlgo"];
	        this.signatureAlgo = source["signatureAlgo"];
	        this.fingerprintSHA1 = source["fingerprintSHA1"];
	        this.fingerprintSHA256 = source["fingerprintSHA256"];
	    }
	}
	export class CompressInfo {
	    data: string;
	    originalBytes: number;
	    compressedBytes: number;
	    ratio: number;
	
	    static createFrom(source: any = {}) {
	        return new CompressInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.originalBytes = source["originalBytes"];
	        this.compressedBytes = source["compressedBytes"];
	        this.ratio = source["ratio"];
	    }
	}

}

