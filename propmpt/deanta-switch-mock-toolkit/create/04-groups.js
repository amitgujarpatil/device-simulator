#!/usr/bin/env node
// Recreate the source account's vehicle groups INSIDE the mock account and add the matching mock vehicles.
// (Groups are named vehicle sets within one account — not separate accounts.)
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> node create/04-groups.js
// Reads data/source-dump/groups.json + data/mock-seeded.json; writes data/mock-groups.json.
// Skips "all_vehicles" groups (e.g. Live-Tracking) unless INCLUDE_ALL_VEHICLE_GROUPS=1. Reuses a group if the name exists.
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, TARGET = process.env.TARGET_ACCOUNT;
const D = process.env.DATA_DIR || path.join(__dirname, "..", "data", "source-dump");
const SEEDED = process.env.SEEDED || path.join(__dirname, "..", "data", "mock-seeded.json");
const OUT = process.env.OUT || path.join(__dirname, "..", "data", "mock-groups.json");
if (!TOKEN || !TARGET) { console.error("set INTANGLES_TOKEN and TARGET_ACCOUNT"); process.exit(1); }
const H = { "Content-Type": "application/json", "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(r => r.json());
const post = (p, b) => fetch(API + p, { method: "POST", headers: H, body: JSON.stringify(b) }).then(async r => { const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _http: r.status, _raw: t.slice(0, 300) }; } });
(async () => {
    const srcGroups = JSON.parse(fs.readFileSync(path.join(D, "groups.json"), "utf8")).groups
        .filter(g => process.env.INCLUDE_ALL_VEHICLE_GROUPS === "1" || !g.all_vehicles);
    const rows = JSON.parse(fs.readFileSync(SEEDED, "utf8")).filter(r => r.test_vid);
    const existing = ((await get(`/group/listV2?acc_id=${TARGET}&psize=200&pnum=1`)).groups) || [];
    const out = [];
    for (const sg of srcGroups) {
        const vids = rows.filter(r => (r.source_groups || []).includes(sg.id)).map(r => r.test_vid);
        let gid = (existing.find(g => g.name === sg.name) || {}).id;
        if (!gid) {
            const cj = await post(`/group/createV2?acc_id=${TARGET}`, { name: sg.name, description: sg.description || "", account_id: TARGET });
            const r = cj.result || cj.group || cj;
            gid = r && (r.id || r._id || (Array.isArray(r) && r[0] && (r[0].id || r[0]._id)));
            if (!gid) { console.log(`FAIL create ${sg.name}: ${JSON.stringify(cj).slice(0, 300)}`); continue; }
        }
        const aj = await post(`/group/${gid}/addvehiclesV2?acc_id=${TARGET}`, { vehicle_ids: vids });
        console.log(`${sg.name}: group ${gid}, add ${vids.length} vehicles -> ${JSON.stringify(aj.status || aj.error || aj).slice(0, 200)}`);
        out.push({ source_group_id: sg.id, name: sg.name, test_group_id: gid, vehicle_ids: vids });
    }
    fs.writeFileSync(OUT, JSON.stringify(out, null, 2));
    for (const g of ((await get(`/group/listV2?acc_id=${TARGET}&psize=200&pnum=1`)).groups) || []) console.log(`read-back: ${g.id} | ${g.name} | vehicles_count ${g.vehicles_count}`);
})().catch(e => { console.error(e); process.exit(1); });
