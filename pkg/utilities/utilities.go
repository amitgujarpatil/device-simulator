package utilities

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"hash"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Hash returns MD5, SHA1, SHA256 and SHA512 hex digests of text.
func Hash(text string) map[string]string {
	data := []byte(text)
	h := func(fn func() hash.Hash) string {
		w := fn()
		w.Write(data)
		return hex.EncodeToString(w.Sum(nil))
	}
	return map[string]string{
		"md5":    h(func() hash.Hash { return md5.New() }),
		"sha1":   h(func() hash.Hash { return sha1.New() }),
		"sha256": h(func() hash.Hash { return sha256.New() }),
		"sha512": h(func() hash.Hash { return sha512.New() }),
	}
}

// HMAC computes HMAC-SHA256 or HMAC-SHA512.
func HMAC(text, key, algo string) (string, error) {
	var hfn func() hash.Hash
	switch strings.ToLower(algo) {
	case "sha256":
		hfn = sha256.New
	case "sha512":
		hfn = sha512.New
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algo)
	}
	mac := hmac.New(hfn, []byte(key))
	mac.Write([]byte(text))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// Compress compresses text with zlib or gzip and returns base64-encoded output.
func Compress(text, algo string) (string, error) {
	var buf bytes.Buffer
	switch strings.ToLower(algo) {
	case "zlib":
		w := zlib.NewWriter(&buf)
		if _, err := w.Write([]byte(text)); err != nil {
			return "", err
		}
		if err := w.Close(); err != nil {
			return "", err
		}
	case "gzip":
		w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			return "", err
		}
		if _, err := w.Write([]byte(text)); err != nil {
			return "", err
		}
		if err := w.Close(); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algo)
	}
	orig := len([]byte(text))
	comp := buf.Len()
	ratio := 0.0
	if orig > 0 {
		ratio = float64(comp) / float64(orig) * 100
	}
	_ = ratio
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// CompressInfo is the return type for Compress with metadata.
type CompressInfo struct {
	Data            string  `json:"data"`
	OriginalBytes   int     `json:"originalBytes"`
	CompressedBytes int     `json:"compressedBytes"`
	Ratio           float64 `json:"ratio"`
}

// CompressWithInfo compresses and returns data with compression stats.
func CompressWithInfo(text, algo string) (CompressInfo, error) {
	var buf bytes.Buffer
	orig := []byte(text)
	switch strings.ToLower(algo) {
	case "zlib":
		w := zlib.NewWriter(&buf)
		if _, err := w.Write(orig); err != nil {
			return CompressInfo{}, err
		}
		if err := w.Close(); err != nil {
			return CompressInfo{}, err
		}
	case "gzip":
		w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if err != nil {
			return CompressInfo{}, err
		}
		if _, err := w.Write(orig); err != nil {
			return CompressInfo{}, err
		}
		if err := w.Close(); err != nil {
			return CompressInfo{}, err
		}
	default:
		return CompressInfo{}, fmt.Errorf("unsupported algorithm: %s", algo)
	}
	ratio := 0.0
	if len(orig) > 0 {
		ratio = (1 - float64(buf.Len())/float64(len(orig))) * 100
	}
	return CompressInfo{
		Data:            base64.StdEncoding.EncodeToString(buf.Bytes()),
		OriginalBytes:   len(orig),
		CompressedBytes: buf.Len(),
		Ratio:           ratio,
	}, nil
}

// Decompress decodes base64 then decompresses with zlib or gzip.
func Decompress(b64Input, algo string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64Input))
	if err != nil {
		return "", fmt.Errorf("invalid base64: %w", err)
	}
	r := bytes.NewReader(data)
	var rc io.ReadCloser
	switch strings.ToLower(algo) {
	case "zlib":
		rc, err = zlib.NewReader(r)
		if err != nil {
			return "", err
		}
	case "gzip":
		rc, err = gzip.NewReader(r)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported algorithm: %s", algo)
	}
	defer rc.Close()
	out, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CertInfo holds parsed certificate fields.
type CertInfo struct {
	Subject          string   `json:"subject"`
	Issuer           string   `json:"issuer"`
	CommonName       string   `json:"commonName"`
	IssuerCommonName string   `json:"issuerCommonName"`
	Organization     []string `json:"organization"`
	NotBefore        string   `json:"notBefore"`
	NotAfter         string   `json:"notAfter"`
	IsExpired        bool     `json:"isExpired"`
	DaysUntilExpiry  int      `json:"daysUntilExpiry"`
	SerialNumber     string   `json:"serialNumber"`
	Version          int      `json:"version"`
	IsCA             bool     `json:"isCA"`
	DNSNames         []string `json:"dnsNames"`
	IPAddresses      []string `json:"ipAddresses"`
	EmailAddresses   []string `json:"emailAddresses"`
	KeyUsage         []string `json:"keyUsage"`
	ExtKeyUsage      []string `json:"extKeyUsage"`
	PublicKeyAlgo    string   `json:"publicKeyAlgo"`
	SignatureAlgo    string   `json:"signatureAlgo"`
	FingerprintSHA1  string   `json:"fingerprintSHA1"`
	FingerprintSHA256 string  `json:"fingerprintSHA256"`
}

// ParseCert parses a PEM-encoded X.509 certificate.
func ParseCert(pemText string) (CertInfo, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return CertInfo{}, fmt.Errorf("no PEM block found — paste a valid certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CertInfo{}, fmt.Errorf("parse certificate: %w", err)
	}

	fp1 := sha1.Sum(block.Bytes)
	fp256 := sha256.Sum256(block.Bytes)

	var keyUsages []string
	ku := cert.KeyUsage
	if ku&x509.KeyUsageDigitalSignature != 0  { keyUsages = append(keyUsages, "Digital Signature") }
	if ku&x509.KeyUsageContentCommitment != 0 { keyUsages = append(keyUsages, "Content Commitment") }
	if ku&x509.KeyUsageKeyEncipherment != 0   { keyUsages = append(keyUsages, "Key Encipherment") }
	if ku&x509.KeyUsageDataEncipherment != 0  { keyUsages = append(keyUsages, "Data Encipherment") }
	if ku&x509.KeyUsageKeyAgreement != 0      { keyUsages = append(keyUsages, "Key Agreement") }
	if ku&x509.KeyUsageCertSign != 0          { keyUsages = append(keyUsages, "Certificate Sign") }
	if ku&x509.KeyUsageCRLSign != 0           { keyUsages = append(keyUsages, "CRL Sign") }

	var extKU []string
	for _, e := range cert.ExtKeyUsage {
		switch e {
		case x509.ExtKeyUsageServerAuth:      extKU = append(extKU, "Server Auth")
		case x509.ExtKeyUsageClientAuth:      extKU = append(extKU, "Client Auth")
		case x509.ExtKeyUsageCodeSigning:     extKU = append(extKU, "Code Signing")
		case x509.ExtKeyUsageEmailProtection: extKU = append(extKU, "Email Protection")
		case x509.ExtKeyUsageTimeStamping:    extKU = append(extKU, "Timestamping")
		case x509.ExtKeyUsageOCSPSigning:     extKU = append(extKU, "OCSP Signing")
		}
	}

	var ips []string
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	now := time.Now()
	days := int(cert.NotAfter.Sub(now).Hours() / 24)

	return CertInfo{
		Subject:          cert.Subject.String(),
		Issuer:           cert.Issuer.String(),
		CommonName:       cert.Subject.CommonName,
		IssuerCommonName: cert.Issuer.CommonName,
		Organization:     cert.Subject.Organization,
		NotBefore:        cert.NotBefore.UTC().Format("2006-01-02 15:04:05 UTC"),
		NotAfter:         cert.NotAfter.UTC().Format("2006-01-02 15:04:05 UTC"),
		IsExpired:        now.After(cert.NotAfter),
		DaysUntilExpiry:  days,
		SerialNumber:     cert.SerialNumber.Text(16),
		Version:          cert.Version,
		IsCA:             cert.IsCA,
		DNSNames:         cert.DNSNames,
		IPAddresses:      ips,
		EmailAddresses:   cert.EmailAddresses,
		KeyUsage:         keyUsages,
		ExtKeyUsage:      extKU,
		PublicKeyAlgo:    cert.PublicKeyAlgorithm.String(),
		SignatureAlgo:    cert.SignatureAlgorithm.String(),
		FingerprintSHA1:  colonSep(hex.EncodeToString(fp1[:])),
		FingerprintSHA256: colonSep(hex.EncodeToString(fp256[:])),
	}, nil
}

func colonSep(s string) string {
	var b strings.Builder
	for i, c := range s {
		if i > 0 && i%2 == 0 {
			b.WriteRune(':')
		}
		b.WriteRune(c)
	}
	return strings.ToUpper(b.String())
}

// CertFormats holds the cert in different inline string formats.
type CertFormats struct {
	SingleLine string `json:"singleLine"`
	JSONString string `json:"jsonString"`
	Base64DER  string `json:"base64DER"`
}

// CertToFormats converts a PEM cert to three different inline string formats.
func CertToFormats(pemText string) (CertFormats, error) {
	trimmed := strings.TrimSpace(pemText)
	if trimmed == "" {
		return CertFormats{}, fmt.Errorf("empty input")
	}
	block, _ := pem.Decode([]byte(trimmed))

	singleLine := strings.ReplaceAll(trimmed, "\n", "\\n")

	jsonBytes, _ := json.Marshal(trimmed)
	jsonStr := string(jsonBytes)

	var b64der string
	if block != nil {
		b64der = base64.StdEncoding.EncodeToString(block.Bytes)
	}

	return CertFormats{
		SingleLine: singleLine,
		JSONString: jsonStr,
		Base64DER:  b64der,
	}, nil
}

// YAMLToJSON converts YAML input to indented JSON.
func YAMLToJSON(yamlStr string) (string, error) {
	var obj interface{}
	if err := yaml.Unmarshal([]byte(yamlStr), &obj); err != nil {
		return "", fmt.Errorf("YAML parse: %w", err)
	}
	obj = normaliseYAML(obj)
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("JSON marshal: %w", err)
	}
	return string(out), nil
}

// JSONToYAML converts JSON input to YAML.
func JSONToYAML(jsonStr string) (string, error) {
	var obj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &obj); err != nil {
		return "", fmt.Errorf("JSON parse: %w", err)
	}
	out, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("YAML marshal: %w", err)
	}
	return string(out), nil
}

// normaliseYAML converts yaml.v3 map[interface{}]interface{} nodes to map[string]interface{}.
func normaliseYAML(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			out[k] = normaliseYAML(v)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			out[fmt.Sprintf("%v", k)] = normaliseYAML(v)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, v := range val {
			out[i] = normaliseYAML(v)
		}
		return out
	default:
		return v
	}
}
