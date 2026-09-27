package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"device-simulator/pkg/simulator"
)

// MQTTClientOpts holds user-configurable connection parameters.
type MQTTClientOpts struct {
	Protocol     string `json:"protocol"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	ClientID     string `json:"clientId"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	KeepAlive    int    `json:"keepAlive"`
	CleanSession bool   `json:"cleanSession"`
	// TLSMode: none | skip | embedded | custom
	TLSMode  string `json:"tlsMode"`
	// Custom cert file paths (used when tlsMode == "custom")
	CAFile          string `json:"caFile"`
	CertFile        string `json:"certFile"`
	KeyFile         string `json:"keyFile"`
	AutoReconnect   bool   `json:"autoReconnect"`
	ProtocolVersion int    `json:"protocolVersion"` // 3=MQTT 3.1, 4=MQTT 3.1.1 (default)
}

// MQTTClientState is returned to the frontend.
type MQTTClientState struct {
	Connected bool           `json:"connected"`
	Broker    string         `json:"broker,omitempty"`
	ClientID  string         `json:"clientId,omitempty"`
	Subs      map[string]int `json:"subs"`
}

type mqttService struct {
	mu     sync.Mutex
	client mqtt.Client
	broker string
	cid    string
	subs   map[string]byte
}

func newMQTTService() *mqttService {
	return &mqttService{subs: make(map[string]byte)}
}

func (s *mqttService) isConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client != nil && s.client.IsConnected()
}

func (s *mqttService) disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Disconnect(300)
		s.client = nil
	}
	s.subs = make(map[string]byte)
	s.broker = ""
	s.cid = ""
}

func mqttTS() string { return time.Now().Format("15:04:05.000") }

// buildCustomTLSConfig builds a *tls.Config from user-provided cert files.
// Any of the three paths can be empty to omit that component.
func buildCustomTLSConfig(caFile, certFile, keyFile string) (*tls.Config, error) {
	cfg := &tls.Config{}

	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse CA cert: no valid PEM blocks found")
		}
		cfg.RootCAs = pool
	}

	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	} else if certFile != "" || keyFile != "" {
		return nil, fmt.Errorf("both client cert and key must be provided together")
	}

	return cfg, nil
}

// MQTTClientConnect connects to the given broker.
func (a *App) MQTTClientConnect(opts MQTTClientOpts) error {
	if a.mqttSvc.isConnected() {
		a.mqttSvc.disconnect()
	}

	protocol := opts.Protocol
	if protocol == "" {
		protocol = "mqtt"
	}
	port := opts.Port
	if port <= 0 {
		port = 1883
	}
	brokerURL := fmt.Sprintf("%s://%s:%d", protocol, opts.Host, port)

	clientID := opts.ClientID
	if clientID == "" {
		clientID = fmt.Sprintf("devSim-%d", time.Now().UnixMilli()%100000)
	}
	keepAlive := opts.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 60
	}

	mqttOpts := mqtt.NewClientOptions()
	mqttOpts.AddBroker(brokerURL)
	mqttOpts.SetClientID(clientID)
	mqttOpts.SetKeepAlive(time.Duration(keepAlive) * time.Second)
	mqttOpts.SetCleanSession(opts.CleanSession)
	mqttOpts.SetAutoReconnect(opts.AutoReconnect)
	if opts.ProtocolVersion > 0 {
		mqttOpts.SetProtocolVersion(uint(opts.ProtocolVersion))
	}
	mqttOpts.SetConnectTimeout(15 * time.Second)
	if opts.AutoReconnect {
		mqttOpts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
			runtime.EventsEmit(a.ctx, "mqttclient:status", map[string]interface{}{
				"reconnecting": true, "ts": mqttTS(),
			})
		})
		mqttOpts.SetOnConnectHandler(func(_ mqtt.Client) {
			runtime.EventsEmit(a.ctx, "mqttclient:status", map[string]interface{}{
				"connected": true, "broker": brokerURL, "ts": mqttTS(),
			})
		})
	}

	if opts.Username != "" {
		mqttOpts.SetUsername(opts.Username)
		mqttOpts.SetPassword(opts.Password)
	}

	// TLS configuration — only applied when the protocol actually uses TLS.
	if protocol == "mqtts" || protocol == "wss" {
		switch opts.TLSMode {
		case "skip":
			mqttOpts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec
		case "embedded":
			tlsCfg, err := simulator.BuildEmbeddedTLSConfig()
			if err != nil {
				return fmt.Errorf("embedded TLS: %w", err)
			}
			mqttOpts.SetTLSConfig(tlsCfg)
		case "custom":
			tlsCfg, err := buildCustomTLSConfig(opts.CAFile, opts.CertFile, opts.KeyFile)
			if err != nil {
				return fmt.Errorf("custom TLS: %w", err)
			}
			mqttOpts.SetTLSConfig(tlsCfg)
		// "none" / "" → no explicit TLS config; paho uses system CAs
		}
	}

	mqttOpts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		runtime.EventsEmit(a.ctx, "mqttclient:status", map[string]interface{}{
			"connected": false, "error": err.Error(), "ts": mqttTS(),
		})
	})

	client := mqtt.NewClient(mqttOpts)
	tok := client.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		return fmt.Errorf("connection timeout after 15s")
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("%w", err)
	}
	if !client.IsConnected() {
		return fmt.Errorf("connected token OK but client reports not connected")
	}

	a.mqttSvc.mu.Lock()
	a.mqttSvc.client = client
	a.mqttSvc.broker = brokerURL
	a.mqttSvc.cid = clientID
	a.mqttSvc.mu.Unlock()
	return nil
}

// MQTTClientDisconnect closes the active connection.
func (a *App) MQTTClientDisconnect() {
	a.mqttSvc.disconnect()
	runtime.EventsEmit(a.ctx, "mqttclient:status", map[string]interface{}{
		"connected": false, "ts": mqttTS(),
	})
}

// MQTTClientSubscribe subscribes to a topic filter.
// Mutex is NOT held during the blocking tok.Wait() to avoid blocking publish/unsubscribe.
func (a *App) MQTTClientSubscribe(topic string, qos int) error {
	a.mqttSvc.mu.Lock()
	client := a.mqttSvc.client
	a.mqttSvc.mu.Unlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}
	q := byte(qos)
	tok := client.Subscribe(topic, q, a.mqttMsgHandler)
	tok.Wait()
	if err := tok.Error(); err != nil {
		return err
	}
	a.mqttSvc.mu.Lock()
	a.mqttSvc.subs[topic] = q
	a.mqttSvc.mu.Unlock()
	return nil
}

// MQTTClientUnsubscribe removes a subscription.
func (a *App) MQTTClientUnsubscribe(topic string) error {
	a.mqttSvc.mu.Lock()
	client := a.mqttSvc.client
	a.mqttSvc.mu.Unlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}
	tok := client.Unsubscribe(topic)
	tok.Wait()
	if err := tok.Error(); err != nil {
		return err
	}
	a.mqttSvc.mu.Lock()
	delete(a.mqttSvc.subs, topic)
	a.mqttSvc.mu.Unlock()
	return nil
}

// MQTTClientPublish publishes a message and echoes it to the frontend.
func (a *App) MQTTClientPublish(topic, payload string, qos int, retain bool) error {
	a.mqttSvc.mu.Lock()
	client := a.mqttSvc.client
	a.mqttSvc.mu.Unlock()

	if client == nil || !client.IsConnected() {
		return fmt.Errorf("not connected")
	}
	tok := client.Publish(topic, byte(qos), retain, []byte(payload))
	tok.Wait()
	if err := tok.Error(); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "mqttclient:message", map[string]interface{}{
		"id": fmt.Sprintf("m%d", time.Now().UnixNano()),
		"topic": topic, "payload": payload,
		"qos": qos, "retained": retain,
		"ts": mqttTS(), "dir": "out",
	})
	return nil
}

// MQTTClientGetState returns current connection + subscription state.
func (a *App) MQTTClientGetState() MQTTClientState {
	a.mqttSvc.mu.Lock()
	defer a.mqttSvc.mu.Unlock()
	connected := a.mqttSvc.client != nil && a.mqttSvc.client.IsConnected()
	subs := make(map[string]int, len(a.mqttSvc.subs))
	for t, q := range a.mqttSvc.subs {
		subs[t] = int(q)
	}
	return MQTTClientState{
		Connected: connected,
		Broker:    a.mqttSvc.broker,
		ClientID:  a.mqttSvc.cid,
		Subs:      subs,
	}
}

// MQTTPickFile opens a file picker dialog for selecting certificate files.
func (a *App) MQTTPickFile(title string) (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
		Filters: []runtime.FileFilter{
			{DisplayName: "PEM / Certificate Files", Pattern: "*.pem;*.crt;*.cer;*.key;*.p12"},
			{DisplayName: "All Files", Pattern: "*"},
		},
	})
}

// mqttMsgHandler is the shared paho message callback for all subscriptions.
func (a *App) mqttMsgHandler(_ mqtt.Client, msg mqtt.Message) {
	runtime.EventsEmit(a.ctx, "mqttclient:message", map[string]interface{}{
		"id":       fmt.Sprintf("m%d", time.Now().UnixNano()),
		"topic":    msg.Topic(),
		"payload":  string(msg.Payload()),
		"qos":      int(msg.Qos()),
		"retained": msg.Retained(),
		"ts":       mqttTS(),
		"dir":      "in",
	})
}
