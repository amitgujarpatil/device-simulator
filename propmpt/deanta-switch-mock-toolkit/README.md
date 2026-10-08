# Switch mock-account toolkit (Deanta → `intangles-switch-test-deanta`)

Scripts used to clone the EU customer account **Deanta** into a POC mock account, replay real vehicle data onto the
mock IMEIs, and debug which alerts come through. Everything talks to **EU production**
(`https://apis.intangles-aws-eu-north-1.eu.intangles.com`). Source accounts are only ever read; the only account
written to is the mock.

## What exists today (2026-10-05)

| | |
|---|---|
| Source account | `1482046680973967360` — Deanta (EU, paid_pilot, 29 vehicles) |
| Mock account | `1606685851746566144` — `intangles-switch-test-deanta` (stage poc, direct, prepaid) |
| Mock fleet | 29 devices + vehicles, `MK…` plates, device tag `switchtest-<imei>` — `data/deanta-mock-seeded.json` |
| Geofences | 5 (2 hubs) — `data/geofence-mapping-deanta.json` |
| Groups | Haydock Depot `1606689036791971840`, Lancaster Way Depot `1606689046208184320` (14 each) |
| Alert config | Deanta thresholds, no recipients; geofence entry/exit on the 5 new geofences |
| tracker_attach_time | all 29 = `1767225600000` (2026-01-01 00:00 UTC, **milliseconds**) |
| Replay plans | `data/deanta-alert-replay-windows.jsonc` (AY74ULE), `data/deanta-alert-replay-windows-AY74ULA.jsonc` |

## Setup

```bash
export INTANGLES_TOKEN='<EU user token>'   # sensitive: never commit, never paste into files; expires on logout
# Node 18+ (scripts use global fetch). Run from this folder.
```

Every script reads the token from the environment and never writes it to disk.

## Folder map

| Folder | Scripts | Writes? |
|---|---|---|
| `discovery/` | `collect-account.sh`, `collect-per-vehicle.js`, `build-mapping.js` | read-only (prod) |
| `create/` | `00-create-account.sh`, `01-ui-config.js`, `02-seed-geofences.js`, `03-seed-vehicles.js`, `04-groups.js`, `05-alert-config.js` | **writes to prod mock account** |
| `alerts/` | `alerts-scan.js`, `plan-replay-windows.js`, `alerts-compare.js`, `compare-mocks.js` | read-only |
| `fix/` | `setMockTrackerAttachTime.js` (intangles `Backend/` pod), `data_apis/setMockTrackerAttachTime.js` (data_apis pod) | **writes** — run inside a prod pod |
| `debug/` | `DASH0-QUERIES.md` | — |
| `data/` | mappings and replay plans for the Deanta clone | — |

## Cloning another account (order matters)

```bash
SOURCE_ACCOUNT=<id> bash discovery/collect-account.sh          # dump source (customer data, keep internal)
node discovery/collect-per-vehicle.js
node discovery/build-mapping.js                                 # -> data/mock-mapping.json  — REVIEW IT
MOCK_NAME=<name> bash create/00-create-account.sh               # dry run, then APPLY=1     -> note the new id
export TARGET_ACCOUNT=<new mock id>
node create/01-ui-config.js                                     # dry run, then APPLY=1
SOURCE_ACCOUNTS=$SOURCE_ACCOUNT DRY_RUN=1 node create/02-seed-geofences.js   # then without DRY_RUN
#   then: POST /geofence/$TARGET_ACCOUNT/createGeofenceIndexByAccId  (map is blank until the index is rebuilt)
DRY_RUN=1 LIMIT=2 node create/03-seed-vehicles.js ; LIMIT=1 node create/03-seed-vehicles.js ; node create/03-seed-vehicles.js
node create/04-groups.js
node create/05-alert-config.js                                  # dry run, then APPLY=1
```

`02-seed-geofences.js` is the older India toolkit script: set `INTANGLES_API` to the EU URL, `TARGET_ACCOUNT`,
`SOURCE_ACCOUNTS` and `OUT` (it defaults to the India test account and KMT/Sangitam/Raj Ratan sources).

There is **no delete route** for accounts, devices or vehicles — only detach. Dry-run first, create one, check, then the rest.

## Planning a replay

```bash
SOURCE_ACCOUNT=1482046680973967360 DAYS=30 node alerts/alerts-scan.js      # alert types per source vehicle
node alerts/plan-replay-windows.js                                         # fewest 24 h windows covering each vehicle's types
PLATES=AY74ULA node alerts/plan-replay-windows.js ; FLEET=1 node alerts/plan-replay-windows.js
```

Replay = fetch the **source IMEI's** raw data for each window and publish it as the **mock IMEI** (done by the team's
replay service, not by these scripts). Data must be newer than the vehicle's `tracker_attach_time` or the parser
drops it.

## After a replay

```bash
SRC_VID=<source vid> MOCK_VID=<mock vid> WINDOWS=data/deanta-alert-replay-windows.jsonc node alerts/alerts-compare.js
IMEIS="OK=<working mock imei>,BAD=<failing mock imei>,SRC=<its source imei>" node alerts/compare-mocks.js
```

Then trace the IMEI in Dash0 with `debug/DASH0-QUERIES.md`.

## Things learned the hard way

| Symptom | Cause | Fix |
|---|---|---|
| Mock IMEIs missing in Device Management (`idevice/listV3?valid_intangles_device=true`) | filter requires `manufacturing_batch`; API-created devices have none | cosmetic only — no alert code reads it. Set a fake batch via `/idevice/<id>/updateV2` if the screen matters |
| Some mocks raise only geofence/data_loss alerts | IMEI has a batch-mode (history) session → data goes to the history pipeline; `alert-checker-history` / `trip-decider-history` stall ~90 s into a replay | not a vehicle-config issue; check history consumer lag (Kafka) |
| Replayed packets silently dropped | parser discards packets older than `vehicle.tracker_attach_time` (`Parser/parser.js:1110/1186`); attach stamps "now" | set it to an earlier date with `fix/setMockTrackerAttachTime.js` in a pod (no HTTP route can set it) |
| `tracker_attach_time` looks like `1767229200` | someone wrote **seconds**; platform uses **milliseconds** | compare with an original vehicle: 13 digits = ms |
| `baseinfoV2` returns 200 but a field didn't change | the HTTP route copies an allowlist of fields; the store below accepts anything | use the controller/SDK in a pod script |
| `vehicle_verified` tag missing on mocks | not settable through `addTags` | expected |
| New account has alert recipients nobody set | platform defaults on algo_output / def_chori / dtc / fuel_chori | leave or clear deliberately |
| `/geofence/createV2` adds alert config by itself | entry/exit config per new geofence id | don't copy the source geofence alert block |
| `/account/<id>` or v1 `/alertconfig/account/<id>/all` → 503 appacitive | legacy routes in EU | use `/accountV2/<id>`, `/alertconfigV2/...` |
| `driver/get`, `reminder/list`, `subscription/list/v2` return other accounts | wrong/ignored account filter | drivers need `account_id=`; filter the others yourself |
| Early alerts of a window missing on the mock | no lead-in data (stoppage/idling/fuel need prior state) | start windows ≥ 2 h before the first alert (`LEAD_HOURS`) |

## Data sensitivity

`data/*` contains real Deanta IMEIs, plates and vehicle ids. Internal sharing only; never commit to a public place.
Do not add source dumps (`data/source-dump/`, created by `collect-account.sh`) to shared bundles — they hold customer
user names and emails.
