#!/usr/bin/env node
// Read-only. Builds the source -> mock mapping: a new Luhn-valid IMEI on the source TAC (each
// candidate checked unused via allinfoV2) and a new plate ("MK" + source plate minus its first two
// characters; plates are globally unique so they cannot be reused). Records what is reused as-is.
//   INTANGLES_TOKEN=... node discovery/build-mapping.js
// Reads DATA_DIR/{per-vehicle,groups,account-specs}.json; writes OUT_DIR/mock-mapping.{json,csv}.
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN;
const D = process.env.DATA_DIR || path.join(__dirname, "..", "data", "source-dump");
const OUT = process.env.OUT_DIR || path.join(__dirname, "..", "data");
const PLATE_PREFIX = process.env.PLATE_PREFIX || "MK";
if (!TOKEN) { console.error("set INTANGLES_TOKEN"); process.exit(1); }
const H = { "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(r => r.json()).catch(e => ({ _err: e.message }));
function checkDigit(d14) { let s = 0, db = true; for (let i = d14.length - 1; i >= 0; i--) { let n = +d14[i]; if (db) { n *= 2; if (n > 9) n -= 9; } s += n; db = !db; } return (10 - s % 10) % 10; }
const used = new Set();
function genImei(tac) { for (let t = 0; t < 100; t++) { const d14 = tac + ("000000" + Math.floor(Math.random() * 1e6)).slice(-6); const im = d14 + checkDigit(d14); if (!used.has(im)) { used.add(im); return im; } } throw new Error("imei gen"); }
const rd = f => JSON.parse(fs.readFileSync(path.join(D, f), "utf8"));
const rows = rd("per-vehicle.json");
const groups = rd("groups.json").groups;
const specs = rd("account-specs.json").specs || [];
const gname = id => (groups.find(g => g.id === id) || {}).name || id;
const sname = id => { const s = specs.find(x => x.id === id); return s ? `${s.manufacturer} ${s.model}` : ""; };
rows.forEach(r => used.add(String(r.imei)));
const srcPlates = new Set(rows.map(r => (r.allinfo.vehicle || {}).plate).filter(Boolean));
const mockPlates = new Set();
(async () => {
    const map = [];
    for (const r of rows) {
        const t = r.allinfo.tracker || {}, v = r.allinfo.vehicle || {};
        let mock;
        for (let i = 0; i < 10; i++) {
            mock = genImei(String(r.imei).slice(0, 8));
            const j = await get(`/idevice/${mock}/allinfoV2`);
            if (!(j && j.result && j.result.tracker && j.result.tracker.id)) break;
            mock = null;
        }
        let plate = v.plate ? PLATE_PREFIX + String(v.plate).slice(2) : null;
        for (let n = 0; plate && (srcPlates.has(plate) || mockPlates.has(plate)); n++) plate = PLATE_PREFIX[0] + "ABCDEFGHJ"[n] + String(v.plate).slice(2);
        if (plate) mockPlates.add(plate);
        const vac = (r.alertconfig && r.alertconfig.result && r.alertconfig.result.alert_config) || {};
        map.push({
            source_account_id: t.account_id || v.account_id,
            original_imei: String(r.imei), original_plate: v.plate || null, original_vid: v.id || null, original_dev_id: t.id,
            test_imei: mock, test_plate: plate,
            reuse: {
                spec_id: v.spec_id || null, spec_name: sname(v.spec_id), protocol: v.protocol || null,
                dev_type: t.type, base_type: t.base_type, communication_protocol: t.communication_protocol,
                obd_attached: v.obd_attached, fuel_level_enabled: v.fuel_level_enabled,
                emmission_standard: v.emmission_standard || null, vehicle_type: v.vehicle_type || null,
                tags: v.tags || [], groups: (v.groups || []).map(g => gname(g.id || g)),
                vehicle_alert_config_types: Object.keys(vac)
            },
            not_reused: { vin: v.vin || null }
        });
        process.stdout.write(".");
    }
    fs.mkdirSync(OUT, { recursive: true });
    fs.writeFileSync(path.join(OUT, "mock-mapping.json"), JSON.stringify(map, null, 2));
    const cols = ["original_imei", "original_plate", "original_vid", "test_imei", "test_plate"];
    fs.writeFileSync(path.join(OUT, "mock-mapping.csv"), [...cols, "spec_id", "protocol", "dev_type", "groups"].join(",") + "\n" +
        map.map(m => [...cols.map(c => m[c]), m.reuse.spec_id, m.reuse.protocol, m.reuse.dev_type, JSON.stringify(m.reuse.groups.join("|"))].join(",")).join("\n") + "\n");
    console.log(`\n${map.length} rows -> ${path.join(OUT, "mock-mapping.json")} (review before creating anything)`);
})();
