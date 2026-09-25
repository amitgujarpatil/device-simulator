"use strict";
const crypto = require("crypto");

// Mirrors VERSION_AES_CONFIG in encryptionHandler.js — keep in sync
const VERSION_AES_CONFIG = {
    1: { keySize: 16, algorithm: "aes-128-ecb" },
    2: { keySize: 32, algorithm: "aes-256-ecb" },
};

// ── Default fixed AES keys ────────────────────────────────────────────────────
// These are used for every packet when no key is passed explicitly.
// Override at runtime via env vars (hex-encoded, exact byte length required):
//   AES_KEY_128=<32 hex chars>   e.g. AES_KEY_128=696e74616e676c657331323821
//   AES_KEY_256=<64 hex chars>   e.g. AES_KEY_256=696e74616e676c65733235366b6579...
const DEFAULT_AES_128_KEY = process.env.AES_KEY_128
    ? Buffer.from(process.env.AES_KEY_128, "hex")
    : Buffer.from("intangles128key!", "utf8");   // 16 bytes

const DEFAULT_AES_256_KEY = process.env.AES_KEY_256
    ? Buffer.from(process.env.AES_KEY_256, "hex")
    : Buffer.from("intangles256keydefault_v2_global", "utf8");  // 32 bytes

const DEFAULT_AES_KEYS = { 1: DEFAULT_AES_128_KEY, 2: DEFAULT_AES_256_KEY };

/**
 * Encrypt a telemetry payload using the hybrid envelope scheme.
 *
 * Packet layout (big-endian, matches encryptionHandler.js + firmware):
 *   [version uint16BE 2B | rsaEncAesKey 256B | dataLen uint32BE 4B | aesPayload varB | trailingJson varB]
 *
 * @param {string|object} payload    - Telemetry object or JSON string to encrypt
 * @param {string}        publicKeyPem - RSA-2048 public key in PEM format
 * @param {number}        version     - AES version: 1 (128-bit) or 2 (256-bit). Default 2.
 * @param {Buffer|null}   aesKey      - AES key to use; null = use default for version.
 * @returns {Buffer}  Complete binary packet ready to store as BLOB
 */
function encryptPacket(payload, publicKeyPem, version = 2, aesKey = null) {
    const aesConfig = VERSION_AES_CONFIG[version] || VERSION_AES_CONFIG[1];

    // Serialise payload to UTF-8 bytes
    const rawBuffer = Buffer.from(
        typeof payload === "string" ? payload : JSON.stringify(payload),
        "utf8"
    );

    // Use provided key, default fixed key, or fall back to random (safety net)
    const key = aesKey || DEFAULT_AES_KEYS[version] || crypto.randomBytes(aesConfig.keySize);
    if (key.length !== aesConfig.keySize) {
        throw new Error(
            `AES key for version ${version} must be ${aesConfig.keySize} bytes, got ${key.length}`
        );
    }
    const resolvedKey = key;

    // Null-pad raw payload to the nearest 16-byte block boundary
    const blockSize = 16;
    const paddedLength = Math.ceil(rawBuffer.length / blockSize) * blockSize;
    const padded = Buffer.alloc(paddedLength, 0);
    rawBuffer.copy(padded);

    // AES-ECB encrypt (no built-in padding — we padded manually above)
    const cipher = crypto.createCipheriv(aesConfig.algorithm, resolvedKey, null);
    cipher.setAutoPadding(false);
    const encryptedPayload = Buffer.concat([cipher.update(padded), cipher.final()]);

    // RSA-OAEP/SHA-1 encrypt the AES key (matches PSA_ALG_RSA_OAEP(PSA_ALG_SHA_1) in firmware)
    const encryptedAesKey = crypto.publicEncrypt(
        {
            key: publicKeyPem,
            padding: crypto.constants.RSA_PKCS1_OAEP_PADDING,
            oaepHash: "sha1",
        },
        resolvedKey
    );

    // Pack: version (2B BE) | rsaEncKey (256B) | dataLen (4B BE) | encPayload
    const versionBuf = Buffer.allocUnsafe(2);
    versionBuf.writeUInt16BE(version, 0);

    const dataLenBuf = Buffer.allocUnsafe(4);
    dataLenBuf.writeUInt32BE(encryptedPayload.length, 0);

    return Buffer.concat([
        versionBuf,
        encryptedAesKey,
        dataLenBuf,
        encryptedPayload,
    ]);
}

module.exports = { encryptPacket, VERSION_AES_CONFIG, DEFAULT_AES_128_KEY, DEFAULT_AES_256_KEY };
