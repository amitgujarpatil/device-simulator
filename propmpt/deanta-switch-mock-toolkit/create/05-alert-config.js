#!/usr/bin/env node
// Copy the source account's alert THRESHOLDS onto the mock account — never its recipients.
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> node create/05-alert-config.js            # dry run (prints configs)
//   INTANGLES_TOKEN=... TARGET_ACCOUNT=<mock id> APPLY=1 node create/05-alert-config.js    # writes + reads back
// Route: POST /alertconfig/account/addV2  body {account_id, configs:[{type, is_enabled, config, rest_time?}]}
// - Only alert types that carry a "config" block are copied; "users" lists are dropped (no mock alerts to customers).
// - The "geofence" block is skipped: /geofence/createV2 already adds an entry/exit alert config for every new
//   geofence, keyed by the NEW geofence id (source ids would not match anyway).
// - A new account already has platform defaults, incl. default Intangles user lists on algo_output/def_chori/dtc/
//   fuel_chori; this script leaves those alone.
const fs = require("fs");
const path = require("path");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, ACC = process.env.TARGET_ACCOUNT, APPLY = process.env.APPLY === "1";
const D = process.env.DATA_DIR || path.join(__dirname, "..", "data", "source-dump");
if (!TOKEN || !ACC) { console.error("set INTANGLES_TOKEN and TARGET_ACCOUNT"); process.exit(1); }
const H = { "Content-Type": "application/json", "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const src = JSON.parse(fs.readFileSync(path.join(D, "alertconfigV2-account.json"), "utf8")).result.alert_config || {};
const configs = [];
for (const [type, c] of Object.entries(src)) {
    if (type === "geofence" || !c || !c.config) continue;
    const e = { type, is_enabled: c.is_enabled !== false, config: c.config };
    if (c.rest_time !== undefined) e.rest_time = c.rest_time;
    configs.push(e);
}
(async () => {
    configs.forEach(c => console.log(`${c.type.padEnd(22)} ${JSON.stringify(c.config)}${c.rest_time !== undefined ? " rest_time=" + c.rest_time : ""}`));
    if (!APPLY) { console.log("dry run — set APPLY=1 to write"); return; }
    const r = await fetch(`${API}/alertconfig/account/addV2?acc_id=${ACC}`, { method: "POST", headers: H, body: JSON.stringify({ account_id: ACC, configs }) });
    console.log("POST", r.status, (await r.text()).slice(0, 160));
    const a = ((await (await fetch(`${API}/alertconfigV2/account/${ACC}/all`, { headers: H })).json()).result || {}).alert_config || {};
    for (const c of configs) {
        const got = a[c.type] || {};
        console.log(`${JSON.stringify(got.config) === JSON.stringify(c.config) ? "OK  " : "DIFF"} ${c.type} ${JSON.stringify(got.config)}`);
    }
    console.log("geofence entries:", Object.keys(a.geofence || {}).length, "| types with user lists:", Object.keys(a).filter(k => Array.isArray(a[k].users) && a[k].users.length).join(","));
})();
