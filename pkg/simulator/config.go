package simulator

// Config is the full simulation configuration passed from the frontend.
type Config struct {
	SrcIMEI            string `json:"srcImei"`
	TgtIMEI            string `json:"tgtImei"`
	FromMS             int64  `json:"fromMs"`
	UntilMS            int64  `json:"untilMs"`
	HistoryEndMS       int64  `json:"historyEndMs"`
	APIBase            string `json:"apiBase"`
	APIToken           string `json:"apiToken"`
	UserToken          string `json:"userToken"`
	SessionToken       string `json:"sessionToken"`
	UploadURL          string `json:"uploadUrl"`
	MQTTProtocol       string `json:"mqttProtocol"`
	MQTTBroker         string `json:"mqttBroker"`
	MQTTPort           int    `json:"mqttPort"`
	TLSCerts           string `json:"tlsCerts"`
	MQTTUsername       string `json:"mqttUsername"`
	// Fallback TLS settings used when the primary plain broker is unreachable.
	FallbackMQTTProtocol string `json:"fallbackMqttProtocol"`
	FallbackMQTTBroker   string `json:"fallbackMqttBroker"`
	FallbackMQTTPort     int    `json:"fallbackMqttPort"`
	FallbackTLSCerts     string `json:"fallbackTlsCerts"`
	EncryptEnabled     bool   `json:"encryptEnabled"`
	AESVersion         int    `json:"aesVersion"`
	RSAKey             string `json:"rsaKey"`
	BatchSize          int    `json:"batchSize"`
	BatchUploadDelayMs int    `json:"batchUploadDelayMs"`
	GPSl1IntervalMs    int    `json:"gpsL1IntervalMs"`
	OBDAccumIntervalMs int    `json:"obdAccumIntervalMs"`
	NormalIntervalMs      int    `json:"normalModeIntervalMs"`
	NormalGPSIntervalMs   int    `json:"normalGpsIntervalMs"`
	NormalOBDIntervalMs   int    `json:"normalObdIntervalMs"`
	// NormalStreamMode: "independent" (default) = GPS+OBD on separate goroutines/timers;
	// "natural" = single goroutine, time-sorted original order, one delay per packet.
	NormalStreamMode        string `json:"normalStreamMode"`
	NaturalOrderIntervalMs  int    `json:"naturalOrderIntervalMs"`
	APIPageSize        int    `json:"apiPageSize"`
	APIRequestDelay    int    `json:"apiRequestDelay"`
	OutputDir          string `json:"outputDir"`
	DryRun             bool   `json:"dryRun"`
	Mode               string `json:"mode"`
	EnrichL1           bool   `json:"enrichL1"`
	SkipFetch          bool   `json:"skipFetch"`
	// Cross-region fetch: when set, source data is fetched from this API instead of APIBase/APIToken.
	SrcAPIBase  string `json:"srcApiBase"`
	SrcAPIToken string `json:"srcApiToken"`
}

// RegionConfig is used for TestMQTT.
type RegionConfig struct {
	MQTTProtocol string `json:"mqttProtocol"`
	MQTTBroker   string `json:"mqttBroker"`
	MQTTPort     int    `json:"mqttPort"`
	TLSCerts     string `json:"tlsCerts"`
	TgtIMEI      string `json:"tgtImei"`
	MQTTUsername string `json:"mqttUsername"`
}

// SimEvent is emitted to the frontend via Wails events.
type SimEvent struct {
	Elapsed int64                  `json:"elapsed"`
	Tag     string                 `json:"tag"`
	Cls     string                 `json:"cls"`
	Msg     string                 `json:"msg"`
	Ty      string                 `json:"ty"`
	Step    string                 `json:"step"`
	Data    map[string]interface{} `json:"data,omitempty"`
}

// Packet represents a single telemetry entry from the API.
type Packet struct {
	T      int64                  `json:"t"`
	Packet map[string]interface{} `json:"packet"`
}

// livePacket is a packet with type classification for phase 2.
type livePacket struct {
	Packet map[string]interface{}
	Type   string
}
