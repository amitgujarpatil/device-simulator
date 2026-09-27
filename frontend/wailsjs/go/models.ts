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

