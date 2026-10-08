#!/usr/bin/env node
// Read-only: after a replay, compare alerts on the MOCK vehicle vs its SOURCE vehicle, window by window.
//   INTANGLES_TOKEN=... SRC_VID=1535690443373674496 MOCK_VID=1606688593688920064 \
//     WINDOWS=data/deanta-alert-replay-windows.jsonc node alerts/alerts-compare.js
// WINDOWS: a replay-windows .jsonc (uses main.windows[] / windows[] / addons[] with start_ms/end_ms), or
//          inline "start_ms-end_ms,start_ms-end_ms".
// Alert timestamps are the packets' event time, so a replay of September data shows up in September.
const fs = require("fs");
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN, SRC = process.env.SRC_VID, MOCK = process.env.MOCK_VID;
if (!TOKEN || !SRC || !MOCK || !process.env.WINDOWS) { console.error("set INTANGLES_TOKEN, SRC_VID, MOCK_VID, WINDOWS"); process.exit(1); }
const H = { "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(async r => { const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _raw: t.slice(0, 200) }; } });
const iso = t => new Date(t).toISOString().slice(0, 19) + "Z";
async function alerts(vid, s, e) { const all = []; for (let p = 1; p <= 20; p++) { const j = await get(`/alertlog/vehicle/${vid}/logsV2/${s}/${e}?psize=500&pnum=${p}`); const a = Array.isArray(j) ? j : []; all.push(...a); if (a.length < 500) break; } return all; }
const cnt = a => { const c = {}; a.forEach(x => c[x.type] = (c[x.type] || 0) + 1); return c; };
function loadWindows(w) {
    if (/^\d+-\d+/.test(w)) return w.split(",").map((x, i) => { const [s, e] = x.split("-").map(Number); return { id: "w" + (i + 1), start_ms: s, end_ms: e }; });
    const j = JSON.parse(fs.readFileSync(w, "utf8").replace(/^\s*\/\/.*$/gm, "").replace(/([^:"])\/\/.*$/gm, "$1"));
    return [...((j.main && j.main.windows) || []), ...(j.windows || []), ...(j.addons || [])];
}
(async () => {
    for (const w of loadWindows(process.env.WINDOWS)) {
        const [a, b] = await Promise.all([alerts(SRC, w.start_ms, w.end_ms), alerts(MOCK, w.start_ms, w.end_ms)]);
        const ca = cnt(a), cb = cnt(b); const types = [...new Set([...Object.keys(ca), ...Object.keys(cb)])].sort();
        console.log(`\n${w.id} ${iso(w.start_ms)} .. ${iso(w.end_ms)}   source=${a.length} mock=${b.length}`);
        types.forEach(t => console.log(`   ${t.padEnd(26)} source ${String(ca[t] || 0).padStart(4)}  mock ${String(cb[t] || 0).padStart(4)}${(cb[t] || 0) < (ca[t] || 0) ? "  <-- missing " + ((ca[t] || 0) - (cb[t] || 0)) : ""}`));
    }
})().catch(e => { console.error(e); process.exit(1); });
