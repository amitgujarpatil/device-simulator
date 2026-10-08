#!/usr/bin/env node
// Copy the source account's UI config (pages, units, currency, fleet_health flags ...) onto the mock account.
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> node create/01-ui-config.js            # dry run (prints configs)
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> APPLY=1 node create/01-ui-config.js    # writes + reads back
// Route: POST /config/add/accountV2/  body {account_id, configs:[{section:"ui", name, value}]}
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, ACC = process.env.TARGET_ACCOUNT, APPLY = process.env.APPLY === "1";
const D = process.env.DATA_DIR || path.join(__dirname, "..", "data", "source-dump");
if (!TOKEN || !ACC) { console.error("set INTANGLES_TOKEN and TARGET_ACCOUNT"); process.exit(1); }
const H = { "Content-Type": "application/json", "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const src = JSON.parse(fs.readFileSync(path.join(D, "config-all-accountV2.json"), "utf8")).result.ui || {};
const ui = {};
for (const [k, v] of Object.entries(src)) if (!k.startsWith("__") && v !== null) ui[k] = v;
const configs = Object.entries(ui).map(([name, value]) => ({ section: "ui", name, value }));
const canon = v => JSON.stringify(v, (k, x) => (x && typeof x === "object" && !Array.isArray(x)) ? Object.keys(x).sort().reduce((o, kk) => (o[kk] = x[kk], o), {}) : x);
(async () => {
    console.log(`${configs.length} ui keys: ${Object.keys(ui).join(", ")}`);
    if (!APPLY) { console.log("dry run — set APPLY=1 to write"); return; }
    const r = await fetch(`${API}/config/add/accountV2/?acc_id=${ACC}`, { method: "POST", headers: H, body: JSON.stringify({ account_id: ACC, configs }) });
    console.log("POST", r.status, (await r.text()).slice(0, 200));
    const got = ((await (await fetch(`${API}/config/all/accountV2/${ACC}`, { headers: H })).json()).result || {}).ui || {};
    let bad = 0;
    for (const [k, v] of Object.entries(ui)) if (canon(got[k]) !== canon(v)) { bad++; console.log("MISMATCH", k, JSON.stringify(got[k])); }
    console.log(`read-back: ${configs.length - bad}/${configs.length} keys match`);
    // "drive_iq" in pages can be rejected for some account types (configController isDriveIQPageAccessibleToAccount).
})();
