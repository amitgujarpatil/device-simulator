#!/usr/bin/env node
// Generates a fresh RSA-2048 key pair and writes rsa_public.pem + rsa_private.pem
// to the build-sqlite directory. Run once before using fetch-and-store.js.
//
//   node generate-keypair.js
//   node generate-keypair.js --out /path/to/dir
"use strict";
const crypto = require("crypto");
const fs     = require("fs");
const path   = require("path");

const defaultKeysDir = require("path").join(__dirname, "keys");
const outDir = process.argv.includes("--out")
    ? process.argv[process.argv.indexOf("--out") + 1]
    : defaultKeysDir;

if (!require("fs").existsSync(outDir)) require("fs").mkdirSync(outDir, { recursive: true });

const pubPath  = path.join(outDir, "rsa_public.pem");
const privPath = path.join(outDir, "rsa_private.pem");

if (fs.existsSync(pubPath) || fs.existsSync(privPath)) {
    console.error("Key files already exist:");
    if (fs.existsSync(pubPath))  console.error("  " + pubPath);
    if (fs.existsSync(privPath)) console.error("  " + privPath);
    console.error("Delete them first if you want to regenerate.");
    process.exit(1);
}

console.log("Generating RSA-2048 key pair...");
const { publicKey, privateKey } = crypto.generateKeyPairSync("rsa", {
    modulusLength: 2048,
    publicKeyEncoding:  { type: "spki",  format: "pem" },
    privateKeyEncoding: { type: "pkcs8", format: "pem" },
});

fs.writeFileSync(pubPath,  publicKey,  { mode: 0o644 });
fs.writeFileSync(privPath, privateKey, { mode: 0o600 });

console.log("Done:");
console.log("  Public key:  " + pubPath);
console.log("  Private key: " + privPath + "  (keep safe — needed for decryption)");
console.log("\nUse with fetch-and-store.js:");
console.log("  node fetch-and-store.js   # keys/public-latest.pem is picked up automatically");
