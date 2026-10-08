# Device Simulator — Simulation Timeline

> Full sequence of phases, concurrent streams, handoff points, files produced, and resume behaviour.

---

## High-level flow

```
Fetch → Split → Batch Create → MQTT Connect → Phase 1 → Phase 2 → Done
```

---

## Detailed timeline

### Pre-Phase: Fetch

| What | Detail |
|------|--------|
| API calls | `GET /logsV2/<srcImei>?psize=N` — paginated until empty page |
| Output | `allPackets[]` — OBD + GPS sorted by timestamp |
| Events | `fetch:start` → `fetch:page` × N → `fetch:done` |
| Skip | If `SkipFetch=on` **and** `Phase1Complete=true`: loads `live_pkts_<imei>.json` cache instead |

---

### Pre-Phase: Split & Batch Create

| What | Detail |
|------|--------|
| Historic | `t < historyEnd` → `historicRows[]` → `historic_batch_1…N_<tgtImei>.db` |
| Live GPS | `t ≥ historyEnd`, GPS type → `liveGpsPackets[]` (used by GPS L1 stream) |
| Live OBD | `t ≥ historyEnd`, OBD type → `liveObdPackets[]` (used by OBD accumulation) |
| Live all | `livePkts[]` — GPS + OBD interleaved in original order (used by Normal mode) |
| Cache | `live_pkts_<tgtImei>.json` saved after batch creation (SkipFetch cache) |
| Events | `split:done` → `batch:creating` → `batch:done` |

---

### Pre-Phase: MQTT Connect

| What | Detail |
|------|--------|
| Tries | Primary broker first; falls back to secondary on failure |
| Dry run | Uses no-op client — no real connection |
| Events | `mqtt:connect` → `mqtt:connected` |

---

### Phase 1 — Three concurrent goroutines

```
┌─────────────────────┬──────────────────────────┬────────────────────────┐
│  STREAM A           │  STREAM B  (GPS L1)       │  STREAM C  (OBD Accum) │
│  Batch Upload       │  MQTT Publish             │  DB Accumulation       │
├─────────────────────┼──────────────────────────┼────────────────────────┤
│                     │                          │                        │
│  historic_batch_1   │  liveGpsPackets[i%len]   │  OBD pkt → DB row      │
│  ── batchDelay ──   │  every gpsL1IntervalMs   │  (appends, resumable)  │
│  historic_batch_2   │  (cycles infinitely)     │                        │
│  ── batchDelay ──   │                          │  live_obd_<imei>.db    │
│  ...                │  ▲                       │  grows throughout P1   │
│  historic_batch_N   │  │ DOES NOT STOP         │                        │
│                     │  │ at Phase 1 end —       │  ► Stops when          │
│  ► saveRunState     │  │ continues into P2!     │    Stream A finishes   │
│    after each batch │  │                        │                        │
│  ► Phase1Complete   │  │                        │                        │
│    = true on finish │  │                        │                        │
└─────────────────────┴──┼───────────────────────┴────────────────────────┘
                         │
              GPS L1 still running ───────────────────────────────────────►
```

**Resume**: If crashed during Phase 1, next run skips batches `i < batchesUploaded` and resumes OBD accumulation from `COUNT(*)` in the existing DB.

---

### Phase 2 — OBD Upload (GPS L1 still live)

```
                    ┌──────────────────────────┐
GPS L1 running ─────┤  Upload live_obd_<imei>  ├────────────────────────►
                    │  .db to API              │
                    │                          │
                    │  ✗ FAIL?                 │
                    │  └─ wait 30s, retry      │  GPS L1 continues
                    │     wait 30s, retry      │  through every retry
                    │     ...                  │
                    │                          │
                    │  ✓ SUCCESS               │
                    │  Phase2OBDUploaded=true   │
                    │  saveRunState            │
                    └──────────┬───────────────┘
                               │
                         closeGpsL1()
                               │
                    ┌──────────▼───────────────┐
                    │  GPS L1 goroutine drains  │
                    │  emits "stream complete"  │
                    └──────────┬───────────────┘
                               │ GPS L1 fully stopped
                               ▼
```

---

### Phase 2 — Normal Mode Streaming

```
livePkts[] traversed in original order (GPS + OBD interleaved)

  for each packet:
    if GPS → wait normalGpsIntervalMs → publish → <tgtImei>/obd
    if OBD → wait normalObdIntervalMs → publish → <tgtImei>/obd
    saveRunState(LiveStreamOffset = i)   ← crash-safe checkpoint
```

**Resume**: After crash, normal mode restarts from `LiveStreamOffset` — skips already-published packets.

---

## Files on disk

| File | Created | Purpose |
|------|---------|---------|
| `historic_batch_1…N_<tgtImei>.db` | Batch Create | Historic packets for upload |
| `live_obd_<tgtImei>.db` | Phase 1 Stream C | OBD packets accumulated for Phase 2 upload |
| `live_pkts_<tgtImei>.json` | After batch:done | SkipFetch cache (reuse on next run) |
| `sim_state_<tgtImei>.json` | Updated continuously | Resume state: batches uploaded, OBD count, phase flags, stream offset |
| `sim_logs_<runId>.json` | When sim ends/stops | Full event log with payloads — viewable in History page |

---

## Resume state (`sim_state_<tgtImei>.json`)

```json
{
  "batchesUploaded": 12,
  "totalBatches": 26,
  "totalOBDPackets": 340,
  "phase1Complete": true,
  "phase2OBDUploaded": false,
  "liveStreamOffset": 110
}
```

| Flag | Guards |
|------|--------|
| `batchesUploaded` | Skip already-uploaded batches in Phase 1 |
| `phase1Complete` | Skip entire Phase 1 on resume; required for SkipFetch |
| `phase2OBDUploaded` | Skip OBD DB upload on resume; prevents duplicate upload |
| `liveStreamOffset` | Resume Normal mode from exact packet index |

---

## Dry run differences

| Step | Dry run behaviour |
|------|------------------|
| Fetch | Real API call (same as live) |
| Batch create | Files written to disk (same as live) |
| MQTT | No-op client — no real connection |
| Phase 1 Stream A | Logs `DRY/UPLOAD` skip per batch, 200 ms simulated delay |
| Phase 1 Stream B | Logs `P1/GPS` per packet with payload, no real publish |
| Phase 1 Stream C | Logs `DRY/OBD` per row, no DB insert |
| Phase 2 OBD upload | Logs `DRY/UPLOAD` skip, `phase2OBDUploaded` NOT persisted |
| Normal mode | Logs `P2/GPS` / `P2/OBD` per packet with payload, no real publish |

---

## Other simulation modes

| Mode | What it does |
|------|-------------|
| `simulate` | Full pipeline described above |
| `fetch` | Fetch + batch create only; shows download card for data export |
| `mqtt-pub` | Fetch then publish all packets directly via MQTT, no batch files |
