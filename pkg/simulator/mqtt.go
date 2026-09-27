package simulator

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// embeddedTLSConfig builds a *tls.Config from the bundled CA/cert/key.
// The broker cert uses the legacy CN field (no SANs), which Go 1.15+ rejects
// during standard hostname verification. We work around this by setting
// InsecureSkipVerify=true (disables Go's hostname check) while still
// manually verifying the server cert chain against our embedded CA via
// VerifyPeerCertificate — so we keep CA-trust without the hostname check.
func embeddedTLSConfig() (*tls.Config, error) {
	clientCert, err := tls.X509KeyPair([]byte(embeddedMQTTCert), []byte(embeddedMQTTKey))
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM([]byte(embeddedMQTTCA)) {
		return nil, fmt.Errorf("failed to append CA cert")
	}
	return &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true, //nolint:gosec // hostname check skipped; CA verified manually below
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no server certificate presented")
			}
			serverCert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil {
				return fmt.Errorf("parse server cert: %w", err)
			}
			// Verify cert chain against embedded CA (no hostname check).
			if _, err := serverCert.Verify(x509.VerifyOptions{Roots: caCertPool}); err != nil {
				return fmt.Errorf("server cert not trusted by embedded CA: %w", err)
			}
			return nil
		},
	}, nil
}

func buildTLSConfig(_ Config) (*tls.Config, error) {
	return embeddedTLSConfig()
}

func connectMQTT(cfg Config) (mqtt.Client, error) {
	if cfg.TgtIMEI == "" {
		return nil, fmt.Errorf("target IMEI (clientId) is empty — set it in the test config")
	}

	protocol := "mqtts"
	if cfg.MQTTProtocol != "" {
		protocol = cfg.MQTTProtocol
	}

	opts := mqtt.NewClientOptions()
	broker := fmt.Sprintf("%s://%s:%d", protocol, cfg.MQTTBroker, cfg.MQTTPort)
	opts.AddBroker(broker)
	opts.SetClientID(cfg.TgtIMEI)
	opts.SetKeepAlive(60 * time.Second)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(false)
	opts.SetConnectTimeout(15 * time.Second)

	if protocol == "mqtts" || protocol == "wss" {
		tlsCfg, err := buildTLSConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tlsCfg)
	}

	// Set username only — no password (broker uses TLS client-cert + username for auth).
	if cfg.MQTTUsername != "" {
		opts.SetUsername(cfg.MQTTUsername)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("MQTT connect timeout after 15s")
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("MQTT connect: %w", err)
	}
	if !client.IsConnected() {
		return nil, fmt.Errorf("MQTT client not connected after token wait")
	}
	return client, nil
}

// TestMQTTConnection connects to MQTT and returns ok/message.
func TestMQTTConnection(rcfg RegionConfig) (bool, string) {
	cfg := Config{
		MQTTProtocol: rcfg.MQTTProtocol,
		MQTTBroker:   rcfg.MQTTBroker,
		MQTTPort:     rcfg.MQTTPort,
		TLSCerts:     rcfg.TLSCerts,
		TgtIMEI:      rcfg.TgtIMEI,
		MQTTUsername: rcfg.MQTTUsername,
	}
	client, err := connectMQTT(cfg)
	if err != nil {
		return false, err.Error()
	}
	client.Disconnect(250)
	return true, fmt.Sprintf("Connected to %s:%d", rcfg.MQTTBroker, rcfg.MQTTPort)
}

// publishMQTT publishes a payload to MQTT and waits for confirmation.
func publishMQTT(client mqtt.Client, topic string, payload []byte) error {
	token := client.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
}

// BuildEmbeddedTLSConfig returns a *tls.Config using the bundled client cert/key/CA.
func BuildEmbeddedTLSConfig() (*tls.Config, error) {
	return embeddedTLSConfig()
}

// isNetworkError returns true for DNS / host-unreachable failures (not MQTT-level auth errors).
func isNetworkError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "lookup ") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "host unreachable")
}

// connectWithFallback tries the primary MQTT connection; if it fails with a network/DNS
// error and TLS fallback settings are configured, retries over TLS and logs the switch.
func connectWithFallback(cfg Config, emitFn func(SimEvent), elap func() int64) (mqtt.Client, Config, error) {
	client, err := connectMQTT(cfg)
	if err == nil {
		return client, cfg, nil
	}
	if cfg.FallbackMQTTBroker == "" || !isNetworkError(err) {
		return nil, cfg, err
	}
	parts := strings.Split(err.Error(), ":")
	shortErr := strings.TrimSpace(parts[len(parts)-1])
	emitFn(SimEvent{
		Elapsed: elap(), Tag: "MQTT", Cls: "warn",
		Msg: fmt.Sprintf("Plain broker unreachable (%s) — retrying with TLS (%s:%d)…",
			shortErr, cfg.FallbackMQTTBroker, cfg.FallbackMQTTPort),
		Ty: "warn",
	})
	tlsCfg := cfg
	tlsCfg.MQTTProtocol = cfg.FallbackMQTTProtocol
	tlsCfg.MQTTBroker = cfg.FallbackMQTTBroker
	tlsCfg.MQTTPort = cfg.FallbackMQTTPort
	tlsCfg.TLSCerts = cfg.FallbackTLSCerts
	emitFn(SimEvent{
		Elapsed: elap(), Tag: "MQTT", Cls: "mq",
		Msg: fmt.Sprintf("Connecting to %s://%s:%d (clientId:%s)…",
			tlsCfg.MQTTProtocol, tlsCfg.MQTTBroker, tlsCfg.MQTTPort, tlsCfg.TgtIMEI),
		Ty: "info", Step: "mqtt:connect",
	})
	client2, err2 := connectMQTT(tlsCfg)
	if err2 != nil {
		return nil, cfg, fmt.Errorf("plain: %v; TLS fallback: %w", err, err2)
	}
	return client2, tlsCfg, nil
}
