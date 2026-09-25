#!/usr/bin/env node
// Decrypts AppData_encrypted*.db and compares with AppData_unencrypted*.db row by row.
// Usage:
//   node compare-dbs.js
//   node compare-dbs.js --enc ./output/AppData_encrypted5.db --unc ./output/AppData_unencrypted7.db
//   node compare-dbs.js --private-key ./keys/private-latest.pem
"use strict";

const fs       = require("fs");
const path     = require("path");
const crypto   = require("crypto");
const Database = require("better-sqlite3");

// ── defaults ──────────────────────────────────────────────────────────────────
const DEFAULT_ENC  = path.join(__dirname, "output", "AppData_encrypted5.db");
const DEFAULT_UNC  = path.join(__dirname, "output", "AppData_unencrypted7.db");
const DEFAULT_PRIV = path.join(__dirname, "keys", "private-latest.pem");

const DEFAULT_AES_128_KEY = Buffer.from("intangles128key!", "utf8");
const DEFAULT_AES_256_KEY = Buffer.from("intangles256keydefault_v2_global", "utf8");
const VERSION_AES = { 1: { keySize: 16, algo: "aes-128-ecb" }, 2: { keySize: 32, algo: "aes-256-ecb" } };

// ── CLI args ──────────────────────────────────────────────────────────────────
function parseArgs(argv) {
    const out = {};
    for (let i = 2; i < argv.length; i++) {
        const arg = argv[i];
        if (!arg.startsWith("--")) continue;
        const eq = arg.indexOf("=");
        if (eq !== -1) { out[arg.slice(2, eq)] = arg.slice(eq + 1); }
        else if (i + 1 < argv.length && !argv[i + 1].startsWith("--")) { out[arg.slice(2)] = argv[++i]; }
        else { out[arg.slice(2)] = "true"; }
    }
    return out;
}
const cli     = parseArgs(process.argv);
const encPath = cli.enc         || DEFAULT_ENC;
const uncPath = cli.unc         || DEFAULT_UNC;
const privKey = cli["private-key"] || DEFAULT_PRIV;

if (!fs.existsSync(privKey)) {
    console.error(`Private key not found: ${privKey}`);
    process.exit(1);
}
const privateKeyPem = fs.readFileSync(privKey, "utf8");

// ── decrypt one BLOB row ──────────────────────────────────────────────────────
function decryptBlob(blob) {
    let offset = 0;

    const version = blob.readUInt16BE(offset); offset += 2;
    const cfg = VERSION_AES[version];
    if (!cfg) throw new Error(`Unknown AES version: ${version}`);

    const encAesKey = blob.slice(offset, offset + 256); offset += 256;
    const dataLen   = blob.readUInt32BE(offset);        offset += 4;
    const encData   = blob.slice(offset, offset + dataLen); offset += dataLen;
    // trailing JSON (DE/TE/TN) is after encData — ignored for comparison

    // Decrypt AES key with RSA private key
    const aesKey = crypto.privateDecrypt(
        { key: privateKeyPem, padding: crypto.constants.RSA_PKCS1_OAEP_PADDING, oaepHash: "sha1" },
        encAesKey
    );

    // Decrypt payload with AES-ECB (no built-in padding — was manually null-padded)
    const decipher = crypto.createDecipheriv(cfg.algo, aesKey, null);
    decipher.setAutoPadding(false);
    const decrypted = Buffer.concat([decipher.update(encData), decipher.final()]);

    // Strip null padding and parse JSON
    let end = decrypted.length;
    while (end > 0 && decrypted[end - 1] === 0) end--;
    return decrypted.slice(0, end).toString("utf8");
}

// ── main ──────────────────────────────────────────────────────────────────────
console.log(`\nEncrypted DB:   ${encPath}`);
console.log(`Unencrypted DB: ${uncPath}`);
console.log(`Private key:    ${privKey}\n`);

const encDb = new Database(encPath, { readonly: true });
const uncDb = new Database(uncPath, { readonly: true });

const encRows = encDb.prepare("SELECT DNO, DATASTRING FROM oData ORDER BY DNO").all();
const uncRows = uncDb.prepare("SELECT DNO, DATASTRING FROM oData ORDER BY DNO").all();

encDb.close();
uncDb.close();

console.log(`Encrypted rows: ${encRows.length}`);
console.log(`Unencrypted rows: ${uncRows.length}`);
if (encRows.length !== uncRows.length) {
    console.log(`\n⚠  Row count mismatch! encrypted=${encRows.length} unencrypted=${uncRows.length}`);
}

const count   = Math.min(encRows.length, uncRows.length);
let matches   = 0, mismatches = 0, errors = 0;

for (let i = 0; i < count; i++) {
    const encRow = encRows[i];
    const uncRow = uncRows[i];
    let decrypted;
    try {
        decrypted = decryptBlob(Buffer.isBuffer(encRow.DATASTRING) ? encRow.DATASTRING : Buffer.from(encRow.DATASTRING));
    } catch (e) {
        console.log(`DNO ${encRow.DNO}  DECRYPT ERROR: ${e.message}`);
        errors++;
        continue;
    }

    if (decrypted === uncRow.DATASTRING) {
        matches++;
    } else {
        mismatches++;
        console.log(`\nDNO ${encRow.DNO}  MISMATCH`);
        console.log(`  encrypted→decrypted: ${decrypted.slice(0, 120)}`);
        console.log(`  unencrypted:         ${uncRow.DATASTRING.slice(0, 120)}`);
    }
}

console.log(`\n${"─".repeat(50)}`);
console.log(`Matches:    ${matches}`);
console.log(`Mismatches: ${mismatches}`);
console.log(`Errors:     ${errors}`);
if (mismatches === 0 && errors === 0 && encRows.length === uncRows.length) {
    console.log(`\n✓ Both DBs contain identical data.`);
} else {
    console.log(`\n✗ DBs differ — see details above.`);
}
