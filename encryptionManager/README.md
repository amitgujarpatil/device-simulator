# Encryption Manager

Hybrid envelope encryption system for MQTT payload security using RSA-2048 and AES-128-ECB with key versioning support.

## Overview

The Encryption Manager provides a complete solution for encrypting and decrypting MQTT payloads using a hybrid envelope encryption approach. It combines:

- **RSA-2048**: For key encryption and secure key distribution
- **AES-128-ECB**: For fast payload encryption
- **Key Versioning**: Support for key rotation without breaking existing clients
- **AWS Secrets Manager**: Secure storage of private keys
- **MongoDB**: Metadata tracking via IntanglesSDK

## Architecture

### Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    Encryption Manager                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐         ┌─────────────────┐                   │
│  │  KeyManager  │────────▶│EncryptionHandler│                   │
│  └──────────────┘         └─────────────────┘                   │
│        │                            │                           │
│        │ Manages keys               │ Encrypts/Decrypts         │
│        │ and versions               │ payloads                  │
│        │                            │                           │
│        ▼                            ▼                           │
│  ┌─────────────────────────────────────────┐                    │
│  │      AWS Secrets Manager                │                    │
│  │  - mqtt-encryption-key-v1               │                    │
│  │  - mqtt-encryption-key-v2               │                    │
│  │  - mqtt-encryption-key-v3 (current)     │                    │
│  └─────────────────────────────────────────┘                    │
│                     │                                           │
│                     ▼                                           │
│  ┌─────────────────────────────────────────┐                    │
│  │      MongoDB (IntanglesSDK)             │                    │
│  │  Metadata:                              │                    │
│  │  - current_version: 3                   │                    │
│  │  - versions: [v1, v2, v3]               │                    │
│  │  - secret_names, ARNs, timestamps       │                    │
│  └─────────────────────────────────────────┘                    │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Encryption Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                      Encryption Process                         │
└─────────────────────────────────────────────────────────────────┘

Client Side (Encryption):
┌──────────────┐
│  JSON Payload│
└──────┬───────┘
       │
       │ 1. Generate random AES-128 key
       ▼
┌──────────────┐
│  AES Key     │ ────┐
│ (128-bit)    │     │
└──────────────┘     │ 2. Encrypt AES key with RSA public key
                     │
       ┌─────────────┘
       │
       ▼
┌──────────────┐    3. Encrypt payload with AES-128-ECB
│ RSA Public   │
│ Key (v3)     │
└──────────────┘
       │
       │ 4. Build packet: [Version(2)|EncAESKey(256)|DataLen(4)|EncPayload(...)|TrailingJSON]
       ▼
┌──────────────────────────────────────────────────────────────────┐
│  Encrypted Packet                                                │
│  ┌────────┬──────────┬─────────┬─────────────┬──────────────┐    │
│  │Version │Enc AES   │Data Len │Enc Payload  │Trailing JSON │    │
│  │2 bytes │256 bytes │4 bytes  │Variable     │Variable      │    │
│  └────────┴──────────┴─────────┴─────────────┴──────────────┘    │
└──────────────────────────────────────────────────────────────────┘
       │
       │ 5. Send via MQTT
       ▼
   MQTT Broker


Server Side (Decryption):
┌──────────────────────────────────────┐
│  Encrypted Packet (from MQTT)        │
└──────┬───────────────────────────────┘
       │
       │ 1. Parse packet header
       ▼
┌──────────────┐    2. Read version: 3
│ Version: 3   │
└──────┬───────┘
       │
       │ 3. Fetch RSA private key v3 from KeyManager
       ▼
┌──────────────┐    4. Decrypt AES key with RSA private key
│ RSA Private  │
│ Key (v3)     │
└──────┬───────┘
       │
       ▼
┌──────────────┐    5. Decrypt payload with AES key
│  AES Key     │
│ (decrypted)  │
└──────┬───────┘
       │
       ▼
┌──────────────┐    6. Parse decrypted JSON and trailing JSON
│ Decrypted    │
│ JSON Payload │
└──────┬───────┘
       │
       ▼
┌──────────────┐    7. Merge both JSON objects
│ Trailing     │
│ JSON         │
└──────┬───────┘
       │
       ▼
┌──────────────┐
│ Merged JSON  │  ← Complete data restored
│ Payload      │
└──────────────┘
```

### Packet Format

```
Encrypted Packet Structure:
┌────────────┬─────────────────────┬─────────────┬──────────────────────┬──────────────────┐
│  Version   │  Encrypted AES Key  │ Data Length │  Encrypted Payload   │  Trailing JSON   │
│  (2 bytes) │    (256 bytes)      │  (4 bytes)  │   (Variable length)  │  (Variable len)  │
│  uint16_BE │    RSA-encrypted    │  uint32_BE  │   AES-128-ECB        │   Unencrypted    │
└────────────┴─────────────────────┴─────────────┴──────────────────────┴──────────────────┘

Minimum packet size: 262 bytes (2 + 256 + 4 + at least some payload)

Field Descriptions:
- Version (2 bytes): Key version number in big-endian format
- Encrypted AES Key (256 bytes): AES-128 key encrypted with RSA-2048 public key
- Data Length (4 bytes): Length of encrypted payload in big-endian format
- Encrypted Payload (variable): JSON data encrypted with AES-128-ECB
- Trailing JSON (variable): Unencrypted JSON (GPS date, time, network time, etc.)

Example:
Bytes:  [0x00, 0x03, ...256 bytes..., 0x00,0x00,0x01,0x00, ...encrypted..., {...}]
         ↑     ↑      ↑               ↑                    ↑             ↑
      Version  3    Enc AES Key     DataLen=256      Enc Payload    Trailing JSON
```

## Features

- ✅ **Hybrid Encryption**: Combines RSA strength with AES speed
- ✅ **Key Versioning**: Rotate keys without breaking existing clients
- ✅ **Backward Compatibility**: Old clients continue working with old keys
- ✅ **Automatic Detection**: Identifies encrypted vs. plain packets
- ✅ **In-Memory Caching**: Keys cached for performance
- ✅ **Secure Storage**: Private keys in AWS Secrets Manager
- ✅ **Metadata Tracking**: MongoDB via IntanglesSDK
- ✅ **Multi-Version Support**: Decrypt packets from any active key version

## Components

### KeyManager
Manages RSA-2048 key pairs with versioning, rotation, and caching.

**Features:**
- IntanglesSDK-based metadata storage (zero MongoDB client dependency)
- AWS Secrets Manager backend for key storage
- In-memory caching (75% fewer DB queries)
- Key versioning with backward compatibility
- Automatic key rotation

**Performance:**
- `initialize()`: 1 DB query (cached)
- `listVersions()`: 0 DB queries (cache hit)
- `isKeyValid()`: 0 DB queries (cache hit)
- `getPublicKey()`, `getPrivateKey()`: 0 DB queries after first load

### EncryptionHandler
Handles hybrid envelope encryption/decryption of MQTT packets.

**Features:**
- Automatic encryption detection
- Hybrid RSA + AES envelope encryption
- Backward compatible (non-encrypted packets pass through)
- Version-aware decryption

**Packet Format:**
```
┌─────────────┬──────────────────────────┬─────────────┬─────────────────────┬──────────────────┐
│  Version    │  Encrypted AES Key       │ Data Length │  Encrypted Payload  │  Trailing JSON   │
│  (2 bytes)  │  (256 bytes)             │  (4 bytes)  │  (variable length)  │  (variable len)  │
└─────────────┴──────────────────────────┴─────────────┴─────────────────────┴──────────────────┘
```

## Quick Start

### 1. Generate First Key Pair

```bash
cd /home/intuser/code/platform/Backend
node scripts/mqtts/generateMQTTencryptionKeysScript.js generate
```

This creates:
- RSA-2048 key pair
- AWS secret: `mqtt-encryption-key-v1`
- Metadata document in `secret_manager_meta` collection

### 2. Initialize in Your Application

```javascript
const KeyManager = require("./Server/helpers/encryptionManager/keyManager");
const EncryptionHandler = require("./Server/helpers/encryptionManager/encryptionHandler");

const keyManager = new KeyManager({
    metadataId: "mqtt_encryption_keys_meta"
});

await keyManager.initialize();

const encryptionHandler = new EncryptionHandler(keyManager);
```

### 3. Encrypt Data

```javascript
const payload = Buffer.from("sensitive data");
const encryptedPacket = await encryptionHandler.encrypt(payload);
```

### 4. Decrypt Data

```javascript
const isEncrypted = await encryptionHandler.isEncryptedPacket(packet);

if (isEncrypted) {
    const decrypted = await encryptionHandler.decrypt(packet);
} else {
    // Handle non-encrypted packet
}
```

### 5. Rotate Keys (Periodic Maintenance)

```bash
cd /home/intuser/code/platform/Backend
node scripts/generateMQTTencryptionKeysScript.js rotate
```

## Key Rotation

### Why Rotate?

- **Compliance**: Many security standards require periodic key rotation
- **Breach Containment**: Limits exposure window if a key is compromised
- **Best Practice**: Industry standard for cryptographic key management

### Rotation Strategy

All previous key versions remain **valid forever** for decryption:

```
Timeline:
─────────────────────────────────────────────────────────>
  v1 Generated    v2 Generated    v3 Generated
      │               │               │
      ├───────────────┼───────────────┼──> v1 valid ✓
                      ├───────────────┼──> v2 valid ✓
                                      └──> v3 valid ✓ (current)
```

**New encryptions** use the current version (v3), but **old packets** encrypted with v1 or v2 can still be decrypted.

### When to Rotate

```bash
# Scheduled rotation (recommended every 90 days)
0 0 1 */3 * cd /home/intuser/code/platform/Backend && node scripts/generateMQTTencryptionKeysScript.js rotate

# After security incident
cd /home/intuser/code/platform/Backend
node scripts/generateMQTTencryptionKeysScript.js rotate

# Before major deployment
cd /home/intuser/code/platform/Backend
node scripts/generateMQTTencryptionKeysScript.js rotate
```

## API Reference

### KeyManager

```javascript
const keyManager = new KeyManager({
    metadataId: "mqtt_encryption_keys_meta",  // Document ID
    provider: "aws",                           // Secret provider
    secretPrefix: "mqtt-encryption-key"        // Secret name prefix
});

await keyManager.initialize();
```

#### Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `initialize()` | `Promise<void>` | Load metadata (call first) |
| `generateKeyPair()` | `Promise<Object>` | Generate new RSA-2048 key |
| `rotateKeys()` | `Promise<Object>` | Generate new key (keeps old valid) |
| `getPublicKey(version)` | `Promise<string>` | Get PEM public key |
| `getPrivateKey(version)` | `Promise<string>` | Get PEM private key |
| `getCurrentVersion()` | `number` | Get active version number |
| `getCurrentPublicKey()` | `Promise<Object>` | Get current public key |
| `listVersions()` | `Promise<number[]>` | List all valid versions |
| `isKeyValid(version)` | `Promise<boolean>` | Check if version exists |

### EncryptionHandler

```javascript
const handler = new EncryptionHandler(keyManager);
```

#### Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `isEncryptedPacket(buffer)` | `Promise<boolean>` | Detect encryption |
| `encrypt(buffer)` | `Promise<Buffer>` | Encrypt with current key |
| `decrypt(buffer)` | `Promise<Buffer>` | Decrypt with any valid key |

## Configuration

### Environment Variables

```bash
# AWS Credentials (for Secrets Manager)
AWS_ACCESS_KEY_ID=your_access_key
AWS_SECRET_ACCESS_KEY=your_secret_key
AWS_REGION=us-east-1

# Debug Logging
DEBUG=KeyManager*,EncryptionHandler*
```

### IntanglesSDK Configuration

Metadata is stored using `Intangles.Object.SecretManagerMeta`:

```javascript
{
    _id: "mqtt_encryption_keys_meta",
    current_version: 3,
    metadata_type: "encryption_keys",
    provider: "aws",
    versions: [
        {
            version: 1,
            secretName: "mqtt-encryption-key-v1",
            secretArn: "arn:aws:secretsmanager:...",
            created_at: 1738617600000,  // EPOCH ms
            isActive: true
        }
    ],
    created_at: 1738617600000,
    updated_at: 1738704000000
}
```

## Security Considerations

### Encryption Strength

- **RSA-2048**: Industry standard for key encapsulation
- **AES-128**: Fast symmetric encryption for payload
- **ECB Mode**: Stateless (suitable for independent packets)

### Key Storage

- **AWS Secrets Manager**: Enterprise-grade secret storage
- **Automatic Encryption**: Keys encrypted at rest by AWS KMS
- **Access Control**: IAM policies control who can access secrets

### Backward Compatibility

Old encrypted packets remain decryptable:
- ✅ No grace period - all versions valid
- ✅ Automatic version detection
- ✅ Zero downtime during rotation

### Non-Encrypted Traffic

Packets without encryption header pass through unchanged:
- ✅ Gradual migration supported
- ✅ Mixed encrypted/non-encrypted traffic
- ✅ No breaking changes for legacy clients

### Cache Strategy

1. **Metadata Cache**: Loaded once during `initialize()`
2. **Key Cache**: Lazy-loaded from AWS, kept in memory
3. **LRU Eviction**: Configurable `maxVersionsInMemory` (default: 100)

### Optimization Tips

```javascript
// ✅ DO: Initialize once per application lifecycle
await keyManager.initialize();

// ❌ DON'T: Re-initialize on every operation
for (let packet of packets) {
    await keyManager.initialize();  // SLOW!
}

// ✅ DO: Reuse EncryptionHandler instance
const handler = new EncryptionHandler(keyManager);

// ❌ DON'T: Create new instance per packet
for (let packet of packets) {
    const handler = new EncryptionHandler(keyManager);  // WASTEFUL!
}
```

## Troubleshooting

### "Must call initialize() before generating keys"

```javascript
// WRONG
const keyManager = new KeyManager();
await keyManager.generateKeyPair();  // ERROR!

// CORRECT
const keyManager = new KeyManager();
await keyManager.initialize();
await keyManager.generateKeyPair();  // OK
```

### "Key version X not found in AWS Secrets Manager"

Check AWS Secrets Manager console for secret `mqtt-encryption-key-vX`. If missing:

```bash
# Regenerate missing key version
node manageKeys.js generate
```

### High AWS Costs

Each secret costs $0.40/month. Monitor secret count:

```javascript
const versions = await keyManager.listVersions();
console.log(`Total secrets: ${versions.length}`);
```

## Testing

### Round-trip test (encryptionRoundTripTest.js)

Directly exercises `KeyManager` + `EncryptionHandler` — **no MQTT broker required**. For each key version it encrypts the test payload in-process, immediately decrypts it via `decryptPacket()`, and verifies the output matches field-by-field.

```bash
cd Backend

# Test all versions found in KeyManager
node Utils/encryptionManager/encryptionRoundTripTest.js

# Test a single specific version
VERSION=1 node Utils/encryptionManager/encryptionRoundTripTest.js   # aes-128-ecb
VERSION=2 node Utils/encryptionManager/encryptionRoundTripTest.js   # aes-256-ecb
VERSION=3 node Utils/encryptionManager/encryptionRoundTripTest.js   # aes-192-ecb
```

Sample output:

```
============================================================
ENCRYPTION ROUND-TRIP TEST
============================================================

Testing versions from KeyManager: 1, 2, 3

Results:
  [PASS] v1 (aes-128-ecb) — packet 611B → decrypted 138B (4ms)
  [PASS] v2 (aes-256-ecb) — packet 627B → decrypted 138B (3ms)
  [PASS] v3 (aes-192-ecb) — packet 619B → decrypted 138B (3ms)

============================================================
SUMMARY  passed: 3  failed: 0  skipped: 0
============================================================
```

A version is **skipped** (not a failure) when KeyManager has no key registered for it. Exit code is `1` if any version fails — suitable for CI.

#### Environment variable

| Variable | Default | Description |
|----------|---------|-------------|
| `VERSION` | _(not set)_ | Test only this version. When not set, all versions from KeyManager are tested. |

#### AES algorithm by key version

| Version | Key size | Algorithm |
|---------|----------|-----------|
| 1 | 16 bytes | aes-128-ecb |
| 2 | 32 bytes | aes-256-ecb |
| 3 | 24 bytes | aes-192-ecb |

#### Enable debug logging

```bash
DEBUG=EncryptionHandler*,KeyManager* VERSION=2 node Utils/encryptionManager/encryptionRoundTripTest.js
```

## Cost Analysis

### AWS Secrets Manager Pricing

- **Storage**: $0.40/month per secret
- **API Calls**: $0.05 per 10,000 API calls
