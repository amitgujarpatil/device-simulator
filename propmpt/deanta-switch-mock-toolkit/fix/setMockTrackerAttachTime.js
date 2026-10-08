"use strict";
/**
 * Set tracker_attach_time on the intangles-switch-test-deanta mock vehicles (EU).
 *
 * Lives at the top level of intangles/Backend/ and runs inside any EU intangles backend pod
 * (parser, alert-checker, ...), from the Backend folder:
 *     node setMockTrackerAttachTime.js            # dry run: prints current vs target, writes nothing
 *     APPLY=1 node setMockTrackerAttachTime.js    # writes, then reads every vehicle back
 *
 * Why not the API: /vehicle/:vid/update/baseinfoV2 only copies an allowlist of fields and drops
 * tracker_attach_time, and attaching a device always stamps the current time. So this writes the
 * field on the meta vehicle through the Intangles SDK, the same way dataIO/intanglesAPI/vehicle.js
 * does for tags, then clears the vehicleinfo-<vid> and ideviceinfo-<imei> cache entries.
 *
 * tracker_attach_time is epoch MILLISECONDS (data_apis vehicleStore sets new Date().getTime();
 * the parser compares it against msg.tracker.timestamp in ms). The mocks currently hold
 * 1767229200, which is the same instant in seconds.
 *
 * Env:
 *   APPLY=1      actually write (default: dry run)
 *   TARGET_MS    epoch milliseconds (default 1767225600000 = 2026-01-01T00:00:00Z)
 *   ONLY_VIDS    comma-separated subset of vehicle ids, e.g. to try one vehicle first
 */
const cn = require("./Config/config");

const ACCOUNT_ID = "1606685851746566144"; // intangles-switch-test-deanta
const TARGET_MS = Number(process.env.TARGET_MS || Date.UTC(2026, 0, 1));
const APPLY = process.env.APPLY === "1";
const BY_USER = "setMockTrackerAttachTime";

// mock vehicle id -> mock imei (from deanta-mock-seeded.json)
const MOCK_VEHICLES = {
    "1606688412650176512": "866308061027751", // MK75PXJ
    "1606688547622879232": "866308061764833", // MK24MXW
    "1606688561451499520": "866308064768088", // MK76RDZ
    "1606688571882733568": "866308065502064", // MK26PNO
    "1606688583924580352": "866308060185709", // MK75PXH
    "1606688593688920064": "866308060277944", // MK74ULE
    "1606688604023685120": "866308068006683", // MK74ULC
    "1606688614790463488": "866308069347805", // MK25TXE
    "1606688625196531712": "866308068410778", // MK74ULD
    "1606688634893762560": "866308064943145", // MK72VNY
    "1606688646134497280": "866308069713956", // MK25TXF
    "1606688657714970624": "866308065261646", // MK25TVW
    "1606688669215752192": "866308068278555", // MK72VNX
    "1606688680083193856": "866308067533331", // MK26PNU
    "1606688690749308928": "866308065444242", // MK26PNN
    "1606688700824027136": "866308068402494", // MK26PNK
    "1606688712182202368": "866308061728770", // MK25TXG
    "1606688723355828224": "866308067689166", // MK75PXL
    "1606688736601440256": "866308060028024", // MK26PNJ
    "1606688745875046400": "866308060226958", // MK21UPZ
    "1606688756310474752": "866308063595201", // MK24UFM
    "1606688766817206272": "867624069096401", // MK26PNZ
    "1606688776652849152": "869305078631705", // MK74ULB
    "1606688787029557248": "869305074205843", // MK74ULA
    "1606688797305602048": "869305075126600", // MK75PXK
    "1606688808286289920": "869305070848521", // MK26PNL
    "1606688821326381056": "869305074633291", // MK26PNE
    "1606688831984107520": "869305072039590", // MK26PNX
    "1606688841521954816": "866308060737830" // MK26PNF
};

// fields that must not move when only the attach time is written
const GUARD_FIELDS = [ "plate", "account_id", "spec_id", "protocol", "obd_attached", "tracker_id", "tracker_imei", "tags" ];

function out(line) {
    process.stdout.write(line + "\n");
}

function iso(t) {
    if (t === undefined || t === null) return String(t);
    const n = Number(t);
    if (!isFinite(n)) return String(t);
    return new Date(n < 1e12 ? n * 1000 : n).toISOString() + (n < 1e12 ? " (seconds)" : "");
}

async function main() {
    if (!Number.isInteger(TARGET_MS) || TARGET_MS < 1e12) {
        throw new Error(`TARGET_MS must be epoch milliseconds, got ${process.env.TARGET_MS}`);
    }
    await cn.init();
    // required after cn.init(): the SDK and cache modules read config at load time
    const Intangles = require("@intangles/berries/intangles_sdk/intangles");
    const { clearVehicleInfoCache } = require("@intangles/berries/io_utils/cache/vehicleCache.js");
    const nc = require("./Config/namingConventions.js");
    const cacheIO = require("./Cache/cacheIO.js");

    async function readVehicle(vid) {
        const query = Intangles.Object.Vehicle.AggregateQuery();
        query.match(new Intangles.Filter.Property("_id").equalTo(vid));
        query.projection([ "_id", "tracker_attach_time", ...GUARD_FIELDS ]);
        const rows = await query.execute();
        return rows && rows[0];
    }

    const only = (process.env.ONLY_VIDS || "").split(",").map(s => s.trim()).filter(Boolean);
    const vids = Object.keys(MOCK_VEHICLES).filter(v => !only.length || only.includes(v));
    out(`${APPLY ? "APPLY" : "DRY RUN"}: ${vids.length} vehicles -> tracker_attach_time=${TARGET_MS} (${iso(TARGET_MS)})\n`);

    let ok = 0, skipped = 0, failed = 0;
    for (const vid of vids) {
        const imei = MOCK_VEHICLES[vid];
        try {
            const before = await readVehicle(vid);
            if (!before || String(before.account_id) !== ACCOUNT_ID) {
                out(`SKIP ${vid}: not found in mock account (account_id=${before && before.account_id})`);
                skipped++;
                continue;
            }
            const label = `${before.plate} vid=${vid} imei=${imei}`;
            if (Number(before.tracker_attach_time) === TARGET_MS) {
                out(`SKIP ${label}: already ${TARGET_MS}`);
                skipped++;
                continue;
            }
            if (!APPLY) {
                out(`WOULD ${label}: ${before.tracker_attach_time} (${iso(before.tracker_attach_time)}) -> ${TARGET_MS}`);
                continue;
            }

            const vehicle = new Intangles.Object.Vehicle(vid);
            vehicle.tracker_attach_time = TARGET_MS;
            vehicle.__updatedby = BY_USER;
            await vehicle.save();

            await clearVehicleInfoCache(vid);
            await cacheIO.del(nc.getIdeviceInfoKey(imei));

            const after = await readVehicle(vid);
            const moved = GUARD_FIELDS.filter(k => JSON.stringify(before[k]) !== JSON.stringify(after[k]));
            if (Number(after.tracker_attach_time) === TARGET_MS && !moved.length) {
                out(`OK   ${label}: ${before.tracker_attach_time} -> ${after.tracker_attach_time}`);
                ok++;
            } else {
                out(`FAIL ${label}: now ${after.tracker_attach_time}${moved.length ? `, also changed: ${moved.join(",")}` : ""}`);
                failed++;
            }
        } catch (err) {
            out(`ERR  ${vid}: ${err && err.message ? err.message : err}`);
            failed++;
        }
    }
    out(`\n${APPLY ? `updated ${ok}` : "dry run"}, skipped ${skipped}, failed ${failed}`);
    return failed;
}

main()
    .then(failed => process.exit(failed ? 1 : 0))
    .catch(err => {
        process.stderr.write(`${err && err.stack ? err.stack : err}\n`);
        process.exit(1);
    });
