#!/usr/bin/env node
//
// simulate-device.js — History-replay + live-mode device simulation.
//
// Defaults are aligned with pipeline.js (FROM_MS / UNTIL_MS / TARGET_IMEI)
// and fetch-and-store.js (IMEI / TOKEN / PUBLIC_KEY_PATH).
//
// Run:
//   node simulate-device.js --user-token <token>
//   node simulate-device.js --user-token <token> --send-encrypted false
//   node simulate-device.js --dry-run                      # no uploads, no real MQTT
//   npm run simulate -- --user-token <token>
//
// All options (CLI flag / env var / built-in default):
//   --imei               IMEI                    869305070942563
//   --target-imei        TARGET_IMEI             869305077523101
//   --from               FROM_MS                 1789756200000
//   --until              UNTIL_MS                1789842600000
//   --history-end        HISTORY_END_MS          FROM_MS + 12h
//   --batch-size         BATCH_SIZE              30
//   --batch-delay        BATCH_UPLOAD_DELAY_MS   120000
//   --gps-l1-interval    GPS_L1_INTERVAL_MS      10000
//   --obd-accum-interval OBD_ACCUM_INTERVAL_MS   120000
//   --normal-interval    NORMAL_INTERVAL_MS      60000
//   --send-encrypted     SEND_ENCRYPTED          true
//   --public-key         PUBLIC_KEY_PATH         keys/public-latest.pem
//   --aes-version        AES_VERSION             2
//   --upload-url         UPLOAD_URL              https://device-history-server...
//   --user-token         USER_TOKEN              (required)
//   --session-token      SESSION_TOKEN           auto-generated UUID
//   --mqtt-host          MQTT_HOST               mqttsecure.intangles-aws-eu-north-1...
//   --mqtt-port          MQTT_PORT               1884
//   --mqtt-cert-dir      MQTT_CERT_DIR           /home/.../eu_region_client_certs/
//   --token              API_TOKEN               <built-in>
//   --psize              PSIZE                   1000
//   --delay              DELAY_MS                300
//   --out-dir            OUT_DIR                 ./sim_output
//   --dry-run            DRY_RUN                 false  (skip all uploads + MQTT, safe to test)
//   --test-mqtt                                         connect + publish one ping, then exit

"use strict";

const fs       = require("fs");
const path     = require("path");
const crypto   = require("crypto");
const Database = require("better-sqlite3");
const mqtt     = require("mqtt");
const { encryptPacket } = require("./encryptionManager/encryptionEngine");

// ── Built-in defaults (aligned with pipeline.js and fetch-and-store.js) ───────
const BUILTIN_API_HOST              = "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const BUILTIN_IMEI                  = "869305070942563";           // from fetch-and-store.js
const BUILTIN_TARGET_IMEI           = "869305073861125";           // from pipeline.js
const BUILTIN_FROM_MS               = "1790101800000";             // 24-hour window start
const BUILTIN_UNTIL_MS              = "1790188200000";             // 24-hour window end
const BUILTIN_TOKEN                 = "RlOLZw2on9fP2edIOuGeQbZBGuxxBnUBEXkwLpiec1H9cCO6wsDo4mmOvMwgXUhD";
const BUILTIN_BATCH_SIZE            = 30;                          // records per historic SQLite file
const BUILTIN_BATCH_UPLOAD_DELAY_MS = 120000;
const BUILTIN_GPS_L1_INTERVAL_MS    = 10000;
const BUILTIN_OBD_ACCUM_INTERVAL_MS = 120000;
const BUILTIN_NORMAL_INTERVAL_MS    = 60000;
const BUILTIN_SEND_ENCRYPTED        = true;
const BUILTIN_PUBLIC_KEY_PATH       = path.join(__dirname, "keys", "public-latest.pem"); // from fetch-and-store.js
const BUILTIN_AES_VERSION           = 2;
const BUILTIN_UPLOAD_URL            = "https://device-history-server.intangles-aws-eu-north-1.eu.intangles.com/upload";
const BUILTIN_MQTT_HOST             = "mqttsecure.intangles-aws-eu-north-1.eu.intangles.com";
const BUILTIN_MQTT_PORT             = 8884;
const BUILTIN_MQTT_CERT_DIR         = "/home/amitgujar/Desktop/Desktop-prev/eu_region_client_certs/";
const BUILTIN_PSIZE                 = 1000;

// ── Embedded keys / certs (used when no override path is provided) ─────────────
// RSA-2048 public key for SQLite payload encryption (matches keys/public-latest.pem)
const EMBEDDED_RSA_PUBLIC_KEY = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAumPKm+0/ZUkMkTSWBnML
US+rihCji7XItpqNupDTP4vNZqLf6dgNonSAF6VU+p30YVNOIZ17WRDRhvzhJ0ag
GxapPUkATx4j/EHPEjCuC5y8GoUc3deP8nT7UiGim/lUPJ4J9lybSLen309gI61a
8pxf3mQZDxKNKW6u5D2XYb5469gdPVp1X44J1/+uuF5+e/MszFUt/+a5Ob5PZn39
WIm8ipXjedF4o6WAH2xPpWqIxGsUEOFQDH2DNG+gsTf4I6pP3iJnE65Y112M0tia
kkD6ts6q4uXFnr0wXBAsnSTsf78SAEn4wQdq2tw6W24IR4K8gG3XxqLsCkoXwOvi
FwIDAQAB
-----END PUBLIC KEY-----`;

// MQTT TLS: CA certificate (eu_region_client_certs2/ca-crt.pem)
const EMBEDDED_MQTT_CA = `-----BEGIN CERTIFICATE-----
MIIDhTCCAm2gAwIBAgIUKBU3E6ANhynqIYrhCcemSe2VfVwwDQYJKoZIhvcNAQEL
BQAwUjELMAkGA1UEBhMCQVUxEzARBgNVBAgMClNvbWUtU3RhdGUxITAfBgNVBAoM
GEludGVybmV0IFdpZGdpdHMgUHR5IEx0ZDELMAkGA1UEAwwCY2EwHhcNMjYwOTIy
MDk1MzU1WhcNMzEwOTIzMDk1MzU1WjBSMQswCQYDVQQGEwJBVTETMBEGA1UECAwK
U29tZS1TdGF0ZTEhMB8GA1UECgwYSW50ZXJuZXQgV2lkZ2l0cyBQdHkgTHRkMQsw
CQYDVQQDDAJjYTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBANgqtQxi
YAVrmzu6H0/J7G5H4hL4UBRTRHizdIfrC6vbS0lMBHmdp38jeMnUrFwPrMUEaktm
uCAkSRfoZSFHz15jv03s0bNhjjdcB6dzXfvUTw5hL+dUS1Alo+HKokRnSG+zhRaB
h77cff5D6pqzz7CXvDOCmZ0mgHwe2C6tiRbz6kfFf0Lz+Zav6bfss95tCQbSlJdD
Uk9QclYs1JPJun6eNkpxyzUmxcweWGuC4xNMQJxME5pPx1ltIsOatM4grIKvmUU7
HfGS1GZ74I1IyrL1DpPf5i4Lq7s8Kv28bRMY6SZLbvmXYl/WZ9QFjAsjPospJCMh
hAE9bbSTrQnoKWECAwEAAaNTMFEwHQYDVR0OBBYEFI/9KVj5Sp07MlIVI53xzpVz
j+JIMB8GA1UdIwQYMBaAFI/9KVj5Sp07MlIVI53xzpVzj+JIMA8GA1UdEwEB/wQF
MAMBAf8wDQYJKoZIhvcNAQELBQADggEBAMTV6iWDo8iOnYiuBMyo+lUxWzfBeP6M
3gbSXn+CXUvyFQ+/+c3kkQhTM12yrYoSX9OAnvYGoQ9XbMJ29SMmqDFnxL51Z0Pm
oGrJlUGiaOMDYrq3gPmLKSTVEIC8+30FW73dv6VCUoePldvwU+eBi6AmlpPyVbHl
sVDompGUq5QT6CjGltXlNCsVvtML3bIxfuDDt6YfofHMa1BHa0h8DTu029J4Q/DV
qf6sMSQIHT7hIi7ha8rTDlUN5rtm+7x3EymkEaSFPa0BbqUnx0OmUHppTyxo5xJN
1Hic3A0h7QH9tBnR9K6+78v/7gKmhJ+CBPHCFkBCp1ZS8eJbrEs/G38=
-----END CERTIFICATE-----`;

// MQTT TLS: client certificate (eu_region_client_certs2/eu-client-cert-crt.pem)
const EMBEDDED_MQTT_CERT = `-----BEGIN CERTIFICATE-----
MIIEAzCCAusCFAJr7V8qjx1lB0BLtks4xU/qnIQqMA0GCSqGSIb3DQEBCwUAMFIx
CzAJBgNVBAYTAkFVMRMwEQYDVQQIDApTb21lLVN0YXRlMSEwHwYDVQQKDBhJbnRl
cm5ldCBXaWRnaXRzIFB0eSBMdGQxCzAJBgNVBAMMAmNhMB4XDTI2MDkyMjA5NTQy
M1oXDTMxMDkyMzA5NTQyM1owKjEoMCYGA1UEAwwfZXUtY2xpZW50LWNlcnQuY2xp
ZW50LmxvY2FsaG9zdDCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAOPN
/myp2q6cj2zsAJLHyzBNFJ3u1ayhVxm9qNH0dMFqGQ+4AbQXUxbfofZy6r3po2eX
jYVbqfF5bp0s9/lVB+eYj6r6WrqRKEcxupBUvVuHr9CAiypUcNwuxZnptpCo0a9O
C0zaYwdC15dK6CLdE/Wy7rHAHYh8vwXonipaUcDHVRriPYy6m5ImlKhD7K3+p+sO
2WyxfSxfN3b+Lzt7IjhKTCOfSBfMCsmPsEz7V6UMDDkfffT/7jHDlR7oVDmVOh24
8+FSA2KCfQqZ+by5brSQYqirUhipkLOgW0DCjFCd9X/7yN1rlYV3n7orqSk8zguG
FEc+q3zuxdnesv04w7liOaOVRQl8P+fRQX0cowsssLyZR8gnyxZm0sqmRY7A37pD
1IAO3JuQiJiQgamrXbY2YeAKd2QthwFo9ewSZo/5Ix2/Yfd8IaYsp9Nf5EWb5gSQ
jN/aiFNVbbCHazz1sPKVQ78mQ3E2Lxdg4J4m7Zuqm/RKbrXAHjCLVPOgDY2CUN7U
FbSaGoXJNPvu3WRavc0CD9wsq79iDYYBe6SaI1RUfQ7TYBMtDyN8wIeNRIa5mnzC
xBTfak97VMGqmGY9tQRahzwGYler4yxVd/BzpgSCwEgPAMtw1zf0LPVjzonmoHXR
vXKUesT82qO9lvwzE2WSaITiALRvk8jqH9E5N/ohAgMBAAEwDQYJKoZIhvcNAQEL
BQADggEBAFhNCeY/Jib6lhuc7I52UPfHk/snMjyCp01cbj6rySSQxBzJGfUJQEUg
YiGEsWX4glN9AJwINfN2+VtnVA2bZBPEfYrgGSBA5gv+w1J52q3W7xFxQIYGlgLe
bwNLOoAkDlnj3/L1KIee3bpp20fD8H4PvyrKAzRv8QvikJal0Fj2XDatyjVa2nOE
U3yrcOhX3nURVb4AE60gXiWvHVMH6v8+OCOW3RfXH4s56y0Btq7kqezYHSfs0uZF
rdILbOva4EUAys9XkmU/jBi0qyv9LECquHvNBbbv8Uk0wtmp+63x0aCVNdhHAU5I
RcMrY++3ljpYjBISDFM5y4/g6m6KHSQ=
-----END CERTIFICATE-----`;

// MQTT TLS: client private key (eu_region_client_certs/eu-client-cert-key.pem)
const EMBEDDED_MQTT_KEY = `-----BEGIN RSA PRIVATE KEY-----
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
-----END RSA PRIVATE KEY-----`;
const BUILTIN_DELAY_MS              = 300;
const MAX_RETRIES                   = 3;
const RETRY_DELAY_MS                = 2000;

// ── CLI arg parser ─────────────────────────────────────────────────────────────
function parseCliArgs(argv) {
    const out = {};
    for (let i = 2; i < argv.length; i++) {
        const arg = argv[i];
        if (!arg.startsWith("--")) continue;
        const eq = arg.indexOf("=");
        if (eq !== -1) {
            out[arg.slice(2, eq)] = arg.slice(eq + 1);
        } else if (i + 1 < argv.length && !argv[i + 1].startsWith("--")) {
            out[arg.slice(2)] = argv[++i];
        } else {
            out[arg.slice(2)] = "true";
        }
    }
    return out;
}
const cli = parseCliArgs(process.argv);

function opt(cliKey, envKey, fallback) {
    return cli[cliKey] !== undefined ? cli[cliKey]
         : process.env[envKey]       ? process.env[envKey]
         : fallback;
}

// ── Resolved options ───────────────────────────────────────────────────────────
const API_HOST              = opt("host",              "API_HOST",              BUILTIN_API_HOST);
const IMEI                  = opt("imei",              "IMEI",                  BUILTIN_IMEI);
const TARGET_IMEI           = opt("target-imei",       "TARGET_IMEI",           BUILTIN_TARGET_IMEI);
const TOKEN                 = opt("token",             "API_TOKEN",             BUILTIN_TOKEN);
const FROM_MS               = opt("from",              "FROM_MS",               BUILTIN_FROM_MS);
const UNTIL_MS              = opt("until",             "UNTIL_MS",              BUILTIN_UNTIL_MS);
const _defaultHistoryEnd    = String(Number(FROM_MS) + 12 * 3600000);
const HISTORY_END_MS        = opt("history-end",       "HISTORY_END_MS",        _defaultHistoryEnd);
const BATCH_SIZE            = parseInt(opt("batch-size",          "BATCH_SIZE",            String(BUILTIN_BATCH_SIZE)), 10);
const BATCH_UPLOAD_DELAY_MS = parseInt(opt("batch-delay",         "BATCH_UPLOAD_DELAY_MS", String(BUILTIN_BATCH_UPLOAD_DELAY_MS)), 10);
const GPS_L1_INTERVAL_MS    = parseInt(opt("gps-l1-interval",     "GPS_L1_INTERVAL_MS",    String(BUILTIN_GPS_L1_INTERVAL_MS)), 10);
const OBD_ACCUM_INTERVAL_MS = parseInt(opt("obd-accum-interval",  "OBD_ACCUM_INTERVAL_MS", String(BUILTIN_OBD_ACCUM_INTERVAL_MS)), 10);
const NORMAL_INTERVAL_MS    = parseInt(opt("normal-interval",     "NORMAL_INTERVAL_MS",    String(BUILTIN_NORMAL_INTERVAL_MS)), 10);
const SEND_ENCRYPTED        = opt("send-encrypted", "SEND_ENCRYPTED", String(BUILTIN_SEND_ENCRYPTED)) !== "false";
const PUBLIC_KEY_PATH       = opt("public-key",     "PUBLIC_KEY_PATH", BUILTIN_PUBLIC_KEY_PATH);
const AES_VERSION           = parseInt(opt("aes-version", "AES_VERSION", String(BUILTIN_AES_VERSION)), 10);
const UPLOAD_URL            = opt("upload-url",    "UPLOAD_URL",    BUILTIN_UPLOAD_URL);
const USER_TOKEN            = opt("user-token",    "USER_TOKEN",    "");
const SESSION_TOKEN         = opt("session-token", "SESSION_TOKEN", "1B2dG2mhIV0IjdlOGLCfEhKCsXLb6V");
const MQTT_HOST             = opt("mqtt-host",     "MQTT_HOST",     BUILTIN_MQTT_HOST);
const MQTT_PORT             = parseInt(opt("mqtt-port",     "MQTT_PORT",     String(BUILTIN_MQTT_PORT)), 10);
const MQTT_CERT_DIR         = opt("mqtt-cert-dir", "MQTT_CERT_DIR", BUILTIN_MQTT_CERT_DIR);
const OUT_DIR               = opt("out-dir",       "OUT_DIR",       path.join(__dirname, "sim_output"));
const PSIZE                 = parseInt(opt("psize", "PSIZE",   String(BUILTIN_PSIZE)), 10) || BUILTIN_PSIZE;
const DELAY_MS              = parseInt(opt("delay", "DELAY_MS", String(BUILTIN_DELAY_MS)), 10);
const DRY_RUN               = opt("dry-run", "DRY_RUN", "false") === "true" || cli["dry-run"] === "true";
const TEST_MQTT             = cli["test-mqtt"] === "true" || cli["test-mqtt"] === "";

// ── Validation ─────────────────────────────────────────────────────────────────
if (!TOKEN && !TEST_MQTT) {
    console.error("[ERROR] API_TOKEN is required — set --token or API_TOKEN env var");
    process.exit(1);
}
if (!USER_TOKEN && !DRY_RUN && !TEST_MQTT) {
    console.error("[ERROR] --user-token (USER_TOKEN) is required for HTTP upload (or use --dry-run)");
    process.exit(1);
}

// ── File paths ─────────────────────────────────────────────────────────────────
const runKey    = `${IMEI}_${FROM_MS}_${UNTIL_MS}`;
const dataFile  = path.join(OUT_DIR, `data_${runKey}.jsonl`);
const stateFile = path.join(OUT_DIR, `sim_state_${TARGET_IMEI}_${FROM_MS}_${UNTIL_MS}.json`);

// ── Logging helpers ────────────────────────────────────────────────────────────
const T0 = Date.now();

function ts() {
    const elapsed = ((Date.now() - T0) / 1000).toFixed(1);
    return `[+${elapsed.padStart(7)}s]`;
}

function log(tag, msg) {
    console.log(`${ts()} ${tag.padEnd(16)} ${msg}`);
}

function logProgress(tag, current, total, extra = "") {
    const pct  = total > 0 ? Math.round((current / total) * 100) : 0;
    const bar  = "[" + "█".repeat(Math.floor(pct / 5)).padEnd(20, "░") + "]";
    process.stdout.write(`\r${ts()} ${tag.padEnd(16)} ${bar} ${pct}% (${current}/${total}) ${extra}   `);
}

// ── State helpers ──────────────────────────────────────────────────────────────
function loadState() {
    try {
        return JSON.parse(fs.readFileSync(stateFile, "utf8"));
    } catch {
        return {
            fetchComplete:           false,
            totalPackets:            0,
            historicBatchesUploaded: 0,
            obdAccumIndex:           0,
            phase1Complete:          false,
            phase2Complete:          false,
            normalModeIndex:         0,
        };
    }
}

function saveState(state) {
    fs.writeFileSync(stateFile, JSON.stringify(state, null, 2));
}

// ── Packet helpers ─────────────────────────────────────────────────────────────
function identifyPacketType(packet) {
    if (!packet || typeof packet !== "object") return "unknown";
    if (packet.cv !== undefined || packet.tv !== undefined) return "handshake";
    if (packet.GA !== undefined || packet.GD !== undefined || packet.GT !== undefined) return "gps";
    if (packet.P  !== undefined || packet.DT_UDS3 !== undefined || packet.DT_UDS !== undefined) return "obd";
    if (packet.set !== undefined) return "settings";
    return "unknown";
}

function convertToL1Packet(packet) {
    const p = { ...packet, l: "1" };
    delete p.file;
    return p;
}

// ── Fetch helpers ──────────────────────────────────────────────────────────────
const sleep = ms => new Promise(r => setTimeout(r, ms));

async function fetchPage(lekParam) {
    let url = `${API_HOST}/idevice/logsV2/${IMEI}?psize=${PSIZE}&token=${TOKEN}&from=${FROM_MS}&until=${UNTIL_MS}`;
    if (lekParam && lekParam.t) url += `&last_t=${lekParam.t}`;

    let lastErr;
    for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
        try {
            const res  = await fetch(url);
            const json = await res.json();
            if (!json || (json.status && json.status.code && json.status.code !== 200)) {
                throw new Error("API error: " + JSON.stringify(json).slice(0, 200));
            }
            return json;
        } catch (e) {
            lastErr = e;
            if (attempt < MAX_RETRIES) {
                process.stdout.write(` [retry ${attempt}]`);
                await sleep(RETRY_DELAY_MS);
            }
        }
    }
    throw lastErr;
}

async function fetchAll(state) {
    if (state.fetchComplete) {
        log("[FETCH]", `Skipping — data file already exists (${state.totalPackets} packets)`);
        return;
    }

    log("[FETCH]", `Fetching OBD+GPS from API → ${IMEI}`);
    log("[FETCH]", `Window: ${new Date(Number(FROM_MS)).toISOString()} → ${new Date(Number(UNTIL_MS)).toISOString()}`);

    const allRows = [];
    let page = 0, errors = 0;
    let lekParam = null, prevLekStr = null;

    while (true) {
        page++;
        process.stdout.write(`${ts()} [FETCH]           Page ${page}  `);

        let json;
        try {
            json = await fetchPage(lekParam);
        } catch (e) {
            console.log(` FAIL: ${e.message}`);
            break;
        }

        const logs    = Array.isArray(json.logs) ? json.logs : [];
        const prevLen = allRows.length;
        console.log(` ${logs.length} entries  (total so far: ${prevLen})`);

        if (logs.length === 0) { log("[FETCH]", "Stop: empty page"); break; }
        if (logs.length === 1 && !json.last_evaluated_key) { log("[FETCH]", "Stop: terminal entry"); break; }

        for (const entry of logs) {
            let telemetryArr;
            try {
                const parsed = typeof entry.m === "string" ? JSON.parse(entry.m) : entry.m;
                telemetryArr = Array.isArray(parsed) ? parsed : [parsed];
            } catch {
                errors++;
                continue;
            }
            for (const packet of telemetryArr) {
                const type = identifyPacketType(packet);
                if (type !== "obd" && type !== "gps") continue;
                allRows.push({ t: entry.t || 0, packet });
            }
        }

        const lek    = json.last_evaluated_key;
        const lekStr = lek ? JSON.stringify(lek) : null;
        if (lekStr && lekStr === prevLekStr) { log("[FETCH]", "Stop: same LEK twice (no progress)"); break; }
        prevLekStr = lekStr;

        if (lek) {
            lekParam = lek;
        } else {
            log("[FETCH]", "Stop: no more pages");
            break;
        }

        if (DELAY_MS > 0) await sleep(DELAY_MS);
    }

    log("[FETCH]", `Sorting ${allRows.length} packets by timestamp...`);
    allRows.sort((a, b) => a.t - b.t);

    if (!fs.existsSync(OUT_DIR)) fs.mkdirSync(OUT_DIR, { recursive: true });
    const fd = fs.openSync(dataFile, "w");
    for (const row of allRows) {
        fs.writeSync(fd, JSON.stringify({ t: row.t, packet: row.packet }) + "\n");
    }
    fs.closeSync(fd);

    state.fetchComplete = true;
    state.totalPackets  = allRows.length;
    saveState(state);

    const fileSizeKB = Math.round(fs.statSync(dataFile).size / 1024);
    log("[FETCH]", `Done: ${allRows.length} OBD+GPS packets → ${path.basename(dataFile)} (${fileSizeKB} KB)${errors ? `  (${errors} parse errors skipped)` : ""}`);
}

// ── DB helpers ─────────────────────────────────────────────────────────────────
const SRC_DB_PATH = path.join(__dirname, "AppData.db");

function initSingleDb(dbPath, encrypted) {
    const db             = new Database(dbPath);
    const datastringType = encrypted ? "BLOB" : "CHAR(3000)";

    db.exec(`
        CREATE TABLE IF NOT EXISTS oData (
            DNO        INTEGER PRIMARY KEY AUTOINCREMENT,
            DATASTRING ${datastringType} NOT NULL
        );
        CREATE TABLE IF NOT EXISTS nSetting (
            NO INT, DRATE INT, LSPB REAL, MSPB REAL, HSPB REAL,
            LSPA REAL, MSPA REAL, HSPA REAL, HDOP REAL, ODRATE INT,
            SPEED INT, LEDON INT, URL CHAR(400), PORT INT
        );
        CREATE TABLE IF NOT EXISTS newAPN (
            NO INT, SSELECT INT, APN1 CHAR(50), APN2 CHAR(50)
        );
    `);

    if (fs.existsSync(SRC_DB_PATH)) {
        const src = new Database(SRC_DB_PATH, { readonly: true });
        try {
            const settings  = src.prepare("SELECT * FROM nSetting").all();
            const apns      = src.prepare("SELECT * FROM newAPN").all();
            const hasSetting = db.prepare("SELECT COUNT(*) as c FROM nSetting").get().c;
            if (!hasSetting && settings.length) {
                const ins = db.prepare(
                    "INSERT INTO nSetting VALUES (@NO,@DRATE,@LSPB,@MSPB,@HSPB,@LSPA,@MSPA,@HSPA,@HDOP,@ODRATE,@SPEED,@LEDON,@URL,@PORT)"
                );
                for (const r of settings) ins.run(r);
            }
            const hasApn = db.prepare("SELECT COUNT(*) as c FROM newAPN").get().c;
            if (!hasApn && apns.length) {
                const ins = db.prepare("INSERT INTO newAPN VALUES (@NO,@SSELECT,@APN1,@APN2)");
                for (const r of apns) ins.run(r);
            }
        } finally {
            src.close();
        }
    }

    return db;
}

function createHistoricBatch(batchNum, packets, publicKeyPem, dbPath) {
    log("[BATCH]", `Creating batch ${batchNum}: ${packets.length} records → ${path.basename(dbPath)}`);
    const t0   = Date.now();
    const db   = initSingleDb(dbPath, SEND_ENCRYPTED);
    const stmt = db.prepare("INSERT INTO oData(DATASTRING) VALUES (?)");

    const insertAll = db.transaction((rows) => {
        for (const v of rows) stmt.run(v);
    });

    const rows = packets.map((packet, i) => {
        if ((i + 1) % 100 === 0 || i + 1 === packets.length) {
            logProgress("[BATCH]", i + 1, packets.length, `batch ${batchNum}`);
        }
        const datastring = JSON.stringify(packet);
        if (SEND_ENCRYPTED) return encryptPacket(datastring, publicKeyPem, AES_VERSION);
        return datastring;
    });

    insertAll(rows);
    db.close();

    const sizeKB = Math.round(fs.statSync(dbPath).size / 1024);
    process.stdout.write("\n");
    log("[BATCH]", `Batch ${batchNum} ready: ${packets.length} rows, ${sizeKB} KB (${Date.now() - t0}ms)`);
}

// ── MQTT connection ────────────────────────────────────────────────────────────
function createMockMqttClient() {
    let publishCount = 0;
    return {
        publish(topic, payload, _opts, cb) {
            publishCount++;
            if (publishCount % 20 === 1) {
                log("[DRY/MQTT]", `SKIP publish #${publishCount} → ${topic}  (${Buffer.byteLength(payload)}B)`);
            }
            if (cb) cb(null);
        },
        end() { log("[DRY/MQTT]", `Mock client closed (total skipped publishes: ${publishCount})`); },
        on()   {},
        once() {},
    };
}

function loadMqttCerts() {
    const dirOverride = cli["mqtt-cert-dir"] || process.env.MQTT_CERT_DIR;
    if (dirOverride) {
        const d = dirOverride;
        return {
            ca:   fs.readFileSync(path.join(d, "ca-crt.pem")),
            cert: fs.readFileSync(path.join(d, "eu-client-cert-crt.pem")),
            key:  fs.readFileSync(path.join(d, "eu-client-cert-key.pem")),
            source: d,
        };
    }
    return {
        ca:   Buffer.from(EMBEDDED_MQTT_CA,   "utf8"),
        cert: Buffer.from(EMBEDDED_MQTT_CERT, "utf8"),
        key:  Buffer.from(EMBEDDED_MQTT_KEY,  "utf8"),
        source: "embedded",
    };
}

function loadPublicKey() {
    const pathOverride = cli["public-key"] || process.env.PUBLIC_KEY_PATH;
    if (pathOverride) {
        if (!fs.existsSync(pathOverride)) {
            console.error(`[ERROR] Public key not found: ${pathOverride}`);
            process.exit(1);
        }
        return { pem: fs.readFileSync(pathOverride, "utf8"), source: pathOverride };
    }
    return { pem: EMBEDDED_RSA_PUBLIC_KEY, source: "embedded" };
}

function connectMqtt() {
    const certs = loadMqttCerts();
    return new Promise((resolve, reject) => {
        const client  = mqtt.connect(`mqtts://${MQTT_HOST}`, {
            port:               MQTT_PORT,
            ca:                 certs.ca,
            cert:               certs.cert,
            key:                certs.key,
            clientId:           TARGET_IMEI,
            protocolVersion:    4,
            keepalive:          60,
            rejectUnauthorized: true,
            clean:              true,
            reconnectPeriod:    0,
        });

        let settled = false;

        client.on("error", (err) => {
            if (!settled) {
                settled = true;
                reject(new Error(`MQTT connect failed: ${err.message}`));
            } else {
                log("[MQTT]", `Error: ${err.message}`);
            }
        });

        client.once("connect", () => {
            if (settled) return;
            settled = true;
            log("[MQTT]", `Connected to ${MQTT_HOST}:${MQTT_PORT} (clientId: ${TARGET_IMEI})`);
            resolve(client);
        });
    });
}

// ── HTTP upload ────────────────────────────────────────────────────────────────
async function uploadFile(filePath) {
    const filename = path.basename(filePath);
    const sizeKB   = fs.existsSync(filePath) ? Math.round(fs.statSync(filePath).size / 1024) : 0;

    if (DRY_RUN) {
        log("[DRY/UPLOAD]", `SKIP upload: ${filename} (${sizeKB} KB)  seq-id=1  encrypted=${SEND_ENCRYPTED}`);
        await sleep(200);
        return;
    }

    const fileData = fs.readFileSync(filePath);
    log("[UPLOAD]", `Sending ${filename} (${sizeKB} KB) → seq-id=1, encrypted=${SEND_ENCRYPTED}`);

    for (let attempt = 1; attempt <= MAX_RETRIES; attempt++) {
        const t0 = Date.now();
        try {
            const form = new FormData();
            form.append("file", new Blob([fileData]), filename);

            const uploadHeaders = {
                "intangles-user-token": USER_TOKEN,
                "imei":                 TARGET_IMEI,
                "session-token":        SESSION_TOKEN,
                "seq-id":               "1",
            };
            if (SEND_ENCRYPTED) uploadHeaders["is-file-encrypted"] = "true";

            const res = await fetch(UPLOAD_URL, {
                method:  "POST",
                headers: uploadHeaders,
                body:    form,
            });

            const body = await res.text().catch(() => "");
            if (!res.ok) throw new Error(`HTTP ${res.status}: ${body.slice(0, 200)}`);
            log("[UPLOAD]", `OK ${res.status} — ${filename} in ${Date.now() - t0}ms`);
            return;
        } catch (e) {
            log("[UPLOAD]", `Attempt ${attempt}/${MAX_RETRIES} FAILED: ${e.message}`);
            if (attempt < MAX_RETRIES) await sleep(RETRY_DELAY_MS * attempt);
        }
    }
    log("[UPLOAD]", `All retries exhausted for ${filename} — continuing anyway`);
}

// ── Split packets ──────────────────────────────────────────────────────────────
function splitPackets() {
    const historyEndNum = Number(HISTORY_END_MS);
    const lines = fs.readFileSync(dataFile, "utf8").split("\n").filter(l => l.trim());
    const allRows = lines.map(l => {
        try { return JSON.parse(l); } catch { return null; }
    }).filter(Boolean);

    // Re-filter OBD+GPS in case JSONL was produced by a different script
    const filtered = allRows.filter(r => {
        const t = identifyPacketType(r.packet);
        return t === "obd" || t === "gps";
    });

    const historicRows = filtered.filter(r => r.t < historyEndNum);
    const liveRows     = filtered.filter(r => r.t >= historyEndNum);

    // Divide historic into fixed-size chunks of BATCH_SIZE records each
    const historicBatches = [];
    for (let i = 0; i < historicRows.length; i += BATCH_SIZE) {
        historicBatches.push(historicRows.slice(i, i + BATCH_SIZE).map(r => r.packet));
    }
    if (historicBatches.length === 0) historicBatches.push([]);

    const liveGpsPackets = liveRows.filter(r => identifyPacketType(r.packet) === "gps").map(r => r.packet);
    const liveObdPackets = liveRows.filter(r => identifyPacketType(r.packet) === "obd").map(r => r.packet);
    const livePackets    = liveRows.map(r => ({ packet: r.packet, type: identifyPacketType(r.packet) }));

    log("[SPLIT]", `Total OBD+GPS: ${filtered.length} packets`);
    log("[SPLIT]", `Historic (< ${new Date(historyEndNum).toISOString()}): ${historicRows.length} packets → ${historicBatches.length} files (${BATCH_SIZE} records each)`);
    for (let i = 0; i < historicBatches.length; i++) {
        log("[SPLIT]", `  Batch ${i + 1}: ${historicBatches[i].length} records`);
    }
    log("[SPLIT]", `Live (>= split): ${liveRows.length} packets  (GPS: ${liveGpsPackets.length}  OBD: ${liveObdPackets.length})`);

    return { historicBatches, liveGpsPackets, liveObdPackets, livePackets };
}

// ── Phase 1 ────────────────────────────────────────────────────────────────────
async function runPhase1(state, batchFiles, liveGpsPackets, liveObdPackets, mqttClient, obdDb, publicKeyPem) {
    const phaseStart = Date.now();
    log("[PHASE 1]", "=== Starting Phase 1: Historic upload + Live GPS L1 + OBD accumulation ===");

    let phase1Done = false;
    let resolvePhase1Signal;
    const phase1Signal = new Promise(resolve => { resolvePhase1Signal = resolve; });

    // ── Stream A: sequential historic file uploads ──────────────────────────
    const streamA = async () => {
        const startAt = state.historicBatchesUploaded || 0;
        log("[P1/UPLOAD]", `Uploading ${batchFiles.length} batch files (resuming from index ${startAt})`);

        for (let i = startAt; i < batchFiles.length; i++) {
            log("[P1/UPLOAD]", `── Batch ${i + 1} of ${batchFiles.length} ──`);
            await uploadFile(batchFiles[i]);
            state.historicBatchesUploaded = i + 1;
            saveState(state);
            if (i < batchFiles.length - 1) {
                log("[P1/UPLOAD]", `Waiting ${BATCH_UPLOAD_DELAY_MS}ms before next upload...`);
                await sleep(BATCH_UPLOAD_DELAY_MS);
            }
        }

        const elapsed = ((Date.now() - phaseStart) / 1000).toFixed(1);
        log("[P1/UPLOAD]", `All ${batchFiles.length} historic batches uploaded in ${elapsed}s — signalling streams to stop`);
        phase1Done = true;
        resolvePhase1Signal();
    };

    // ── Stream B: GPS L1 MQTT streaming ────────────────────────────────────
    const streamB = async () => {
        if (liveGpsPackets.length === 0) {
            log("[P1/GPS]", "No live GPS packets available — stream idle");
            await phase1Signal;
            return;
        }

        let gpsIdx = 0, published = 0, errors = 0;
        log("[P1/GPS]", `GPS L1 stream started: ${liveGpsPackets.length} unique packets cycling every ${GPS_L1_INTERVAL_MS}ms`);
        log("[P1/GPS]", `Topic: ${TARGET_IMEI}/obd  (l:"1" enriched GPS)`);

        const intervalId = setInterval(() => {
            const packet   = liveGpsPackets[gpsIdx % liveGpsPackets.length];
            const l1Packet = convertToL1Packet(packet);
            gpsIdx++;
            mqttClient.publish(
                `${TARGET_IMEI}/obd`,
                JSON.stringify(l1Packet),
                { qos: 0 },
                (err) => {
                    if (err) {
                        errors++;
                        log("[P1/GPS]", `Publish error #${errors}: ${err.message}`);
                    } else {
                        published++;
                        if (published % 10 === 0) {
                            log("[P1/GPS]", `Published ${published} GPS L1 packets (cycling idx ${gpsIdx % liveGpsPackets.length}/${liveGpsPackets.length})`);
                        }
                    }
                }
            );
        }, GPS_L1_INTERVAL_MS);

        await phase1Signal;
        clearInterval(intervalId);
        log("[P1/GPS]", `GPS L1 stream stopped — total published: ${published}${errors ? `  errors: ${errors}` : ""}`);
    };

    // ── Stream C: OBD accumulation into SQLite (resumes from state.obdAccumIndex) ──
    const streamC = async () => {
        if (liveObdPackets.length === 0) {
            log("[P1/OBD]", "No live OBD packets available — stream idle");
            await phase1Signal;
            return;
        }

        // Resume: skip packets already inserted in a previous run
        const resumeFrom = state.obdAccumIndex || 0;
        let obdIdx       = resumeFrom;
        let insertErrors = 0;

        if (DRY_RUN) {
            log("[DRY/OBD]", `OBD accumulation (DRY RUN): ${liveObdPackets.length} packets, resuming from idx ${resumeFrom}, every ${OBD_ACCUM_INTERVAL_MS}ms`);
            while (obdIdx < liveObdPackets.length) {
                await Promise.race([sleep(OBD_ACCUM_INTERVAL_MS), phase1Signal]);
                if (phase1Done) break;
                obdIdx++;
                state.obdAccumIndex = obdIdx;
                saveState(state);
                log("[DRY/OBD]", `SKIP insert OBD row ${obdIdx}/${liveObdPackets.length}`);
            }
            await phase1Signal;
            log("[DRY/OBD]", `OBD accumulation ended (dry run) — would have inserted ${obdIdx - resumeFrom} rows`);
            return;
        }

        const stmt = obdDb.prepare("INSERT INTO oData(DATASTRING) VALUES (?)");
        log("[P1/OBD]", `OBD accumulation started: ${liveObdPackets.length} packets (resuming from idx ${resumeFrom}), one every ${OBD_ACCUM_INTERVAL_MS}ms`);

        while (obdIdx < liveObdPackets.length) {
            await Promise.race([sleep(OBD_ACCUM_INTERVAL_MS), phase1Signal]);
            if (phase1Done) break;

            const packet     = liveObdPackets[obdIdx];
            const datastring = JSON.stringify(packet);
            try {
                const value = SEND_ENCRYPTED
                    ? encryptPacket(datastring, publicKeyPem, AES_VERSION)
                    : datastring;
                stmt.run(value);
                obdIdx++;
                state.obdAccumIndex = obdIdx;
                saveState(state);
                log("[P1/OBD]", `Accumulated OBD row ${obdIdx}/${liveObdPackets.length}`);
            } catch (e) {
                insertErrors++;
                log("[P1/OBD]", `Insert error #${insertErrors}: ${e.message} — skipping row ${obdIdx}`);
                obdIdx++;
            }
        }

        if (obdIdx >= liveObdPackets.length) {
            log("[P1/OBD]", "All available OBD packets exhausted — waiting for phase end");
        }
        await phase1Signal;
        log("[P1/OBD]", `OBD accumulation stopped — total: ${obdIdx}${insertErrors ? `  errors: ${insertErrors}` : ""}`);
    };

    await Promise.all([streamA(), streamB(), streamC()]);
    const elapsed = ((Date.now() - phaseStart) / 1000).toFixed(1);
    log("[PHASE 1]", `=== Phase 1 complete in ${elapsed}s ===`);
}

// ── Phase 2 ────────────────────────────────────────────────────────────────────
async function runPhase2(state, mqttClient, livePackets, obdDbPath) {
    const phaseStart = Date.now();
    log("[PHASE 2]", "=== Starting Phase 2: OBD upload + Normal-mode streaming ===");

    // 1. Upload accumulated OBD file
    const sizeKB = fs.existsSync(obdDbPath) ? Math.round(fs.statSync(obdDbPath).size / 1024) : 0;
    const obdRows = sizeKB > 0
        ? new Database(obdDbPath, { readonly: true }).prepare("SELECT COUNT(*) as c FROM oData").get().c
        : 0;
    log("[P2/UPLOAD]", `Uploading accumulated OBD file: ${path.basename(obdDbPath)} (${sizeKB} KB, ${obdRows} rows)`);
    await uploadFile(obdDbPath);

    // 2. Normal mode streaming
    const startIdx = state.normalModeIndex || 0;
    const remaining = livePackets.length - startIdx;

    if (remaining <= 0) {
        log("[P2/STREAM]", "All live packets already published — Phase 2 complete");
        return;
    }

    log("[P2/STREAM]", `Normal mode: ${remaining} packets to publish at ${NORMAL_INTERVAL_MS}ms interval`);
    log("[P2/STREAM]", `Topic: ${TARGET_IMEI}/obd  (all packets, no L1 enrichment)`);
    log("[P2/STREAM]", `Estimated duration: ~${Math.round((remaining * NORMAL_INTERVAL_MS) / 60000)} minutes`);

    let gpsCount = 0, obdCount = 0, pubErrors = 0;

    await new Promise(resolve => {
        let idx = startIdx;

        const intervalId = setInterval(() => {
            if (idx >= livePackets.length) {
                clearInterval(intervalId);
                const elapsed = ((Date.now() - phaseStart) / 1000).toFixed(1);
                process.stdout.write("\n");
                log("[P2/STREAM]", `Normal mode complete — GPS: ${gpsCount}, OBD: ${obdCount}${pubErrors ? `, errors: ${pubErrors}` : ""} (${elapsed}s)`);
                resolve();
                return;
            }

            const { packet, type } = livePackets[idx++];
            state.normalModeIndex = idx;
            saveState(state);

            const topic = `${TARGET_IMEI}/obd`;
            if (type === "gps" || type === "obd") {
                mqttClient.publish(
                    topic,
                    JSON.stringify(packet),
                    { qos: 0 },
                    (err) => {
                        if (err) {
                            pubErrors++;
                            log("[P2/STREAM]", `Publish error: ${err.message}`);
                        } else {
                            if (type === "gps") gpsCount++; else obdCount++;
                        }
                    }
                );
            }

            const total   = livePackets.length - startIdx;
            const current = idx - startIdx;
            if (current % 5 === 0 || current === 1) {
                logProgress("[P2/STREAM]", current, total, `gps:${gpsCount} obd:${obdCount}`);
            }
        }, NORMAL_INTERVAL_MS);
    });
}

// ── MQTT connection test ───────────────────────────────────────────────────────
async function runMqttTest() {
    const { source: certSource } = loadMqttCerts();
    const separator = "═".repeat(64);
    console.log(`\n${separator}`);
    console.log("  simulate-device.js — MQTT connection test");
    console.log(separator);
    console.log(`  Broker               ${MQTT_HOST}:${MQTT_PORT}`);
    console.log(`  Client ID            ${TARGET_IMEI}`);
    console.log(`  TLS certs            ${certSource}`);
    console.log(`${separator}\n`);

    log("[TEST/MQTT]", `Connecting to ${MQTT_HOST}:${MQTT_PORT}...`);
    let client;
    try {
        client = await connectMqtt();
    } catch (e) {
        log("[TEST/MQTT]", `FAILED — ${e.message}`);
        process.exit(1);
    }

    log("[TEST/MQTT]", `Connection OK — publishing test ping to ${TARGET_IMEI}/ping ...`);
    await new Promise((resolve, reject) => {
        client.publish(`${TARGET_IMEI}/ping`, JSON.stringify({ test: true, ts: Date.now() }), { qos: 0 }, (err) => {
            if (err) reject(err); else resolve();
        });
    });
    log("[TEST/MQTT]", `Publish OK`);

    client.end();
    log("[TEST/MQTT]", `Disconnected. MQTT connection test PASSED.`);
}

// ── Main ───────────────────────────────────────────────────────────────────────
(async () => {
    if (TEST_MQTT) {
        await runMqttTest();
        return;
    }

    if (!fs.existsSync(OUT_DIR)) fs.mkdirSync(OUT_DIR, { recursive: true });

    const { pem: publicKeyPem, source: keySource } = loadPublicKey();
    const { source: certSource }                   = loadMqttCerts();
    const state                                    = loadState();

    const separator = "═".repeat(64);
    console.log(`\n${separator}`);
    if (DRY_RUN) {
        console.log("  simulate-device.js — DRY RUN (no uploads, no real MQTT)");
    } else {
        console.log("  simulate-device.js — History-replay + Live-mode simulation");
    }
    console.log(separator);
    console.log(`  Source IMEI          ${IMEI}  (API data source)`);
    console.log(`  Target IMEI          ${TARGET_IMEI}  (MQTT clientId + upload header)`);
    console.log(`  API window           ${new Date(Number(FROM_MS)).toISOString()} → ${new Date(Number(UNTIL_MS)).toISOString()}`);
    console.log(`  History end          ${new Date(Number(HISTORY_END_MS)).toISOString()}  (splits historic / live)`);
    console.log(`  Batch size           ${BATCH_SIZE} records/file  (12h historic window)`);
    console.log(`  Batch upload delay   ${BATCH_UPLOAD_DELAY_MS}ms`);
    console.log(`  GPS L1 interval      ${GPS_L1_INTERVAL_MS}ms  (Phase 1)`);
    console.log(`  OBD accum interval   ${OBD_ACCUM_INTERVAL_MS}ms  (Phase 1)`);
    console.log(`  Normal interval      ${NORMAL_INTERVAL_MS}ms  (Phase 2)`);
    console.log(`  Encrypted            ${SEND_ENCRYPTED}  (AES-${AES_VERSION === 1 ? 128 : 256}-ECB / RSA-OAEP)`);
    console.log(`  RSA public key       ${keySource}`);
    console.log(`  MQTT TLS certs       ${certSource}`);
    console.log(`  Session token        ${SESSION_TOKEN}`);
    console.log(`  State file           ${stateFile}`);
    console.log(`  Data file            ${dataFile}`);
    console.log(`  Fetch complete       ${state.fetchComplete}`);
    console.log(`  Phase 1 done         ${state.phase1Complete}`);
    console.log(`  Phase 2 done         ${state.phase2Complete}`);
    console.log(separator + "\n");

    // Step 0: Fetch & sort
    await fetchAll(state);

    // Step 1: Split
    log("[SPLIT]", "Partitioning data into historic and live sets...");
    const { historicBatches, liveGpsPackets, liveObdPackets, livePackets } = splitPackets();
    console.log();

    // Step 2: Create SQLite batch files (idempotent)
    const batchCount = historicBatches.length;
    log("[BATCHES]", `Creating ${batchCount} historic SQLite files (${BATCH_SIZE} records each, encrypted=${SEND_ENCRYPTED})...`);
    const batchFiles = [];
    for (let i = 0; i < batchCount; i++) {
        const dbPath = path.join(OUT_DIR, `historic_batch_${i + 1}_${TARGET_IMEI}.db`);
        if (!fs.existsSync(dbPath)) {
            createHistoricBatch(i + 1, historicBatches[i], publicKeyPem, dbPath);
        } else {
            log("[BATCHES]", `Batch ${i + 1}: reusing ${path.basename(dbPath)} (${Math.round(fs.statSync(dbPath).size / 1024)} KB)`);
        }
        batchFiles.push(dbPath);
    }
    console.log();

    // Open (or create) live OBD accumulation DB
    const obdDbPath = path.join(OUT_DIR, `live_obd_${TARGET_IMEI}.db`);
    const obdDb     = initSingleDb(obdDbPath, SEND_ENCRYPTED);
    log("[OBD DB]", `${path.basename(obdDbPath)} opened for accumulation (encrypted=${SEND_ENCRYPTED})`);
    console.log();

    // Step 3: MQTT connection
    let mqttClient;
    if (DRY_RUN) {
        log("[DRY/MQTT]", `Skipping real MQTT connect — using mock client`);
        mqttClient = createMockMqttClient();
    } else {
        log("[MQTT]", `Connecting to ${MQTT_HOST}:${MQTT_PORT} with TLS client certs (${certSource})...`);
        mqttClient = await connectMqtt();
    }
    console.log();

    // Phase 1
    if (!state.phase1Complete) {
        try {
            await runPhase1(state, batchFiles, liveGpsPackets, liveObdPackets, mqttClient, obdDb, publicKeyPem);
        } catch (e) {
            log("[PHASE 1]", `Unexpected error: ${e.message}`);
        }
        obdDb.close();
        log("[PHASE 1]", "OBD accumulation DB closed and flushed.");
        state.phase1Complete = true;
        saveState(state);
    } else {
        log("[PHASE 1]", "Already complete — skipping.");
        obdDb.close();
    }
    console.log();

    // Phase 2
    if (!state.phase2Complete) {
        try {
            await runPhase2(state, mqttClient, livePackets, obdDbPath);
        } catch (e) {
            log("[PHASE 2]", `Unexpected error: ${e.message}`);
        }
        state.phase2Complete = true;
        saveState(state);
    } else {
        log("[PHASE 2]", "Already complete — skipping.");
    }

    mqttClient.end();

    const totalElapsed = ((Date.now() - T0) / 1000).toFixed(1);
    console.log();
    console.log(separator);
    log("[DONE]", `Simulation complete in ${totalElapsed}s`);
    log("[DONE]", `State: ${stateFile}`);
    console.log(separator + "\n");
})().catch(e => { console.error(`[FATAL] ${e.message}`); console.error(e.stack); process.exit(1); });
