#!/usr/bin/env node
// Read-only: per source device, allinfoV2 + per-vehicle alert config + per-vehicle config + vehicle record.
//   INTANGLES_TOKEN=... node discovery/collect-per-vehicle.js
// Reads DATA_DIR/idevices.json (from collect-account.sh), writes DATA_DIR/per-vehicle.json.
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN;
const D = process.env.DATA_DIR || path.join(__dirname, "..", "data", "source-dump");
if (!TOKEN) { console.error("set INTANGLES_TOKEN"); process.exit(1); }
const H = { "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = async p => { try { const r = await fetch(API + p, { headers: H }); const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _http: r.status, _raw: t.slice(0, 300) }; } } catch (e) { return { _err: e.message }; } };
const sleep = ms => new Promise(r => setTimeout(r, ms));
(async () => {
    const devs = JSON.parse(fs.readFileSync(path.join(D, "idevices.json"), "utf8")).idevices;
    const out = [];
    for (const d of devs) {
        const all = await get(`/idevice/${d.imei}/allinfoV2`);
        const r = all.result || all; const vid = r.vehicle && r.vehicle.id;
        const row = { imei: d.imei, allinfo: r };
        if (vid) {
            row.alertconfig = await get(`/alertconfigV2/vehicle/${vid}/all`);
            row.config = await get(`/config/all/vehicleV2/${vid}`);
            row.vehicle_full = await get(`/vehicle/${vid}`);
        }
        out.push(row);
        process.stdout.write(".");
        await sleep(150);
    }
    fs.writeFileSync(path.join(D, "per-vehicle.json"), JSON.stringify(out, null, 1));
    console.log(`\nwrote ${out.length} rows -> ${path.join(D, "per-vehicle.json")}`);
})();
