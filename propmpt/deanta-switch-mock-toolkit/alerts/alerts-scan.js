#!/usr/bin/env node
// Read-only: for every vehicle of an account, which alert types fired in the last DAYS days, and when.
//   INTANGLES_TOKEN=... SOURCE_ACCOUNT=1482046680973967360 DAYS=30 node alerts/alerts-scan.js
// Writes OUT (default data/alerts-scan.json) — input for plan-replay-windows.js.
// Route: GET /alertlog/vehicle/<vid>/logsV2/<start_ms>/<end_ms>?psize=500&pnum=N  -> plain array of alerts
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, ACC = process.env.SOURCE_ACCOUNT || "1482046680973967360";
const DAYS = +(process.env.DAYS || 30);
const OUT = process.env.OUT || path.join(__dirname, "..", "data", "alerts-scan.json");
if (!TOKEN) { console.error("set INTANGLES_TOKEN"); process.exit(1); }
const H = { "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(async r => { const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _raw: t.slice(0, 200) }; } });
const iso = t => new Date(t).toISOString().replace(".000Z", "Z");
(async () => {
    const end = Date.now(), start = end - DAYS * 864e5;
    const veh = (await get(`/vehicle/getlist?&pnum=1&psize=500&acc_id=${ACC}&lang=en`)).v;
    const out = [];
    for (const v of veh) {
        const all = [];
        for (let pnum = 1; pnum <= 20; pnum++) {
            const j = await get(`/alertlog/vehicle/${v.id}/logsV2/${start}/${end}?psize=500&pnum=${pnum}`);
            const arr = Array.isArray(j) ? j : [];
            all.push(...arr);
            if (arr.length < 500) break;
        }
        const types = {};
        for (const a of all) (types[a.type || "?"] = types[a.type || "?"] || []).push(a.timestamp);
        out.push({ vid: v.id, plate: v.plate, imei: v.t_imei || v.tracker_imei, total: all.length, types });
        console.log(`${v.plate} ${v.t_imei} alerts=${all.length} types=${Object.keys(types).length} :: ${Object.entries(types).map(([k, a]) => k + "(" + a.length + ")").join(" ")}`);
    }
    fs.mkdirSync(path.dirname(OUT), { recursive: true });
    fs.writeFileSync(OUT, JSON.stringify({ account: ACC, start, end, vehicles: out }));
    console.log(`\nwindow ${iso(start)} .. ${iso(end)} -> ${OUT}`);
})().catch(e => { console.error(e); process.exit(1); });
