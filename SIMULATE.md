# simulate-device.js

Simulates an IoT device doing a full history-replay + live-mode sequence.

---

## Flow Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                          TOTAL WINDOW                               │
│   FROM_MS ──────────────── HISTORY_END_MS ─────────────── UNTIL_MS │
│   └────── HISTORIC (3h) ──┘└───────── LIVE (3h) ──────────┘        │
└─────────────────────────────────────────────────────────────────────┘

Step 0  FETCH
  └── Download all OBD + GPS packets from API
  └── Sort oldest → newest → write to JSONL

Step 1  SPLIT
  └── historic packets  (t < HISTORY_END_MS)  → 3 batches
  └── live packets      (t ≥ HISTORY_END_MS)  → GPS array + OBD array

Step 2  CREATE SQLite BATCH FILES
  └── historic_batch_1_<IMEI>.db   (time slice 1)
  └── historic_batch_2_<IMEI>.db   (time slice 2)
  └── historic_batch_3_<IMEI>.db   (time slice 3)
  └── live_obd_<IMEI>.db           (empty, filled during Phase 1)

Step 3  MQTT CONNECT
  └── TLS with client certs (eu_region_client_certs/)
  └── clientId = TARGET_IMEI

╔══════════════════════════════════════════════════════════════════════╗
║  PHASE 1 — three streams run concurrently via Promise.all            ║
╟──────────────────────────────────────────────────────────────────────╢
║  Stream A  HISTORIC UPLOAD                                           ║
║    └── POST batch 1 → POST batch 2 → POST batch 3                   ║
║    └── 5 000ms delay between uploads (configurable)                  ║
║    └── Headers: imei, session-token, seq-id=1, is-file-encrypted     ║
║    └── Fires phase1Signal when all batches are uploaded              ║
║                                                                      ║
║  Stream B  GPS L1 MQTT  (runs until phase1Signal)                   ║
║    └── Every 10s: pick next live GPS packet → add l:"1", remove file ║
║    └── Publish JSON to MQTT topic:  {TARGET_IMEI}/gps                ║
║    └── Cycles through available live GPS packets                     ║
║                                                                      ║
║  Stream C  OBD ACCUMULATION  (runs until phase1Signal)              ║
║    └── Every 2min: insert next live OBD packet into live_obd DB      ║
║    └── Encrypted if SEND_ENCRYPTED=true                              ║
╚══════════════════════════════════════════════════════════════════════╝

╔══════════════════════════════════════════════════════════════════════╗
║  PHASE 2 — sequential                                                ║
╟──────────────────────────────────────────────────────────────────────╢
║  1. Upload live_obd_<IMEI>.db via HTTP API                           ║
║  2. Normal-mode streaming (every 1min):                              ║
║     └── GPS packets → MQTT  {TARGET_IMEI}/gps   (no l:"1")          ║
║     └── OBD packets → MQTT  {TARGET_IMEI}/obd                       ║
║     └── Continues until all live packets published                   ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## Parameters

| CLI flag | Env var | Default | Description |
|---|---|---|---|
| `--imei` | `IMEI` | `869305070942563` | Source IMEI — used to query the API for telemetry data |
| `--target-imei` | `TARGET_IMEI` | `869305077523101` | Simulated device IMEI — MQTT clientId and upload `imei` header |
| `--from` | `FROM_MS` | `1789756200000` | Window start (ms epoch). Aligns with pipeline.js default |
| `--until` | `UNTIL_MS` | `1789842600000` | Window end (ms epoch). Aligns with pipeline.js default |
| `--history-end` | `HISTORY_END_MS` | `FROM_MS + 3h` | Split point between historic and live data |
| `--batch-count` | `BATCH_COUNT` | `3` | Number of historic SQLite files to create |
| `--batch-delay` | `BATCH_UPLOAD_DELAY_MS` | `5000` | ms to wait between consecutive file uploads |
| `--gps-l1-interval` | `GPS_L1_INTERVAL_MS` | `10000` | Phase 1: GPS L1 MQTT publish interval (ms) |
| `--obd-accum-interval` | `OBD_ACCUM_INTERVAL_MS` | `120000` | Phase 1: OBD accumulation interval (ms) |
| `--normal-interval` | `NORMAL_INTERVAL_MS` | `60000` | Phase 2: normal-mode MQTT publish interval (ms) |
| `--send-encrypted` | `SEND_ENCRYPTED` | `true` | Encrypt SQLite rows. Pass `false` for plain JSON |
| `--public-key` | `PUBLIC_KEY_PATH` | **embedded** | RSA-2048 public key PEM. Override to use a different key file. |
| `--aes-version` | `AES_VERSION` | `2` | AES version: `1` = 128-bit, `2` = 256-bit |
| `--upload-url` | `UPLOAD_URL` | `https://device-history-server.../upload-2` | HTTP endpoint for SQLite file upload |
| `--user-token` | `USER_TOKEN` | *(required)* | `intangles-user-token` header for upload API |
| `--session-token` | `SESSION_TOKEN` | auto UUID | `session-token` header for upload API (same across all batches in one run) |
| `--mqtt-host` | `MQTT_HOST` | `mqttsecure.intangles-aws-eu-north-1.eu.intangles.com` | MQTT broker hostname |
| `--mqtt-port` | `MQTT_PORT` | `1884` | MQTT broker port |
| `--mqtt-cert-dir` | `MQTT_CERT_DIR` | **embedded** | Override to load TLS certs from a directory instead of using embedded certs. |
| `--token` | `API_TOKEN` | *(built-in)* | Intangles API auth token |
| `--psize` | `PSIZE` | `1000` | API page size |
| `--delay` | `DELAY_MS` | `300` | ms between API page fetches |
| `--out-dir` | `OUT_DIR` | `./output` | Output directory for all generated files |

---

## Output Files

```
output/
├── data_<IMEI>_<FROM>_<UNTIL>.jsonl          ← fetched + sorted packets (shared with pipeline.js)
├── sim_state_<TARGET_IMEI>_<FROM>_<UNTIL>.json  ← resume state
├── historic_batch_1_<TARGET_IMEI>.db         ← time slice 1
├── historic_batch_2_<TARGET_IMEI>.db         ← time slice 2
├── historic_batch_3_<TARGET_IMEI>.db         ← time slice 3
└── live_obd_<TARGET_IMEI>.db                 ← OBD accumulated during Phase 1
```

### SQLite schema (all batch files)

```sql
CREATE TABLE oData    (DNO INTEGER PRIMARY KEY AUTOINCREMENT, DATASTRING BLOB|CHAR(3000) NOT NULL);
CREATE TABLE nSetting (NO INT, DRATE INT, LSPB REAL, ... URL CHAR(400), PORT INT);
CREATE TABLE newAPN   (NO INT, SSELECT INT, APN1 CHAR(50), APN2 CHAR(50));
```

`nSetting` and `newAPN` are copied from `AppData.db` (device config reference).

---

## HTTP Upload API

```
POST https://device-history-server.intangles-aws-eu-north-1.eu.intangles.com/upload-2
Content-Type: multipart/form-data

Headers:
  intangles-user-token: <USER_TOKEN>
  imei:                 <TARGET_IMEI>
  session-token:        <SESSION_TOKEN>    ← same value for all files in one run
  seq-id:               1                  ← always 1
  is-file-encrypted:    true | false

Body:
  file: <SQLite binary>
```

---

## MQTT Topics

| Direction | Topic | Packet format | When |
|---|---|---|---|
| Publish | `{TARGET_IMEI}/gps` | JSON string + `l:"1"` | Phase 1 Stream B (GPS L1) |
| Publish | `{TARGET_IMEI}/gps` | JSON string (no enrichment) | Phase 2 normal mode |
| Publish | `{TARGET_IMEI}/obd` | JSON string | Phase 2 normal mode |

---

## Resume Behaviour

The state file tracks completion of each phase. Restarting with the same `TARGET_IMEI + FROM_MS + UNTIL_MS` resumes from where it stopped:

| State | On restart |
|---|---|
| `fetchComplete: false` | Re-fetches from API |
| `fetchComplete: true` | Reuses existing JSONL |
| `historicBatchesUploaded: N` | Resumes uploads from batch N+1 |
| `phase1Complete: true` | Skips Phase 1 entirely |
| `normalModeIndex: N` | Resumes normal-mode publish from packet N |
| `phase2Complete: true` | Nothing to do — exits immediately |

To restart from scratch: delete `output/sim_state_<TARGET_IMEI>_*.json` (and optionally the batch DB files).

---

## Usage Examples

```bash
# Minimal — uses all built-in defaults
node simulate-device.js --user-token wQBvJm7pexD7YE0XUB0wGHzFpTPNrMKNkrKuTWcy_oN7GTfXxXfvJgmae9zlle6T

# Unencrypted SQLite files (for debugging)
node simulate-device.js --user-token <token> --send-encrypted false

# Custom time window (6-hour window, split at 3h)
node simulate-device.js \
  --user-token <token> \
  --from 1789756200000 \
  --until 1789778200000 \
  --history-end 1789767200000

# Override target device (different IMEI for simulation)
node simulate-device.js --user-token <token> --target-imei 860657057024578

# Speed up for testing (fast intervals)
node simulate-device.js \
  --user-token <token> \
  --batch-delay 1000 \
  --gps-l1-interval 2000 \
  --obd-accum-interval 5000 \
  --normal-interval 3000

# npm shortcut
npm run simulate -- --user-token <token>
```

---

## Related Scripts

| Script | Purpose |
|---|---|
| `fetch-and-store.js` | Fetch API data → write encrypted + unencrypted SQLite (single run) |
| `pipeline.js` | Fetch API data → publish to RabbitMQ (bypasses MQTT) |
| `inspect-db.js` | Read a SQLite DB and print packet types |
| `compare-dbs.js` | Decrypt and compare two SQLite DBs row-by-row |
