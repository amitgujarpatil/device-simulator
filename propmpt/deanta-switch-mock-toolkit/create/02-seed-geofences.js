#!/usr/bin/env node
// Fetch all geofences from the original source accounts (KMT, Sangitam, Raj Ratan,
// KMT car-carrier) and clone them into the live-pipeline test account.
// Preserves: tag, center/radius, points, is_hub, is_charging_station,
//            fence_tag_type, additional_info.
// Deduplicates across accounts by source _id (geofences have account_id[],
// so the same geofence may appear under multiple account list calls).
//
// Usage:
//   INTANGLES_TOKEN='<prod>' node seed-geofences.js
//
// Env vars:
//   INTANGLES_API      — default https://apis.intangles.com
//   INTANGLES_TOKEN    — required
//   TARGET_ACCOUNT     — default 1565345718959341568 (intangles-live-pipeline-test)
//   SOURCE_ACCOUNTS    — comma-separated, default = all 4 source accounts
//   DRY_RUN            — 1 = fetch + log only, no create calls
//   OUT                — output basename, default ./geofence-mapping
"use strict";
const fs = require("fs");

const API    = process.env.INTANGLES_API     || "https://apis.intangles.com";
const TOKEN  = process.env.INTANGLES_TOKEN;
const TARGET = process.env.TARGET_ACCOUNT    || "1565345718959341568";
const DRY    = process.env.DRY_RUN === "1";
const OUT    = process.env.OUT               || (__dirname + "/geofence-mapping");
const SRC_IDS = (process.env.SOURCE_ACCOUNTS ||
    "902855557075959808,166359736558682778,649034868264558592,901025643674730496").split(",");
// RESUME: load an existing mapping file and skip already-created source_ids
const RESUME_FILE = process.env.RESUME;
const alreadyDone = new Map(); // source_id → test_id
if (RESUME_FILE) {
    try {
        const prev = JSON.parse(fs.readFileSync(RESUME_FILE, "utf8"));
        for (const r of prev) {
            if (r.test_id && r.test_id !== "(dry)") alreadyDone.set(r.source_id, r.test_id);
        }
        console.log(`[RESUME] Loaded ${alreadyDone.size} already-created from ${RESUME_FILE}`);
    } catch (e) {
        console.error(`[RESUME] Failed to load ${RESUME_FILE}: ${e.message}`);
        process.exit(1);
    }
}

if (!TOKEN) { console.error("set INTANGLES_TOKEN"); process.exit(1); }

const H = {
    "Content-Type": "application/json",
    "intangles-client": "intangles_app",
    "Intangles-Session-Type": "web",
    "Intangles-User-Token": TOKEN,
    "Accept": "application/json"
};

const get  = p     => fetch(API + p,              { headers: H }).then(r => r.json());
const post = (p, b)=> fetch(API + p, { method: "POST", headers: H, body: JSON.stringify(b) }).then(r => r.json());

const sleep = ms => new Promise(r => setTimeout(r, ms));

// Fetch all pages of geofences for one account (listV2 returns {gf:[], paging:{total_count,page_size,page_num}})
async function fetchAll(accId) {
    const all = [];
    let pnum = 1;
    const psize = 2000;
    while (true) {
        const j = await get(`/geofence/listV2?acc_id=${accId}&pnum=${pnum}&psize=${psize}`);
        if (!j || j.status.code !== 200) throw new Error(`listV2 failed for ${accId}: ${JSON.stringify(j).slice(0,200)}`);
        const page = j.gf || [];
        all.push(...page);
        // Stop when we got fewer results than requested (last page) or nothing
        if (page.length < psize) break;
        pnum++;
        await sleep(200); // be gentle between pages
    }
    return all;
}

// Build the gobj for createV2 from a fetched geofence
function toGobj(g) {
    const gobj = { tag: g.tag };

    if (g.center) {
        // Store returns latitude/longitude; createV2 accepts lat/lng
        gobj.center = {
            lat: g.center.lat || g.center.latitude,
            lng: g.center.lng || g.center.longitude
        };
    }
    if (g.radius != null)               gobj.radius               = g.radius;
    // SDK POLYGON type requires {lat,lng}; listV2 returns {latitude,longitude}
    if (g.points && g.points.length)    gobj.points = g.points.map(p => ({ lat: p.lat || p.latitude, lng: p.lng || p.longitude }));
    if (g.is_hub != null)               gobj.is_hub               = g.is_hub;
    if (g.is_charging_station != null)  gobj.is_charging_station  = g.is_charging_station;
    if (g.fence_tag_type)               gobj.fence_tag_type       = g.fence_tag_type;
    if (g.additional_info)              gobj.additional_info      = g.additional_info;

    return gobj;
}

const ACCOUNT_NAMES = {
    "902855557075959808":  "KMT Parent",
    "166359736558682778":  "Sangitam Travels",
    "649034868264558592":  "Raj Ratan",
    "901025643674730496":  "KMT car carrier"
};

(async () => {
    console.log(`${DRY ? "[DRY] " : ""}Fetching geofences from ${SRC_IDS.length} source accounts → target ${TARGET} @ ${API}\n`);

    // Phase 1: Fetch + deduplicate
    const seen = new Map();   // source _id → {geofence, sourceAccounts[]}
    for (const accId of SRC_IDS) {
        const name = ACCOUNT_NAMES[accId] || accId;
        process.stdout.write(`  Fetching ${name} (${accId})... `);
        try {
            const list = await fetchAll(accId);
            let fresh = 0;
            for (const g of list) {
                const sid = g.id || g._id;
                if (!seen.has(sid)) {
                    seen.set(sid, { g, sourceAccounts: [accId] });
                    fresh++;
                } else {
                    seen.get(sid).sourceAccounts.push(accId);
                }
            }
            console.log(`${list.length} fetched (${fresh} new, ${list.length - fresh} already seen)`);
        } catch (e) {
            console.log(`FAIL — ${e.message}`);
        }
    }

    const unique = Array.from(seen.values());
    const hubCount = unique.filter(({ g }) => g.is_hub).length;
    console.log(`\nTotal unique: ${unique.length}  (hubs: ${hubCount})`);
    if (DRY) { console.log("[DRY] Skipping create calls."); }

    // Phase 2: Create in target account
    const mapping = [];
    let i = 0;
    for (const { g, sourceAccounts } of unique) {
        i++;
        const sid = g.id || g._id;
        const label = `[${i}/${unique.length}] ${g.tag}${g.is_hub ? " (hub)" : ""}`;

        // Skip already-created entries when resuming
        if (alreadyDone.has(sid)) {
            mapping.push({ source_id: sid, source_accounts: sourceAccounts, tag: g.tag, is_hub: !!g.is_hub, test_id: alreadyDone.get(sid) });
            continue;
        }

        if (DRY) {
            console.log(`  ${label} → (dry)`);
            mapping.push({ source_id: sid, source_accounts: sourceAccounts, tag: g.tag, is_hub: !!g.is_hub, test_id: "(dry)" });
            continue;
        }

        try {
            const gobj = toGobj(g);
            const j = await post(`/geofence/createV2?acc_id=${TARGET}`, { gobj });
            const r = j.result;
            const testId = r && (r.id || r._id || (Array.isArray(r) && r[0] && (r[0].id || r[0]._id)));
            if (!testId) throw new Error(JSON.stringify(j).slice(0, 200));
            console.log(`  ${label} → ${testId}`);
            mapping.push({ source_id: sid, source_accounts: sourceAccounts, tag: g.tag, is_hub: !!g.is_hub, test_id: testId });
        } catch (e) {
            console.log(`  ${label} → FAIL: ${e.message}`);
            mapping.push({ source_id: sid, source_accounts: sourceAccounts, tag: g.tag, is_hub: !!g.is_hub, test_id: null, error: e.message });
        }

        await sleep(Number(process.env.DELAY_MS) || 500);
    }

    // Phase 3: Write output files
    fs.writeFileSync(OUT + ".json", JSON.stringify(mapping, null, 2));

    const cols = ["source_id", "tag", "is_hub", "test_id", "source_accounts"];
    const csvRows = mapping.map(r => [r.source_id, JSON.stringify(r.tag), r.is_hub, r.test_id || "", JSON.stringify(r.source_accounts)].join(","));
    fs.writeFileSync(OUT + ".csv", cols.join(",") + "\n" + csvRows.join("\n") + "\n");

    const ok = mapping.filter(r => r.test_id && r.test_id !== "(dry)").length;
    const failed = mapping.filter(r => !r.test_id || r.error).length;
    const mdHeader = `# Geofence mapping — live-pipeline test account\n\nTarget: \`${TARGET}\` (intangles-live-pipeline-test).\n\nCreated: ${ok} | Failed: ${failed} | Hubs: ${mapping.filter(r=>r.is_hub).length}\n\n`;
    const mdTable = "| Source ID | Tag | Hub | Test ID |\n|---|---|---|---|\n" +
        mapping.map(r => `| ${r.source_id} | ${r.tag} | ${r.is_hub ? "✓" : ""} | ${r.test_id || "FAIL"} |`).join("\n") + "\n";
    fs.writeFileSync(OUT + ".md", mdHeader + mdTable);

    console.log(`\nDone: ${ok} created, ${failed} failed`);
    console.log(`Wrote ${OUT}.json / .csv / .md`);
})().catch(e => { console.error(e); process.exit(1); });
