#!/usr/bin/env node
// Create one mock device + vehicle per row of the agreed mapping, on the mock account, and verify each by read-back.
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> DRY_RUN=1 LIMIT=2 node create/03-seed-vehicles.js   # print payloads
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> LIMIT=1 node create/03-seed-vehicles.js             # create ONE, check it
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> node create/03-seed-vehicles.js                     # the rest (resumable)
// MAPPING (default data/mock-mapping.json from build-mapping.js); OUT (default data/mock-seeded.json) — rows already
// in OUT are skipped, so a rerun never creates duplicates. There is NO device/vehicle delete route — only detach.
//
// Per row, 5 calls (each has a trap):
//  1. POST /idevice/createV2?acc_id=T {imei,type,base_type,communication_protocol,tag}   (v1 /idevice/create throws)
//  2. POST /vehicle/createV2?acc_id=T {plate,account_id,tracker_imei,protocol}         (needs token; plate globally unique)
//  3. POST /vehicle/<vid>/attachspecV2/<source spec_id>                                (reuse spec, never create)
//  4. POST /vehicle/<vid>/attachideviceV2/options/<dev_id> {operation:"ATTACH_AS_PRIMARY", functionality:["obd"|"tracker"],
//     skip_subs_validations:true}   (functionality must match device type; can return CROSSSLOT and still commit)
//  5. POST /vehicle/<vid>/update/baseinfoV2 {addTags:<source tags>, removeTags:[], tag:<plate>, emmission_standard, vehicle_type}
//     (v1 update/baseinfo throws "_removeTags is not defined"; the "vehicle_verified" tag is silently not settable)
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, TARGET = process.env.TARGET_ACCOUNT;
const MAPPING = process.env.MAPPING || path.join(__dirname, "..", "data", "mock-mapping.json");
const OUT = process.env.OUT || path.join(__dirname, "..", "data", "mock-seeded.json");
const LIMIT = process.env.LIMIT ? +process.env.LIMIT : Infinity, DRY = process.env.DRY_RUN === "1";
const TAG_PREFIX = process.env.TAG_PREFIX || "switchtest-";
if (!TOKEN || !TARGET) { console.error("set INTANGLES_TOKEN and TARGET_ACCOUNT"); process.exit(1); }
const H = { "Content-Type": "application/json", "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(r => r.json());
const post = (p, b) => fetch(API + p, { method: "POST", headers: H, body: JSON.stringify(b) }).then(async r => { const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _http: r.status, _raw: t.slice(0, 200) }; } });
const sleep = ms => new Promise(r => setTimeout(r, ms));
const info = async imei => { const j = await get(`/idevice/${imei}/allinfoV2`); const r = j.result || j; return { t: r.tracker || {}, v: r.vehicle || {} }; };
const st = j => JSON.stringify((j && (j.status || j)) || j).slice(0, 160);

(async () => {
    const pairs = JSON.parse(fs.readFileSync(MAPPING, "utf8")).filter(r => r.test_imei && r.test_plate);
    const done = fs.existsSync(OUT) ? JSON.parse(fs.readFileSync(OUT, "utf8")) : [];
    const doneSet = new Set(done.map(r => r.original_imei));
    let n = 0;
    for (const p of pairs) {
        if (doneSet.has(p.original_imei)) continue;
        if (n >= LIMIT) break; n++;
        const o = await info(p.original_imei);
        if (o.v.plate !== p.original_plate) { console.log(`SKIP ${p.original_imei}: source plate now ${o.v.plate}, expected ${p.original_plate}`); continue; }
        const devBody = { imei: p.test_imei, type: o.t.type || "obd", tag: TAG_PREFIX + p.test_imei };
        if (o.t.base_type) devBody.base_type = o.t.base_type;
        if (o.t.communication_protocol) devBody.communication_protocol = o.t.communication_protocol;
        const vehBody = { plate: p.test_plate, account_id: TARGET, tracker_imei: p.test_imei };
        if (o.v.protocol) vehBody.protocol = o.v.protocol;
        const baseBody = { addTags: Array.isArray(o.v.tags) ? o.v.tags : [], removeTags: [], tag: p.test_plate };
        if (o.v.vehicle_type) baseBody.vehicle_type = o.v.vehicle_type;
        if (o.v.emmission_standard) baseBody.emmission_standard = o.v.emmission_standard;
        const functionality = devBody.type === "obd" ? ["obd"] : ["tracker"];
        if (DRY) { console.log(`[DRY] ${p.original_imei}/${p.original_plate}\n  device  ${JSON.stringify(devBody)}\n  vehicle ${JSON.stringify(vehBody)}\n  spec    ${o.v.spec_id}\n  attach  ${JSON.stringify(functionality)}\n  base    ${JSON.stringify(baseBody)}`); continue; }
        const row = { original_imei: p.original_imei, original_plate: p.original_plate, test_imei: p.test_imei, test_plate: p.test_plate,
            source_account_id: o.v.account_id, target_account_id: TARGET, spec_id: o.v.spec_id || null, source_groups: (o.v.groups || []).map(g => g.id || g) };
        try {
            const dj = await post(`/idevice/createV2?acc_id=${TARGET}`, devBody);
            row.test_dev_id = dj.result && dj.result.id; if (!row.test_dev_id) throw new Error("device: " + st(dj));
            const vj = await post(`/vehicle/createV2?acc_id=${TARGET}`, vehBody);
            row.test_vid = vj.result && (vj.result.id || vj.result.v_id); if (!row.test_vid) throw new Error("vehicle: " + st(vj));
            if (o.v.spec_id) row.spec_resp = st(await post(`/vehicle/${row.test_vid}/attachspecV2/${o.v.spec_id}?acc_id=${TARGET}`, {}));
            row.attach_resp = st(await post(`/vehicle/${row.test_vid}/attachideviceV2/options/${row.test_dev_id}?acc_id=${TARGET}`, { operation: "ATTACH_AS_PRIMARY", functionality, skip_subs_validations: true, attach_reason: "switch-test" }));
            row.base_resp = st(await post(`/vehicle/${row.test_vid}/update/baseinfoV2?acc_id=${TARGET}`, baseBody));
            await sleep(400);
            const t = await info(p.test_imei);   // verify by read-back, never by response code
            const diffs = [];
            if (t.v.id !== row.test_vid) diffs.push(`not attached (vehicle ${t.v.id})`);
            if (t.v.account_id !== TARGET) diffs.push(`account ${t.v.account_id}`);
            if ((t.v.spec_id || null) !== (o.v.spec_id || null)) diffs.push(`spec ${t.v.spec_id}`);
            if ((t.v.protocol || null) !== (o.v.protocol || null)) diffs.push(`protocol ${t.v.protocol}!=${o.v.protocol}`);
            if (t.t.type !== o.t.type || t.t.base_type !== o.t.base_type) diffs.push(`dev ${t.t.type}/${t.t.base_type}`);
            if (t.v.obd_attached !== o.v.obd_attached) diffs.push(`obd_attached ${t.v.obd_attached}`);
            if ((t.v.emmission_standard || null) !== (o.v.emmission_standard || null)) diffs.push(`emm ${t.v.emmission_standard}`);
            const miss = (o.v.tags || []).filter(x => !(t.v.tags || []).includes(x)); if (miss.length) diffs.push(`missing tags ${miss.join("+")}`);
            row.diffs = diffs;
            console.log(`${diffs.length ? "DIFF" : "OK  "} ${p.original_imei}/${p.original_plate} -> ${p.test_imei}/${p.test_plate} vid=${row.test_vid}${diffs.length ? "  :: " + diffs.join("; ") : ""}`);
        } catch (e) { row.error = e.message; console.log(`FAIL ${p.original_imei}: ${e.message}`); }
        done.push(row);
        fs.writeFileSync(OUT, JSON.stringify(done, null, 2));
        await sleep(300);
    }
    console.log(`\n${done.filter(r => r.test_vid && !r.error).length} created in total, ${done.filter(r => r.error).length} failed (only "missing tags vehicle_verified" is an expected diff)`);
})().catch(e => { console.error(e); process.exit(1); });
