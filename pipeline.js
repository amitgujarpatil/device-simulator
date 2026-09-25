#!/usr/bin/env node
//
// pipeline.js — fetch telemetry from API, sort, store locally, then publish
//               OBD and GPS packets to RabbitMQ message.parser3.
//
// Usage:
//   node pipeline.js                              # run with all defaults
//   node pipeline.js --imei 869305070942563 --from 1789669800000 --until 1789756200000
//
// Options (CLI flag / env var / default):
//   --imei       IMEI         869305070942563
//   --from       FROM_MS      1789669800000
//   --until      UNTIL_MS     1789756200000
//   --token      API_TOKEN    <built-in>
//   --host       API_HOST     <built-in>
//   --rmq-url    RMQ_URL      <built-in>
//   --psize      PSIZE        1000
//   --delay      DELAY_MS     300
//   --out-dir    OUT_DIR      ./output
//
// Resume behaviour:
//   State is stored in output/state_<IMEI>_<FROM>_<UNTIL>.json.
//   Re-running with the same IMEI+from+until resumes publish from where it left off.
//   Delete the state file (or the data file) to start fresh.

"use strict";

const fs             = require("fs");
const path           = require("path");
const zlib           = require("zlib");
const { promisify }  = require("util");
const amqp           = require("amqplib");

// ── built-in defaults ─────────────────────────────────────────────────────────
const BUILTIN_API_HOST  = "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const BUILTIN_IMEI        = "869305070942563";
const BUILTIN_TARGET_IMEI = "869305077523101";  // IMEI written into published messages
const BUILTIN_FROM_MS   = "1789756200000";
const BUILTIN_UNTIL_MS  = "1789842600000";
const BUILTIN_TOKEN     = "RlOLZw2on9fP2edIOuGeQbZBGuxxBnUBEXkwLpiec1H9cCO6wsDo4mmOvMwgXUhD";
const BUILTIN_RMQ_URL   = "amqp://appuser:Ni1sYg0VfUmVibf@rabbitmqcluster-prod.default.svc.cluster.local:5672/prod_new";
const BUILTIN_PSIZE     = 1000;
const BUILTIN_DELAY_MS  = 300;
const BUILTIN_OUT_DIR   = path.join(__dirname, "output");
const QUEUE_NAME        = "message.parser3";
const MAX_RETRIES       = 3;
const RETRY_DELAY_MS    = 2000;

// ── CLI arg parser ────────────────────────────────────────────────────────────
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

// ── resolved options ──────────────────────────────────────────────────────────
const API_HOST = opt("host",    "API_HOST",  BUILTIN_API_HOST);
const IMEI        = opt("imei",        "IMEI",        BUILTIN_IMEI);
const TARGET_IMEI = opt("target-imei", "TARGET_IMEI", BUILTIN_TARGET_IMEI);
const TOKEN    = opt("token",   "API_TOKEN", BUILTIN_TOKEN);
const FROM_MS  = opt("from",    "FROM_MS",   BUILTIN_FROM_MS);
const UNTIL_MS = opt("until",   "UNTIL_MS",  BUILTIN_UNTIL_MS);
const RMQ_URL  = opt("rmq-url", "RMQ_URL",   BUILTIN_RMQ_URL);
const PSIZE    = parseInt(opt("psize",  "PSIZE",    String(BUILTIN_PSIZE)),   10) || BUILTIN_PSIZE;
const DELAY_MS = parseInt(opt("delay",  "DELAY_MS", String(BUILTIN_DELAY_MS)), 10);
const OUT_DIR  = opt("out-dir", "OUT_DIR",   BUILTIN_OUT_DIR);

if (!TOKEN) {
    console.error("API token is required — set --token or API_TOKEN env var");
    process.exit(1);
}

// ── file paths (keyed by run params for state isolation) ─────────────────────
const runKey    = `${IMEI}_${FROM_MS}_${UNTIL_MS}`;
const dataFile  = path.join(OUT_DIR, `data_${runKey}.jsonl`);
const stateFile = path.join(OUT_DIR, `state_${runKey}.json`);

// ── state helpers ─────────────────────────────────────────────────────────────
function loadState() {
    try {
        return JSON.parse(fs.readFileSync(stateFile, "utf8"));
    } catch {
        return { fetchComplete: false, totalPackets: 0, lastProcessedIndex: -1 };
    }
}
function saveState(state) {
    fs.writeFileSync(stateFile, JSON.stringify(state, null, 2));
}

// ── packet type helpers ───────────────────────────────────────────────────────
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

// ── compression (mirrors berries format) ─────────────────────────────────────
const gzipAsync = promisify(zlib.gzip);
async function compressZLIB(content) {
    const raw = typeof content === "string" ? content : JSON.stringify(content);
    const compressed = await gzipAsync(Buffer.from(raw, "utf8"));
    return { zlib: true, data: compressed.toString("base64") };
}

// ── fetch helpers ─────────────────────────────────────────────────────────────
const sleep = ms => new Promise(r => setTimeout(r, ms));

async function fetchPage(lekParam) {
    let url = `${API_HOST}/idevice/logsV2/${IMEI}?psize=${PSIZE}&token=${TOKEN}&from=${FROM_MS}&until=${UNTIL_MS}`;
    if (lekParam && lekParam.t) url += `&last_t=${lekParam.t}`;
    console.log(`  Fetching: ${url}`);

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
                process.stdout.write(` [retry ${attempt}]...`);
                await sleep(RETRY_DELAY_MS);
            }
        }
    }
    throw lastErr;
}

// ── phase 1: fetch all pages → sorted JSONL ──────────────────────────────────
async function fetchAll(state) {
    if (state.fetchComplete) {
        console.log("  Fetch already complete — skipping to publish phase.\n");
        return;
    }

    console.log("Phase 1: Fetching data from API...\n");

    const allRows = [];
    let page = 0, errors = 0;
    let lekParam = null, prevLekStr = null;

    while (true) {
        page++;
        process.stdout.write(`  page ${page}  `);

        let json;
        try {
            json = await fetchPage(lekParam);
        } catch (e) {
            console.log(`FAIL (${e.message})`);
            break;
        }

        const logs = Array.isArray(json.logs) ? json.logs : [];
        console.log(`→ ${logs.length} entries`);

        if (logs.length === 0)                          { console.log("  stop: empty page"); break; }
        if (logs.length === 1 && !json.last_evaluated_key) { console.log("  stop: last entry, not inserting"); break; }

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
                allRows.push({ t: entry.t || 0, packet });
            }
        }

        const lek    = json.last_evaluated_key;
        const lekStr = lek ? JSON.stringify(lek) : null;
        if (lekStr && lekStr === prevLekStr) { console.log("  stop: same LEK twice (no progress)"); break; }
        prevLekStr = lekStr;

        if (lek) {
            lekParam = lek;
        } else {
            const lastT = logs[logs.length - 1]?.t;
            if (!lastT) { console.log("  stop: no timestamp on last entry"); break; }
            // no LEK — API exhausted entries in this window
            break;
        }

        if (DELAY_MS > 0) await sleep(DELAY_MS);
    }

    // sort oldest → newest and write JSONL
    allRows.sort((a, b) => a.t - b.t);
    if (!fs.existsSync(OUT_DIR)) fs.mkdirSync(OUT_DIR, { recursive: true });
    const fd = fs.openSync(dataFile, "w");
    for (const row of allRows) {
        fs.writeSync(fd, JSON.stringify({ t: row.t, packet: row.packet }) + "\n");
    }
    fs.closeSync(fd);

    state.fetchComplete    = true;
    state.totalPackets     = allRows.length;
    state.lastProcessedIndex = -1;
    saveState(state);

    console.log(`\n  Stored ${allRows.length} packets (sorted) → ${dataFile}`);
    if (errors) console.log(`  Parse errors skipped: ${errors}`);
    console.log();
}

// ── phase 2: publish OBD + GPS packets to RabbitMQ ───────────────────────────
async function publishAll(state) {
    console.log("Phase 2: Publishing to RabbitMQ...\n");
    console.log(`  Queue:    ${QUEUE_NAME}`);
    console.log(`  Resume:   from index ${state.lastProcessedIndex + 1} of ${state.totalPackets}\n`);

    const lines = fs.readFileSync(dataFile, "utf8").split("\n").filter(l => l.trim());

    const conn = await amqp.connect(RMQ_URL);
    const ch   = await conn.createConfirmChannel();

    let published = 0, skipped = 0;

    try {
        for (let i = state.lastProcessedIndex + 1; i < lines.length; i++) {
            let row;
            try {
                row = JSON.parse(lines[i]);
            } catch {
                state.lastProcessedIndex = i;
                saveState(state);
                continue;
            }

            const type = identifyPacketType(row.packet);

            if (type !== "obd" && type !== "gps") {
                skipped++;
                state.lastProcessedIndex = i;
                saveState(state);
                continue;
            }

            const enriched = type === "gps" ? convertToL1Packet(row.packet) : row.packet;
            const message = {
                imei:             TARGET_IMEI,
                client:           TARGET_IMEI,
                msg:              JSON.stringify(enriched),
                topic:            type,
                normalized_topic: `${TARGET_IMEI}/${type}`,
                timestamp:        row.t,
                mqtt_server_id:   "mqtt-broker-1",
            };
            const compressed = await compressZLIB(message);
            const payload    = Buffer.from(JSON.stringify(compressed));

            await new Promise((resolve, reject) => {
                ch.sendToQueue(QUEUE_NAME, payload, { persistent: true, contentType: "application/json" }, (err) => {
                    if (err) reject(err); else resolve();
                });
            });

            published++;
            state.lastProcessedIndex = i;
            saveState(state);

            if (published % 50 === 0) {
                process.stdout.write(`\r  Published: ${published}  (index ${i}/${lines.length - 1})   `);
            }
        }
    } finally {
        await ch.close();
        await conn.close();
    }

    console.log(`\r  Published: ${published}, Skipped (non-OBD/GPS): ${skipped}            `);
}

// ── main ──────────────────────────────────────────────────────────────────────
(async () => {
    if (!fs.existsSync(OUT_DIR)) fs.mkdirSync(OUT_DIR, { recursive: true });
    const state = loadState();

    console.log(`\n${"=".repeat(60)}`);
    console.log("pipeline: fetch → sort → publish");
    console.log(`  Source IMEI: ${IMEI}`);
    console.log(`  Target IMEI: ${TARGET_IMEI}`);
    console.log(`  Window:      ${new Date(+FROM_MS).toISOString()} → ${new Date(+UNTIL_MS).toISOString()}`);
    console.log(`  RMQ:         ${RMQ_URL}`);
    console.log(`  Data file:   ${dataFile}`);
    console.log(`  State file:  ${stateFile}`);
    console.log(`  Fetch done:  ${state.fetchComplete}`);
    console.log(`  Published:   ${state.lastProcessedIndex + 1} / ${state.totalPackets || "?"} so far`);
    console.log(`${"=".repeat(60)}\n`);

    await fetchAll(state);
    await publishAll(state);

    console.log(`\n${"=".repeat(60)}`);
    console.log("Done.");
    console.log(`  State: ${stateFile}`);
    console.log(`${"=".repeat(60)}\n`);
})().catch(e => { console.error(e); process.exit(1); });
