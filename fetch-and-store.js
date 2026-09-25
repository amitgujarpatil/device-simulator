#!/usr/bin/env node
// Fetch telemetry packets from /idevice/logsV2/, encrypt each one using the
// hybrid envelope scheme, and write two SQLite output files:
//
//   AppData_unencrypted.db  — raw JSON DATASTRING per packet (same schema as AppData.db)
//   AppData_encrypted.db    — binary BLOB per packet (hybrid RSA+AES encrypted)
//
// Both output DBs also get nSetting + newAPN rows copied from AppData.db.
//
// Usage:
//   node generate-keypair.js               # one-time: create rsa_public.pem + rsa_private.pem
//   node fetch-and-store.js                # runs with all built-in defaults
//   node fetch-and-store.js --imei 869305070258853 --from 1788900000000
//   node fetch-and-store.js --aes-version 1 --public-key ./keys/my_key.pem
//
// Options can be passed as CLI flags (--flag value) or env vars. CLI > env > built-in default.
//
//   CLI flag         Env var      Default
//   ─────────────    ──────────   ──────────────────────────────────
//   --public-key     PUBLIC_KEY   keys/public-latest.pem
//   --aes-version    AES_VERSION  2  (1=aes-128-ecb, 2=aes-256-ecb)
//   --aes-key-128    AES_KEY_128  built-in (hex 16B override)
//   --aes-key-256    AES_KEY_256  built-in (hex 32B override)
//   --token          API_TOKEN    from raw-api.text
//   --imei           IMEI         from raw-api.text (fallback: 869305070951390)
//   --from           FROM_MS      1789237800000
//   --until          UNTIL_MS     1789410599000
//   --host           API_HOST     from raw-api.text (fallback: intangles-aws-eu-north-1)
//   --out-dir        OUT_DIR      output/
//   --psize          PSIZE        1000
//   --delay          DELAY_MS     300
//   --max-pages      MAX_PAGES    0  (0 = unlimited)
"use strict";
const fs   = require("fs");
const path = require("path");
const Database      = require("better-sqlite3");
const { encryptPacket, DEFAULT_AES_128_KEY, DEFAULT_AES_256_KEY } = require("./encryptionManager/encryptionEngine");

// ── hardcoded built-in defaults (all overridable via env or CLI flag) ─────────
const BUILTIN_API_HOST = "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const BUILTIN_IMEI     = "869305070942563";
const BUILTIN_FROM_MS  = "1789669800000";
const BUILTIN_UNTIL_MS = "1789756200000";
// const BUILTIN_FROM_MS  = "1789842600000";
// const BUILTIN_UNTIL_MS = "1789929000000";
const BUILTIN_KEY_PATH = path.join(__dirname, "keys", "public-latest.pem");

// ── CLI arg parser: --key value  or  --key=value ──────────────────────────────
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

// CLI > env > built-in defaults
function opt(cliKey, envKey, fallback) {
    return cli[cliKey] !== undefined ? cli[cliKey]
         : process.env[envKey]       ? process.env[envKey]
         : fallback;
}

// ── resolved options ──────────────────────────────────────────────────────────
const AES_VERSION = parseInt(opt("aes-version", "AES_VERSION", "2"), 10);
const OUT_DIR     = opt("out-dir", "OUT_DIR", null);
const PSIZE       = Math.min(1000, Math.max(1, parseInt(opt("psize", "PSIZE", "1000"), 1000))) || 1000;
const DELAY_MS    = parseInt(opt("delay", "DELAY_MS", "300"), 10);
const MAX_PAGES   = parseInt(opt("max-pages", "MAX_PAGES", "0"), 10) || 0;
const LIMIT       = parseInt(opt("limit",     "LIMIT",     "0"), 10) || 0;  // 0 = unlimited

if (![1, 2].includes(AES_VERSION)) {
    console.error("AES_VERSION must be 1 or 2");
    process.exit(1);
}

// ── master (RSA public) key — cli > env > default path ───────────────────────
const PUBLIC_KEY_PATH = opt("public-key", "PUBLIC_KEY", BUILTIN_KEY_PATH);
if (!fs.existsSync(PUBLIC_KEY_PATH)) {
    console.error(`Master key not found: ${PUBLIC_KEY_PATH}`);
    console.error("Run: node generate-keypair.js   (or set PUBLIC_KEY=./path/to/key.pem)");
    process.exit(1);
}
const publicKeyPem = fs.readFileSync(PUBLIC_KEY_PATH, "utf8");
if (!publicKeyPem.includes("PUBLIC KEY")) {
    console.error("PUBLIC_KEY file does not look like a PEM public key");
    process.exit(1);
}

// ── parse raw-api.text for additional defaults (token + optional overrides) ───
function parseRawApiText() {
    try {
        const txt = fs.readFileSync(path.join(__dirname, "raw-api.text"), "utf8").trim();
        const url = new URL(txt.replace(/&amp;/g, "&"));
        return {
            host:  url.origin,
            imei:  url.pathname.split("/").pop(),
            token: url.searchParams.get("token"),
            from:  url.searchParams.get("from"),
            until: url.searchParams.get("until"),
        };
    } catch { return {}; }
}
const fromFile = parseRawApiText();

const API_HOST = opt("host",    "API_HOST",  fromFile.host  || BUILTIN_API_HOST);
const IMEI     = opt("imei",    "IMEI",      fromFile.imei  || BUILTIN_IMEI);
const TOKEN    = opt("token",   "API_TOKEN", fromFile.token);
const FROM_MS  = opt("from",    "FROM_MS",   fromFile.from  || BUILTIN_FROM_MS);
const UNTIL_MS = opt("until",   "UNTIL_MS",  fromFile.until || BUILTIN_UNTIL_MS);

if (!TOKEN) {
    console.error("API token not found — set API_TOKEN=<token> or update raw-api.text");
    process.exit(1);
}

// ── open output DBs ───────────────────────────────────────────────────────────
const outDir    = OUT_DIR ? OUT_DIR : path.join(__dirname, "output");
const unencPath = path.join(outDir, "AppData_unencrypted5.db");
const encPath   = path.join(outDir, "AppData_encrypted5.db");
const srcPath   = path.join(__dirname, "AppData.db");

if (!fs.existsSync(outDir)) fs.mkdirSync(outDir, { recursive: true });
const unencDb = new Database(unencPath);
const encDb   = new Database(encPath);

function initDbs() {
    // Unencrypted: same schema as AppData.db oData
    unencDb.exec(`
        CREATE TABLE IF NOT EXISTS oData (
            DNO        INTEGER PRIMARY KEY AUTOINCREMENT,
            DATASTRING CHAR(3000) NOT NULL
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

    // Encrypted: same column name DATASTRING, stored as BLOB
    encDb.exec(`
        CREATE TABLE IF NOT EXISTS oData (
            DNO        INTEGER PRIMARY KEY AUTOINCREMENT,
            DATASTRING BLOB NOT NULL
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

    // Copy nSetting + newAPN from source AppData.db into both output DBs
    if (fs.existsSync(srcPath)) {
        const src = new Database(srcPath, { readonly: true });
        try {
            const settings = src.prepare("SELECT * FROM nSetting").all();
            const apns     = src.prepare("SELECT * FROM newAPN").all();

            for (const db of [unencDb, encDb]) {
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
            }
        } finally {
            src.close();
        }
        console.log("Copied nSetting + newAPN from AppData.db");
    }
}

// ── packet type filter ────────────────────────────────────────────────────────
function identifyPacketType(packet) {
    if (!packet || typeof packet !== "object") return "unknown";
    if (packet.cv !== undefined || packet.tv !== undefined) return "handshake";
    if (packet.GA !== undefined || packet.GD !== undefined || packet.GT !== undefined) return "gps";
    if (packet.P  !== undefined || packet.DT_UDS3 !== undefined || packet.DT_UDS !== undefined) return "obd";
    if (packet.set !== undefined) return "settings";
    return "unknown";
}

// ── prepared statements ───────────────────────────────────────────────────────
function prepareInserts() {
    return {
        unenc: unencDb.prepare("INSERT INTO oData(DATASTRING) VALUES (?)"),
        enc:   encDb.prepare("INSERT INTO oData(DATASTRING) VALUES (?)"),
    };
}

// ── fetch one page (with retries) ────────────────────────────────────────────
const MAX_RETRIES = 3;
const RETRY_DELAY_MS = 2000;


async function fetchPage(fromMs, lekParam) {
    let url = `${API_HOST}/idevice/logsV2/${IMEI}?psize=${PSIZE}&token=${TOKEN}&from=${fromMs}&until=${UNTIL_MS}`;
    if (lekParam && lekParam.t) url += `&last_t=${lekParam.t}`;
    console.log(`Fetching: ${url}`);

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
                process.stdout.write(` [retry ${attempt}/${MAX_RETRIES - 1}]... `);
                await sleep(RETRY_DELAY_MS);
            }
        }
    }
    throw lastErr;
}

// ── main ──────────────────────────────────────────────────────────────────────
const sleep = ms => new Promise(r => setTimeout(r, ms));

(async () => {
    const activeKey = AES_VERSION === 1 ? DEFAULT_AES_128_KEY : DEFAULT_AES_256_KEY;
    console.log(`\n${"=".repeat(60)}`);
    console.log(`fetch-and-store`);
    console.log(`  IMEI:        ${IMEI}`);
    console.log(`  Time window: ${new Date(+FROM_MS).toISOString()} → ${new Date(+UNTIL_MS).toISOString()}`);
    console.log(`  AES version: ${AES_VERSION}  (${AES_VERSION === 1 ? "aes-128-ecb" : "aes-256-ecb"})`);
    console.log(`  AES key:     ${activeKey.toString("hex")}  (${process.env[AES_VERSION === 1 ? "AES_KEY_128" : "AES_KEY_256"] ? "env override" : "built-in default"})`);
    console.log(`  Master key:  ${PUBLIC_KEY_PATH}  (${process.env.PUBLIC_KEY ? "env override" : "default path"})`);
    console.log(`  Page size:   ${PSIZE}`);
    console.log(`  Limit:       ${LIMIT > 0 ? LIMIT : "unlimited"}`);
    console.log(`  Filter:      obd + gps only`);
    console.log(`  Out dir:     ${outDir}`);
    console.log(`${"=".repeat(60)}\n`);

    initDbs();
    const stmts = prepareInserts();

    // Wrap both inserts in a single transaction per page for speed
    const insertPage = unencDb.transaction((rows) => {
        for (const { datastring, payload } of rows) {
            stmts.unenc.run(datastring);
            // encrypted inserts run outside this transaction — different DB
        }
    });
    const insertPageEnc = encDb.transaction((rows) => {
        for (const { payload } of rows) {
            stmts.enc.run(payload);
        }
    });

    let page = 0, totalPackets = 0, totalErrors = 0;
    let fromMs = FROM_MS;
    let lekParam = null;
    let prevLekStr = null;
    const allRows = [];   // accumulate across all pages; sorted + inserted after loop

    // eslint-disable-next-line no-constant-condition
    while (true) {
        page++;
        process.stdout.write(`  page ${page}  from=${new Date(+fromMs).toISOString()}... `);

        let json;
        try {
            json = await fetchPage(fromMs, lekParam);
        } catch (e) {
            console.log(`FAIL (${e.message})`);
            break;
        }

        const logs = Array.isArray(json.logs) ? json.logs : [];
        console.log(`${logs.length} entries`);

        if (logs.length === 0) { console.log("  stop: empty page"); break; }
        if (logs.length === 1 && !json.last_evaluated_key) { console.log("  stop: last entry, not inserting"); break; }

        for (const entry of logs) {
            if (LIMIT > 0 && totalPackets >= LIMIT) break;

            // Parse the m field (double-encoded JSON string of an array)
            let telemetryArr;
            try {
                const parsed = typeof entry.m === "string" ? JSON.parse(entry.m) : entry.m;
                telemetryArr = Array.isArray(parsed) ? parsed : [parsed];
            } catch(e) {
                totalErrors++;
                continue;
            }

            for (const obj of telemetryArr) {
                if (LIMIT > 0 && totalPackets >= LIMIT) break;

                const type = identifyPacketType(obj);
                if (type !== "obd" && type !== "gps") continue;

                const datastring = JSON.stringify(obj);
                let payload;
                try {
                    payload = encryptPacket(datastring, publicKeyPem, AES_VERSION);
                } catch (e) {
                    console.error(`    encrypt error: ${e.message}`);
                    totalErrors++;
                    continue;
                }
                allRows.push({ t: entry.t || 0, datastring, payload });
                totalPackets++;
            }
        }

        if (LIMIT > 0 && totalPackets >= LIMIT) { console.log(`  stop: reached LIMIT ${LIMIT}`); break; }
        if (MAX_PAGES > 0 && page >= MAX_PAGES) { console.log(`  stop: reached MAX_PAGES ${MAX_PAGES}`); break; }

        // Advance cursor for the next page.
        // LEK is a DynamoDB continuation token for the same query — fromMs must
        // stay fixed at FROM_MS so the query bounds don't shift. Only advance
        // fromMs when there is no LEK (need to move the time window forward).
        const lek = json.last_evaluated_key;
        const lekStr = lek ? JSON.stringify(lek) : null;
        if (lekStr && lekStr === prevLekStr) { console.log("  stop: same LEK returned twice (no progress)"); break; }
        prevLekStr = lekStr;
        if (lek) {
            lekParam = lek;
            // fromMs intentionally unchanged — keep original FROM_MS as lower bound
        } else {
            lekParam = null;
            const lastT = logs[logs.length - 1]?.t;
            if (!lastT) { console.log("  stop: no timestamp on last entry"); break; }
            fromMs = String(lastT + 1);
        }

        if (DELAY_MS > 0) await sleep(DELAY_MS);
    }

    // Sort all collected rows by API timestamp ascending (oldest → newest)
    allRows.sort((a, b) => a.t - b.t);
    console.log(`\n  Sorting ${allRows.length} packets by timestamp and inserting...`);
    insertPage(allRows);
    insertPageEnc(allRows);

    unencDb.close();
    encDb.close();

    console.log(`\n${"=".repeat(60)}`);
    console.log(`Done`);
    console.log(`  Pages fetched:    ${page}`);
    console.log(`  Packets stored:   ${totalPackets}`);
    console.log(`  Errors skipped:   ${totalErrors}`);
    console.log(`  Unencrypted DB:   ${unencPath}`);
    console.log(`  Encrypted DB:     ${encPath}`);
    console.log(`${"=".repeat(60)}\n`);
})().catch(e => { console.error(e); process.exit(1); });
