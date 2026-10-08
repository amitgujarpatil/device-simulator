#!/usr/bin/env node
// Read-only: diff device + vehicle config of several IMEIs side by side (e.g. a working mock, a failing mock and the
// failing mock's source), plus each vehicle's alert config and recent alert counts. Use when "mock X alerts, mock Y doesn't".
//   INTANGLES_TOKEN=... IMEIS="OK=866308060277944,BAD=866308068006683,SRC=866308064386378" node alerts/compare-mocks.js
// Only fields that DIFFER are printed; ids, plates, timestamps and other per-unit fields are skipped.
const API = process.env.INTANGLES_API || "https://apis.intangles-aws-eu-north-1.eu.intangles.com";
const TOKEN = process.env.INTANGLES_TOKEN;
if (!TOKEN || !process.env.IMEIS) { console.error("set INTANGLES_TOKEN and IMEIS=label=imei,label=imei"); process.exit(1); }
const H = { "intangles-client": "intangles_app", "Intangles-Session-Type": "web", "Intangles-User-Token": TOKEN, "Accept": "application/json" };
const get = p => fetch(API + p, { headers: H }).then(async r => { const t = await r.text(); try { return JSON.parse(t); } catch (e) { return { _http: r.status, _raw: t.slice(0, 200) }; } });
const iso = t => t ? new Date(t > 1e12 ? t : t * 1000).toISOString().slice(0, 19) + "Z" : t;
const IMEIS = Object.fromEntries(process.env.IMEIS.split(",").map(p => p.split("=")));
const skip = /^(id|_id|__id|v_id|t_id|tracker_id|vid|imei|tracker_imei|t_imei|plate|tag|tagged|vin|__lastupdatetime|__lastcreatedtime|created_time|device_installation_id|first_deployment_info|accounts_with_access|account_id|accId|stn|tv|stm|sims|carton|assembly.*|health|suggested_actions|drivers|active_driver|location|timestamp)$/;
(async () => {
    const info = {};
    for (const [k, im] of Object.entries(IMEIS)) { const j = await get(`/idevice/${im}/allinfoV2`); info[k] = j.result || j; }
    const names = Object.keys(IMEIS);
    for (const part of ["tracker", "vehicle"]) {
        const keys = [...new Set(Object.values(info).flatMap(r => Object.keys(r[part] || {})))].filter(k => !skip.test(k)).sort();
        console.log(`\n===== ${part} fields that differ =====`);
        for (const k of keys) {
            const vals = names.map(n => JSON.stringify((info[n][part] || {})[k]));
            if (new Set(vals).size > 1) console.log(`  ${k.padEnd(28)} ${names.map((n, i) => n + "=" + (vals[i] || "undefined").slice(0, 70)).join("  |  ")}`);
        }
    }
    for (const n of names) {
        const t = info[n].tracker || {}, v = info[n].vehicle || {};
        console.log(`\n  ${n}: dev type=${t.type}/${t.base_type} attached_fn=${JSON.stringify(t.attached_functionality)} | veh ${v.id} acc=${v.account_id} obd=${v.obd_attached} spec=${v.spec_id} proto=${v.protocol} tracker_attach_time=${v.tracker_attach_time} (${iso(v.tracker_attach_time)})`);
        if (!v.id) continue;
        const ac = (await get(`/alertconfigV2/vehicle/${v.id}/all`)).result || {};
        const end = Date.now(), start = end - 6 * 864e5;
        const al = await get(`/alertlog/vehicle/${v.id}/logsV2/${start}/${end}?psize=500&pnum=1`); const arr = Array.isArray(al) ? al : [];
        const c = {}; arr.forEach(x => c[x.type] = (c[x.type] || 0) + 1);
        console.log(`     vehicle alert_config keys=[${Object.keys(ac.alert_config || {}).join(",")}] | alerts last 6d: ${arr.length} ${JSON.stringify(c)}`);
    }
})().catch(e => { console.error(e); process.exit(1); });
