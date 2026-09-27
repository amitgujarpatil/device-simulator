package crypto

import (
	"crypto"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

var (
	defaultAES128Key = []byte("intangles128key!")                // 16 bytes
	defaultAES256Key = []byte("intangles256keydefault_v2_global") // 32 bytes
)

// EncryptPacket implements the hybrid RSA+AES scheme from encryptionEngine.js.
// version: 1=AES-128-ECB, 2=AES-256-ECB (default)
func EncryptPacket(payload []byte, publicKeyPEM string, version int) ([]byte, error) {
	var aesKey []byte
	switch version {
	case 1:
		aesKey = defaultAES128Key
	default:
		aesKey = defaultAES256Key
	}

	// Null-pad to 16-byte block boundary
	blockSize := 16
	padded := make([]byte, (len(payload)+blockSize-1)/blockSize*blockSize)
	copy(padded, payload)

	// AES-ECB encrypt: split into 16-byte blocks, encrypt each independently
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += blockSize {
		block.Encrypt(encrypted[i:i+blockSize], padded[i:i+blockSize])
	}

	// RSA-OAEP with SHA-1 (matches JS: crypto.subtle OAEP + SHA-1)
	pubKey, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		return nil, err
	}
	encAESKey, err := rsa.EncryptOAEP(crypto.SHA1.New(), rand.Reader, pubKey, aesKey, nil)
	if err != nil {
		return nil, err
	}

	// Pack: version(2B BE) | rsaEncKey(256B) | dataLen(4B BE) | encPayload
	out := make([]byte, 2+256+4+len(encrypted))
	out[0] = 0
	out[1] = byte(version)
	copy(out[2:258], encAESKey)
	l := len(encrypted)
	out[258] = byte(l >> 24)
	out[259] = byte(l >> 16)
	out[260] = byte(l >> 8)
	out[261] = byte(l)
	copy(out[262:], encrypted)
	return out, nil
}

func parsePublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA key")
	}
	return rsaPub, nil
}
