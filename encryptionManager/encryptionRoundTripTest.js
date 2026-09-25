#!/usr/bin/env node
/* eslint-disable no-console */

/**
 * encryptionRoundTripTest.js
 *
 * Directly exercises EncryptionHandler + KeyManager without any MQTT/broker dependency.
 * For each requested key version it:
 *   1. Loads the RSA public key from KeyManager
 *   2. Encrypts the test payload using the version's AES-ECB algorithm
 *   3. Calls encryptionHandler.decryptPacket() to decrypt
 *   4. Deep-compares the result against the original JSON
 *   5. Reports PASS / FAIL with timing
 *
 * Usage:
 *   node encryptionRoundTripTest.js              # test all versions found in KeyManager
 *   VERSION=2 node encryptionRoundTripTest.js    # test a specific version only
 */

const crypto = require("crypto");

// AES-ECB config per version — must mirror encryptionHandler.js VERSION_AES_CONFIG
const VERSION_AES_CONFIG = {
    1: { keySize: 16, algorithm: "aes-128-ecb" },
    2: { keySize: 32, algorithm: "aes-256-ecb" },
};

// Sample OBD payload used across all test runs
const TEST_PAYLOAD = {
    "T": "O",
    "GD": "02082024",
    "GT": "200328",
    "F": "3",
    "GA": "2739.376608 N,09938.222664 W|2739.376620 N,09938.222652 W",
    "NS": 31,
    "DP": 0.47,
    "ALT": "139.20",
    "DC": 1,
    "IG": 1,
    "IBV": 1624,
    "EBV": 2030,
    "ST": 18,
};

// ---------------------------------------------------------------------------
// Encrypt a payload using hybrid envelope encryption (mirrors firmware format)
// Returns: Buffer  [version(2) | rsaEncAesKey(256) | dataLen(4) | aesPayload | trailingJson]
// ---------------------------------------------------------------------------
function buildEncryptedPacket(payload, keyVersion, publicKeyPem) {
    const aesConfig = VERSION_AES_CONFIG[keyVersion] || VERSION_AES_CONFIG[1];
    const rawBuffer = Buffer.from(JSON.stringify(payload), "utf8");

    // AES key — size matches the version
    const aesKey = crypto.randomBytes(aesConfig.keySize);

    // Pad to 16-byte block boundary (null-byte padding, matches openssl -nopad)
    const blockSize = 16;
    const paddedLength = Math.ceil(rawBuffer.length / blockSize) * blockSize;
    const padded = Buffer.alloc(paddedLength, 0);
    rawBuffer.copy(padded);

    // Encrypt payload
    const cipher = crypto.createCipheriv(aesConfig.algorithm, aesKey, null);
    cipher.setAutoPadding(false);
    const encryptedPayload = Buffer.concat([ cipher.update(padded), cipher.final() ]);

    // RSA-OAEP/SHA-1 encrypt the AES key (matches PSA_ALG_RSA_OAEP(PSA_ALG_SHA_1) in firmware)
    const encryptedAesKey = crypto.publicEncrypt(
        { key: publicKeyPem, padding: crypto.constants.RSA_PKCS1_OAEP_PADDING, oaepHash: "sha1" },
        aesKey
    );

    // Trailing JSON (DE/TE/TN) — appended unencrypted
    const now = new Date();
    const trailingJson = {
        DE: now.toISOString().split("T")[0].replace(/-/g, ""),
        TE: now.toTimeString().split(" ")[0].replace(/:/g, ""),
        TN: String(now.getTime()),
    };

    // Assemble packet
    const versionBuf = Buffer.allocUnsafe(2);
    versionBuf.writeUInt16BE(keyVersion, 0);

    const dataLenBuf = Buffer.allocUnsafe(4);
    dataLenBuf.writeUInt32BE(encryptedPayload.length, 0);

    return Buffer.concat([
        versionBuf,
        encryptedAesKey,
        dataLenBuf,
        encryptedPayload,
        Buffer.from(JSON.stringify(trailingJson), "utf8"),
    ]);
}

// ---------------------------------------------------------------------------
// Deep-equal check (only compares keys present in expected — trailing JSON
// keys like DE/TE/TN will be in actual but not in expected, that is fine)
// ---------------------------------------------------------------------------
function assertPayloadMatch(expected, actual) {
    const mismatches = [];
    for (const key of Object.keys(expected)) {
        if (JSON.stringify(actual[key]) !== JSON.stringify(expected[key])) {
            mismatches.push(`  ${key}: expected ${JSON.stringify(expected[key])}, got ${JSON.stringify(actual[key])}`);
        }
    }
    return mismatches;
}

// ---------------------------------------------------------------------------
// Run one round-trip test for a single version
// ---------------------------------------------------------------------------
async function runVersion(version, encryptionHandler, keyManager) {
    const label = `v${version} (${(VERSION_AES_CONFIG[version] || {}).algorithm || "unknown"})`;

    const isValid = await keyManager.isKeyValid(version);
    if (!isValid) {
        console.log(`  [SKIP] ${label} — key not found in KeyManager`);
        return { version, result: "skip" };
    }

    const publicKey = await keyManager.getPublicKey(version);
    if (!publicKey) {
        console.log(`  [SKIP] ${label} — public key unavailable`);
        return { version, result: "skip" };
    }

    const start = Date.now();

    // Step 1: encrypt
    let packet;
    try {
        packet = buildEncryptedPacket(TEST_PAYLOAD, version, publicKey);
    } catch (err) {
        console.log(`  [FAIL] ${label} — encrypt error: ${err.message}`);
        return { version, result: "fail", error: err.message };
    }

    // Step 2: decrypt via EncryptionHandler
    let decryptedBuffer;
    try {
        decryptedBuffer = await encryptionHandler.decryptPacket(packet);
    } catch (err) {
        console.log(`  [FAIL] ${label} — decrypt error: ${err.message}`);
        return { version, result: "fail", error: err.message };
    }

    // Step 3: parse and compare
    let decrypted;
    try {
        decrypted = JSON.parse(decryptedBuffer.toString("utf8"));
    } catch (err) {
        console.log(`  [FAIL] ${label} — JSON parse error: ${err.message}`);
        return { version, result: "fail", error: err.message };
    }

    const mismatches = assertPayloadMatch(TEST_PAYLOAD, decrypted);
    const elapsed = Date.now() - start;

    if (mismatches.length > 0) {
        console.log(`  [FAIL] ${label} (${elapsed}ms) — field mismatches:\n${mismatches.join("\n")}`);
        return { version, result: "fail" };
    }

    console.log(`  [PASS] ${label} — packet ${packet.length}B → decrypted ${decryptedBuffer.length}B (${elapsed}ms)`);
    return { version, result: "pass" };
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------
async function main() {
    console.log("=".repeat(60));
    console.log("ENCRYPTION ROUND-TRIP TEST");
    console.log("=".repeat(60), "\n");

    const cn = require("@intangles/berries/configuration/config");
    await cn.init();

    const KeyManager = require("./keyManager");
    const EncryptionHandler = require("./encryptionHandler");

    const keyManager = new KeyManager();
    await keyManager.initialize();

    const encryptionHandler = new EncryptionHandler(keyManager);

    // Determine which versions to test
    const versionOverride = process.env.VERSION ? parseInt(process.env.VERSION) : 0;
    let versionsToTest;

    if (versionOverride > 0) {
        versionsToTest = [ versionOverride ];
        console.log(`Testing version: ${versionOverride} (VERSION override)\n`);
    } else {
        const available = await keyManager.listVersions();
        // Test all versions that are both in VERSION_AES_CONFIG and in KeyManager
        versionsToTest = available
            .map(v => (typeof v === "object" ? v.version : v))
            .filter(v => VERSION_AES_CONFIG[v]);
        console.log(`Testing versions from KeyManager: ${versionsToTest.join(", ")}\n`);
    }

    if (versionsToTest.length === 0) {
        console.log("No versions to test — check KeyManager has at least one valid key.");
        process.exit(1);
    }

    // Show test payload
    console.log("Test payload:");
    console.log(JSON.stringify(TEST_PAYLOAD, null, 2), "\n");
    console.log("Results:");

    const results = [];
    for (const version of versionsToTest) {
        const r = await runVersion(version, encryptionHandler, keyManager);
        results.push(r);
    }

    // Summary
    const passed = results.filter(r => r.result === "pass").length;
    const failed = results.filter(r => r.result === "fail").length;
    const skipped = results.filter(r => r.result === "skip").length;

    console.log("\n" + "=".repeat(60));
    console.log(`SUMMARY  passed: ${passed}  failed: ${failed}  skipped: ${skipped}`);
    console.log("=".repeat(60));

    process.exit(failed > 0 ? 1 : 0);
}

main().catch(err => {
    console.error("[Fatal]", err.message);
    process.exit(1);
});
