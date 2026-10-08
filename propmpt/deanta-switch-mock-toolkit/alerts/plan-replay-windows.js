#!/usr/bin/env node
// Offline: from alerts-scan.json, pick the fewest 24-hour replay windows that cover every alert type a vehicle raised
// (greedy set cover over hour-aligned windows). Each window starts >= LEAD_HOURS before the first alert it counts, so
// state-based alerts (stoppage, idling, fuel fill) get lead-in data.
//   node alerts/plan-replay-windows.js                      # ranks all vehicles by types, plans the top ones
//   PLATES=AY74ULE,AY74ULA node alerts/plan-replay-windows.js
//   FLEET=1 node alerts/plan-replay-windows.js              # windows across vehicles to cover every type in the fleet
// Env: SCAN (default data/alerts-scan.json), WINDOW_HOURS (24), LEAD_HOURS (2), TOP (5)
const fs = require("fs");
const path = require("path");
const SCAN = process.env.SCAN || path.join(__dirname, "..", "data", "alerts-scan.json");
const HOUR = 36e5, WIN = (+(process.env.WINDOW_HOURS || 24)) * HOUR, LEAD = (+(process.env.LEAD_HOURS || 2)) * HOUR;
const d = JSON.parse(fs.readFileSync(SCAN, "utf8"));
const iso = t => new Date(t).toISOString().slice(0, 19) + "Z";
const events = vs => { const e = []; for (const v of vs) for (const [ty, ts] of Object.entries(v.types)) ts.forEach(t => e.push({ v, ty, t })); return e.sort((a, b) => a.t - b.t); };

function plan(vs) {
    const e = events(vs);
    const cands = [];
    for (const v of vs) {
        const ev = e.filter(x => x.v === v); if (!ev.length) continue;
        for (let s = Math.floor(ev[0].t / HOUR) * HOUR - WIN; s <= ev[ev.length - 1].t; s += HOUR) {
            const cnt = {}; let n = 0;
            for (const x of ev) if (x.t >= s + LEAD && x.t <= s + WIN) { cnt[x.ty] = (cnt[x.ty] || 0) + 1; n++; }
            if (n) cands.push({ v, s, e: s + WIN, cnt, n });
        }
    }
    const left = new Set(e.map(x => x.ty)); const pick = [];
    while (left.size) {
        let best = null, bg = 0;
        for (const c of cands) { const g = Object.keys(c.cnt).filter(t => left.has(t)).length; if (g > bg || (g === bg && g > 0 && c.n > best.n)) { best = c; bg = g; } }
        if (!best) break;
        const got = Object.keys(best.cnt).filter(t => left.has(t)); got.forEach(t => left.delete(t)); pick.push({ ...best, got });
    }
    return pick.sort((a, b) => a.s - b.s);
}
function show(title, vs) {
    const p = plan(vs);
    console.log(`\n=== ${title}: ${new Set(events(vs).map(x => x.ty)).size} types in ${p.length} windows of ${WIN / HOUR}h`);
    for (const w of p) console.log(`  ${w.v.plate} ${w.v.imei} | ${iso(w.s)} -> ${iso(w.e)} | start_ms ${w.s} end_ms ${w.e}\n     new: ${w.got.join(", ")}\n     all: ${JSON.stringify(w.cnt)}`);
}
if (process.env.FLEET === "1") show("whole fleet", d.vehicles);
else {
    const plates = (process.env.PLATES || "").split(",").filter(Boolean);
    const vs = plates.length ? d.vehicles.filter(v => plates.includes(v.plate))
        : d.vehicles.slice().sort((a, b) => Object.keys(b.types).length - Object.keys(a.types).length).slice(0, +(process.env.TOP || 5));
    for (const v of vs) show(`${v.plate} (vid ${v.vid})`, [v]);
}
