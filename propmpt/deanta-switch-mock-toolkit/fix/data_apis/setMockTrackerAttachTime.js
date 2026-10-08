"use strict";
/**
 * Set tracker_attach_time on the intangles-switch-test-deanta mock vehicles.
 *
 * Run inside a data_apis pod (EU), from the app root, after copying this file into scripts/:
 *     node scripts/setMockTrackerAttachTime.js            # dry run: prints current vs target, writes nothing
 *     APPLY=1 node scripts/setMockTrackerAttachTime.js    # writes, then reads every vehicle back
 *
 * Why a pod script: the HTTP route /vehicle/:vid/update/baseinfoV2 only copies an allowlist of
 * fields and silently drops tracker_attach_time. The controller/store underneath it copy any
 * field not on their deny-list, so this calls vehicleController.updateVehicleBaseInfoV2 directly.
 * That path saves the meta vehicle, updates the core vehicle, clears vehicleinfo-<vid> and
 * writes the vehicle change log, the same as a normal base-info update.
 *
 * Env:
 *   APPLY=1        actually write (default: dry run)
 *   TARGET_MS      attach time in epoch milliseconds (default 1767225600000 = 2026-01-01T00:00:00Z)
 *   ONLY_VIDS      comma-separated subset of vehicle ids, e.g. to try one vehicle first
 */
const cn = require("../configuration/config");

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
    "1606688841521954816": "866308060737830"  // MK26PNF
};

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
    // required after cn.init(): these modules read config at load time
    const vehicleController = require("../controllers/vehicleController.js");
    const redisClient = require("../stores/caching_utilities/redisClient.js");
    const namingConvention = require("../stores/caching_utilities/cacheNamingConvention.js");

    const only = (process.env.ONLY_VIDS || "").split(",").map(s => s.trim()).filter(Boolean);
    const vids = Object.keys(MOCK_VEHICLES).filter(v => !only.length || only.includes(v));
    console.log(`${APPLY ? "APPLY" : "DRY RUN"}: ${vids.length} vehicles -> tracker_attach_time=${TARGET_MS} (${iso(TARGET_MS)})\n`);

    let ok = 0, skipped = 0, failed = 0;
    for (const vid of vids) {
        const imei = MOCK_VEHICLES[vid];
        try {
            const before = await vehicleController.getVehicleById(vid);
            if (!before || String(before.account_id) !== ACCOUNT_ID) {
                console.log(`SKIP ${vid}: not in mock account (account_id=${before && before.account_id})`);
                skipped++;
                continue;
            }
            const label = `${before.plate} vid=${vid} imei=${imei}`;
            if (Number(before.tracker_attach_time) === TARGET_MS) {
                console.log(`SKIP ${label}: already ${TARGET_MS}`);
                skipped++;
                continue;
            }
            if (!APPLY) {
                console.log(`WOULD ${label}: ${before.tracker_attach_time} (${iso(before.tracker_attach_time)}) -> ${TARGET_MS}`);
                continue;
            }

            await vehicleController.updateVehicleBaseInfoV2(vid, {
                tracker_attach_time: TARGET_MS,
                addTags: [],
                removeTags: [],
                by_user: BY_USER
            }, { user: BY_USER, given_by: BY_USER });

            // the store clears vehicleinfo-<vid>; also clear the device lookup entry the parser reads
            await redisClient.del([namingConvention.getVehicleInfoKey(vid), namingConvention.getIdeviceInfoKey(imei)]);

            const after = await vehicleController.getVehicleById(vid);
            const unchanged = ["plate", "spec_id", "protocol", "obd_attached", "tracker_imei", "account_id"]
                .filter(k => JSON.stringify(before[k]) !== JSON.stringify(after[k]));
            if (Number(after.tracker_attach_time) === TARGET_MS && !unchanged.length) {
                console.log(`OK   ${label}: ${before.tracker_attach_time} -> ${after.tracker_attach_time}`);
                ok++;
            } else {
                console.log(`FAIL ${label}: now ${after.tracker_attach_time}${unchanged.length ? `, also changed: ${unchanged.join(",")}` : ""}`);
                failed++;
            }
        } catch (err) {
            console.log(`ERR  ${vid}: ${err && err.message ? err.message : err}`);
            failed++;
        }
    }
    console.log(`\n${APPLY ? `updated ${ok}` : "dry run"}, skipped ${skipped}, failed ${failed}`);
    return failed;
}

main()
    .then(failed => process.exit(failed ? 1 : 0))
    .catch(err => {
        console.error(err);
        process.exit(1);
    });
