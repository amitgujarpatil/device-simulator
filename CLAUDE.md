# Device Simulator — Agent Knowledge Base

> Read this before touching any file. It captures every architectural decision,
> the full feature set, and how every piece connects.

---

## What this app is

A **macOS desktop app** (Wails v2 + Go backend + plain HTML/JS frontend) that
simulates IoT device telemetry. It mimics a real field device by:

1. Fetching historical GPS + OBD packets from a remote API (source IMEI)
2. Splitting them into historic and live sets
3. Uploading historic batches to the API (target IMEI)
4. Streaming live GPS/OBD via MQTT

Three simulation **modes**:
| Mode | What it does |
|------|-------------|
| `simulate` | Full pipeline: fetch → batch → Phase 1 (upload + GPS stream + OBD accum) → Phase 2 (OBD upload + live stream) |
| `mqtt-pub` | Fetch then publish all packets directly via MQTT, no batch files |
| `fetch` | Fetch + create batch files only — exposes a download card for data export |

---

## Project layout

```
/Volumes/personal space/poc/desktop/
├── main.go                    # Wails entry — window config (TitleBarHiddenInset)
├── app.go                     # ALL Go↔JS bridge methods (App struct)
├── mqtt_client.go             # mqttService — standalone MQTT test/publish client
├── wails.json                 # App metadata (company: Intangles, product: Device Simulator)
├── build/
│   ├── appicon.png            # 1024×1024 source icon (new logo, for Wails builds)
│   └── darwin/
│       └── AppIcon.icns       # Pre-built 1.3 MB full iconset (all sizes 16→1024)
├── frontend/
│   ├── sim-state.js           # Shared localStorage state (SimState) — single source of truth
│   ├── sim-nav.js             # Side-nav injected into every page + shared CSS + region picker
│   ├── sim-run.html           # Main simulation control page (most complex)
│   ├── sim-logs.html          # Data logs viewer
│   ├── sim-mqtt.html          # Standalone MQTT client UI
│   ├── sim-history.html       # Run history with re-run / resume
│   ├── sim-metrics.html       # System metrics page (CPU/RAM/app footprint + comparison)
│   ├── sim-settings.html      # Global settings
│   ├── sim-theme.html         # Theme selector
│   ├── api-client.html        # Postman-like HTTP client
│   └── utilities.html         # Utility helpers page
└── pkg/
    ├── simulator/
    │   ├── simulator.go       # Orchestrator: runSimulation, runPhase1, runPhase2, runMQTTPubDirect
    │   ├── config.go          # Config, RegionConfig, Packet, SimEvent structs
    │   ├── fetch.go           # fetchTelemetry, identifyPacketType, convertToL1Packet
    │   ├── batch.go           # initDB, createBatchFile, createOBDAccumDB, insertOBDRow
    │   ├── mqtt.go            # connectWithFallback, publishMQTT, TestMQTTConnection
    │   ├── upload.go          # uploadFile (HTTP multipart to API)
    │   └── embed.go           # Embedded RSA public key
    ├── apiclient/
    │   ├── db.go              # SQLite-backed workspace/collection/request/history/env store
    │   ├── http.go            # Execute() — HTTP request runner for the API client
    │   └── export.go          # Collection import/export
    ├── crypto/
    │   └── encrypt.go         # EncryptPacket (RSA-wrapped AES for batch files)
    └── utilities/
        └── utilities.go       # UtilHash, UtilHMAC, UtilParseCert, UtilYAMLToJSON
```

---

## Go bridge (`app.go`) — complete method list

Every public method on `App` is callable from JS as `window.go.main.App.<Method>()`.

### Simulation
| Method | Purpose |
|--------|---------|
| `StartSimulation(cfg Config) error` | Kicks off goroutine, emits `simulation:event` via Wails EventsEmit |
| `StopSimulation()` | Cancels context |
| `PauseSimulation()` | Blocks goroutine at next checkPause() |
| `ResumeSimulation()` | Unblocks |
| `UpdateMQTTPubIntervals(gpsMs, obdMs int)` | Hot-updates MQTT Direct intervals while running |
| `TestMQTT(cfg RegionConfig) map` | One-shot MQTT connection test |

### File / state
| Method | Purpose |
|--------|---------|
| `GetDefaultOutputDir() string` | Returns `~/Documents/sim_output` |
| `SelectOutputDir() string` | Native directory picker dialog |
| `SaveFile(content, defaultName string) error` | Native save dialog + writes file + removes quarantine |
| `ClearTestState(outputDir, tgtImei string) error` | Deletes `historic_batch_*`, `live_obd_*`, `sim_state_*.json` |
| `GetResumeState(outputDir, tgtImei string) map` | Reads disk: batch file count, uploaded count, OBD rows, phase1Complete |
| `ReadTextFile(path string) string` | Generic file read |

### Export (fetch mode)
| Method | Purpose |
|--------|---------|
| `ExportFetchedData(outputDir, tgtImei, kind, format string) string` | Primary: reads in-memory `sim.fetchedPackets` (always unencrypted). Fallback: reads batch SQLite files (only works if encryption off). `kind` = all/gps/obd. `format` = json/jsonl/csv |

### System metrics
| Method | Purpose |
|--------|---------|
| `GetSystemMetrics() map` | Returns appRSSBytes, appCPUPct, appHeapAlloc, appSys, goroutines, sysTotalBytes, sysUsedBytes, sysFreeBytes, loadAvg1, loadAvg5, cpuCores |

### API Client (AC*)
Full Postman-like HTTP client backed by `pkg/apiclient` SQLite DB.
`APIClientInit(dir)` → `ACListWorkspaces/ACSaveWorkspace/ACDeleteWorkspace` →
`ACListCollections/ACSaveCollection/ACDeleteCollection` →
`ACListRequests/ACSaveRequest/ACDeleteRequest` →
`ACListHistory/ACSaveHistory/ACClearHistory` →
`ACListEnvironments/ACSaveEnvironment/ACSetActiveEnvironment/ACDeleteEnvironment` →
`ACListEnvVariables/ACSaveEnvVariables` →
`ACPickFile()` → `ACExportCollection/ACImportCollection` →
`APIClientSendRequest(payload) SendResponse`

### MQTT Client (standalone)
`MQTTClientConnect`, `MQTTClientDisconnect`, `MQTTClientPublish`,
`MQTTClientSubscribe`, `MQTTClientUnsubscribe`, `MQTTClientStatus`

---

## Simulator internals (`pkg/simulator/simulator.go`)

### Struct
```go
type Simulator struct {
    ctx, cancel             // context lifecycle
    mu, paused              // pause gate
    startT                  // wall clock for elapsed calcs
    liveMu, liveGpsMs, liveObdMs  // hot-updatable MQTT Direct intervals
    fetchMu, fetchedPackets []Packet  // cached raw packets after fetch-only run
}
```

### Key methods
- `StoreFetchedPackets([]Packet)` / `GetFetchedPackets() []Packet` — used by `ExportFetchedData`
- `checkPause(ctx)` — blocks on `mu` when paused; returns error on cancel

### `runSimulation` flow
```
fetchTelemetry()
  └─ paginates API until empty page, sorts by timestamp

split into historicRows / liveGpsPackets / liveObdPackets / livePkts

if mode == "mqtt-pub" → runMQTTPubDirect() [exit]

createBatchFile() × N   (historic_batch_N_<tgtImei>.db)
  └─ oData table: DNO (PK autoincrement), DATASTRING (JSON or encrypted BLOB)

if mode == "fetch" → StoreFetchedPackets(allPackets) [exit]

loadRunState() from sim_state_<tgtImei>.json
if phase1Complete → skip Phase 1

runPhase1() — 3 concurrent goroutines:
  Stream A: upload batch files (skip if i < runState.BatchesUploaded)
            saveRunState() after each batch
  Stream B: publish live GPS via MQTT (cycles until A done)
  Stream C: accumulate OBD into live_obd_<tgtImei>.db
            resumes from COUNT(*) if DB exists

runPhase2():
  Upload live_obd_<tgtImei>.db
  Stream all live packets via MQTT (60s interval)
```

### Resume state file
`sim_state_<tgtImei>.json`:
```json
{ "batchesUploaded": 12, "totalBatches": 26, "totalOBDPackets": 340, "phase1Complete": true }
```

---

## Frontend architecture

### `sim-state.js` — `window.SimState`
Single localStorage key `sim_ds_state` holds the entire app state:
```js
{
  tests: [],           // saved test configs
  runs: [],            // run history
  regions: {...},      // per-region MQTT/API credentials
  activeTestId: N,
  activeRegionKey: "eu-north",
  global: {            // global settings
    outputDir, apiPageSize, apiRequestDelay,
    gpsL1IntervalMs, obdAccumIntervalMs,
    _simRunning, _simRunId   // ← tab-survival flags
  }
}
```
Key methods: `getActiveTest()`, `setActiveTest(id)`, `getRegion(key)`,
`getRegions()`, `setActiveRegionKey(key)`, `addRun()`, `updateRun()`,
`getRuns()`, `setGlobal({...})`, `getGlobal()`, `applyTheme()`

### `sim-nav.js` — `window.SimNav`
Injected into every page's `<body>` first thing. Provides:
- 44px side-nav with 36px traffic-light pad at top (Wails TitleBarHiddenInset clearance)
- Nav icons: ▶ Run · ≡ Logs · ⇄ MQTT · ⚡ API · ⧖ History · ⊹ Metrics · ⚙ Settings · ◈ Theme
- Region picker (dot + short label at bottom of nav, click opens popover menu)
- Shared CSS variables injected via `<style>` tag (dark theme: `--bg0` through `--border`, `--blue/green/yellow/red/purple/orange`)
- Window draggability: `--wails-draggable: drag` on `.hdr`, `.hist-header`, `.page-hdr`, `.ac-hdr`, `.ctrl-bar`, `.conn-status`; `no-drag` on all interactive elements

### `sim-run.html` — simulation control page

**Layout**: side-nav (44px) → left sidebar (238px tests+config+resume) → center pipe → right log panel (320px)

**Simulation lifecycle**:
```
startSim()
  _resetUI()          ← clears pipeline steps/metrics (NOT resume state)
  loadResumeState()   ← shows disk state immediately
  SimState.setGlobal({_simRunning:true, _simRunId})
  EventsOn('simulation:event', handleSimStep)
  App.StartSimulation(buildSimConfig())

stopSim()
  SimState.setGlobal({_simRunning:false, _simRunId:null})
  EventsOff → StopSimulation()
  setTimeout(loadResumeState, 800)

loadFromState()  [on DOMContentLoaded]
  if SimState.getGlobal()._simRunning → reconnectSim()
```

**Tab survival** (`reconnectSim`): when user navigates away mid-run and comes back,
`_simRunning` flag in localStorage triggers reconnect — re-attaches `EventsOn`,
restores button states, adds "Reconnected" log entry.

**Resume state sidebar** shows live disk values:
`rsFetch`, `rsPkts`, `rsBatches` (uploaded/total), `rsObd` (rows/total), `rsP1`, `rsP2`
Populated by `loadResumeState()` → `App.GetResumeState()`.

**Fetch download card** (fetch mode only, appears after `batch:done`):
- Filter: ALL / GPS / OBD
- Format: JSON / JSONL / CSV
- Calls `App.ExportFetchedData()` → `App.SaveFile()` with native dialog
- Reads from in-memory `sim.fetchedPackets` (encryption-safe)

**`handleSimStep(ev)`** maps `ev.step` strings to UI updates:
`fetch:start/page/done` → `split:done` → `batch:creating/done` →
`mqtt:connect/connected` → `mqttpub:start/packet` →
`phase1:start` → `p1:upload/gps/obd` → `phase1:done` →
`phase2:start` → `p2:upload:done/stream` → `phase2:done` → `done`

### `sim-metrics.html` — system metrics
Live 3-second polling via `App.GetSystemMetrics()`:
- Memory arc gauge (used/total)
- CPU load avg arc gauge  
- App process card: RSS, CPU%, Go heap, goroutines + "✓ Lightweight" badge if RSS < 100MB
- Footprint comparison table vs Electron (250-500MB), Java (150-350MB), Node (60-150MB)

---

## Wails v2 specifics

- **Window**: `mac.TitleBarHiddenInset()` — traffic lights visible, content starts at y=0. Side-nav has 36px `sn-trafficpad` div at top for clearance.
- **Drag**: use `--wails-draggable: drag` (NOT `-webkit-app-region`). Applied to header bars via CSS in `sim-nav.js`.
- **Events**: `runtime.EventsEmit(ctx, "simulation:event", ev)` in Go → `window.runtime.EventsOn(...)` in JS. `_evListenerOff` holds the cleanup function.
- **Bridge**: `window.go.main.App.<Method>()` returns a Promise. All bridge calls are async.
- **Build**: `~/go/bin/wails build` from project root. Outputs to `build/bin/device-simulator.app`. After build, replace `Contents/Resources/iconfile.icns` with `build/darwin/AppIcon.icns` (Wails regenerates a thin one from appicon.png).

---

## Icon / branding

- Source: `frontend/ChatGPT Image Sep 27, 2026, 10_06_05 PM.png` (1254×1254 PNG, hexagon logo with cyan accent)
- `build/appicon.png` → 1024×1024 PNG (Wails reads this at build time)
- `build/darwin/AppIcon.icns` → 1.3MB pre-built icns with all sizes 16→1024
- After every `wails build`, run: `cp build/darwin/AppIcon.icns build/bin/device-simulator.app/Contents/Resources/iconfile.icns`
- Refresh icon cache: `killall Dock`

---

## Data files produced at runtime

All files land in `outputDir` (default `~/Documents/sim_output/`):

| File | Contents |
|------|---------|
| `historic_batch_N_<tgtImei>.db` | SQLite: `oData(DNO, DATASTRING)`. DATASTRING = JSON packet or encrypted BLOB |
| `live_obd_<tgtImei>.db` | Same schema, OBD packets accumulated during Phase 1 Stream C |
| `sim_state_<tgtImei>.json` | Resume state: batchesUploaded, totalBatches, totalOBDPackets, phase1Complete |
| `apiclient.db` | API Client workspaces/collections/requests/history/envs |

---

## Encryption

`pkg/crypto/encrypt.go` — `EncryptPacket(plaintext []byte, rsaPublicPEM string, aesVersion string) ([]byte, error)`

RSA-wrapped AES encryption. Applied per-packet before SQLite insert when `cfg.EncryptEnabled == true`.
When enabled, DATASTRING column is BLOB — cannot be JSON-parsed. `ExportFetchedData` avoids this by reading from `sim.fetchedPackets` (pre-encryption in-memory cache).

---

## Key CSS variables (dark theme defaults)

```css
--bg0: #09080e   /* deepest background */
--bg1: #100e1a   /* sidebar / card bg  */
--bg2: #181526   /* input / secondary  */
--bg3: #221e33   /* focus / hover      */
--border: #2d2a40
--t0: #f0eaff    /* primary text       */
--t1: #b8acdc    /* secondary text     */
--t2: #8878b8    /* muted text         */
--blue: #a78bfa  /* primary accent     */
--green: #34d399
--yellow: #fbbf24
--red: #f87171
--purple: #818cf8
--orange: #fb923c
```

Theme can be overridden via `SimState.applyTheme()` which reads `SimState.getGlobal().theme`.

---

## Common pitfalls for agents

1. **Don't reset Resume State in `_resetUI()`** — those lines were removed intentionally. `loadResumeState()` is called right after `_resetUI()` in `startSim()`.
2. **`--wails-draggable`** not `-webkit-app-region` for window dragging.
3. **Batch SQLite files may be encrypted** — never assume DATASTRING is parseable JSON; use `sim.fetchedPackets` for export.
4. **`ExportFetchedData` reads memory first** — the `Simulator.fetchedPackets` field is populated only after a fetch-only run in the same app session. Restarting the app clears it; fallback then reads unencrypted SQLite files.
5. **After every `wails build`**, replace `iconfile.icns` manually (Wails regenerates a thin version from the 1024px PNG).
6. **`sim_state_<tgtImei>.json`** is the resume contract — read by both Go (to skip uploaded batches) and JS (via `GetResumeState` for sidebar display).
7. **`_simRunning` in `SimState.global`** is the tab-survival flag — set true on `startSim`, false on `stopSim`, checked on every `loadFromState` call.
