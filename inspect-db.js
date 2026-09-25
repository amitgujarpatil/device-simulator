#!/usr/bin/env node
// Reads oData rows from a SQLite DB and prints the packet type for each one.
// Usage:
//   node inspect-db.js                              # uses default DB path
//   node inspect-db.js --db ./output/AppData_unencrypted7.db
"use strict";

const path     = require("path");
const Database = require("better-sqlite3");

const DEFAULT_DB = path.join(__dirname, "output", "AppData_unencrypted7.db");

function parseCliArgs(argv) {
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

const cli    = parseCliArgs(process.argv);
const dbPath = cli.db || DEFAULT_DB;

function identifyPacketType(packet) {
    if (!packet || typeof packet !== "object") return "unknown";
    if (packet.cv !== undefined || packet.tv !== undefined) return "handshake";
    if (packet.GA !== undefined || packet.GD !== undefined || packet.GT !== undefined) return "gps";
    if (packet.P  !== undefined || packet.DT_UDS3 !== undefined || packet.DT_UDS !== undefined) return "obd";
    if (packet.set !== undefined) return "settings";
    return "unknown";
}

const db   = new Database(dbPath, { readonly: true });
const rows = db.prepare("SELECT DNO, DATASTRING FROM oData ORDER BY DNO").all();
db.close();

const counts = {};
for (const row of rows) {
    let packet;
    try { packet = JSON.parse(row.DATASTRING); } catch { packet = null; }
    const type = identifyPacketType(packet);
    counts[type] = (counts[type] || 0) + 1;
    console.log(`DNO ${String(row.DNO).padStart(5)}  type=${type.padEnd(10)}  ${row.DATASTRING.slice(0, 80)}`);
}

console.log(`\n${"─".repeat(50)}`);
console.log(`Total: ${rows.length} rows`);
for (const [type, count] of Object.entries(counts).sort()) {
    console.log(`  ${type.padEnd(12)} ${count}`);
}
