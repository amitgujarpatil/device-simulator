# Device Simulator — API Reference

> Document every API call the app makes, which credential it uses, and how
> cross-region mode changes the routing. Correct wrong endpoints here.

---

## Region Base URLs (current seeds)

| Key | Label | API Base | Upload URL |
|-----|-------|----------|------------|
| `eu-north` | EU North (Ireland) | `https://apis.intangles-aws-eu-north-1.eu.intangles.com` | `https://device-history-server.intangles-aws-eu-north-1.eu.intangles.com/upload` |
| `us-east` | US East (Virginia) | `https://apis.intangles-aws-us-east-1.us.intangles.com` | `https://device-history-server.us.intangles.com/upload` |
| `ap-south` | AP South (Mumbai) | `https://apis.intangles-aws-ap-south-1.ap.intangles.com` | `https://device-history-server.ap.intangles.com/upload` |
| `in-central` | IN Central (Pune) | `https://apis.intangles-aws-in-central-1.in.intangles.com` | `https://device-history-server.in.intangles.com/upload` |

---

## Credentials used per API call

Two different token types exist. They are NOT interchangeable:

| Token | Field in Settings | Used by |
|-------|-------------------|---------|
| `apiToken` | API Token | Fetch telemetry (query param `token=`) |
| `userToken` | User Token | All validate APIs (header `Intangles-User-Token`) |
| `sessionToken` | Session | (stored, currently unused in calls) |
| `accId` | Account ID | Passed as `acc_id=` query param in validate calls; **also auto-resolved from `/idevice/listV3` response** |

---

## 1. Fetch Telemetry (simulate / fetch / mqtt-pub modes)

**File**: `pkg/simulator/fetch.go`

```
GET {srcApiBase}/idevice/logsV2/{srcImei}
    ?psize={pageSize}
    &token={srcApiToken}
    &from={fromMs}
    &until={untilMs}
    [&last_t={lastTimestamp}]   ← pagination cursor, omitted on first page
```

**Auth**: query param `token=`  (uses `apiToken`, NOT `userToken`)

**Cross-region**:
- Base URL → `srcRegion.apiBase` (the **SRC** region, not TGT)
- Token → `srcRegion.apiToken`
- Fallback: if `srcRegion` has no `apiBase`/`apiToken`, falls back to TGT region values

**Pagination**: loops until `last_evaluated_key` is null or unchanged, or a page returns 0 new entries.

**Response shape**:
```json
{
  "logs": [ { "t": 1234567890000, "m": "[{...gps/obd packet...}]" } ],
  "last_evaluated_key": { "t": 1234567890000, ... }
}
```

---

## 2. Batch Upload (simulate mode — Phase 1 Stream A)

**File**: `pkg/simulator/upload.go`

```
POST {tgtRegion.uploadUrl}
Content-Type: multipart/form-data
Body: file field = SQLite .db file (historic_batch_N_<tgtImei>.db)
```

**Auth**: none (URL itself is the credential; TLS client cert in embedded certs)

**Cross-region**: always uses **TGT** region `uploadUrl` regardless of srcRegion.

---

## 3. Validate — Device Lookup

**File**: `pkg/simulator/validate.go` → `lookupDevice()`

```
GET {apiBase}/idevice/listV3
    ?query={imei}
    &psize=5
    &pnum=1
    &status=*
    &showall=true
    &lang=en
```

**Auth**: header `Intangles-User-Token: {userToken}`

**Headers sent** (all validate calls):
```
Intangles-Client: intangles_app
Intangles-User-Token: {userToken}
Intangles-Session-Type: web
Accept: application/json
```

**Cross-region**:
- SRC IMEI lookup → `srcRegion.apiBase` + `srcRegion.userToken`
- TGT IMEI lookup → `tgtRegion.apiBase` + `tgtRegion.userToken`

**Response shape** (relevant fields):
```json
{
  "result": {
    "idevices": [
      { "vid": 12345, "account_id": 678, "imei": "...", "tag": "...", "number_plate": "..." }
    ]
  }
}
```
`vid` and `account_id` come back as **JSON numbers** (float64 in Go), not strings.

---

## 4. Validate — Trips

**File**: `pkg/simulator/validate.go` → `fetchAllTrips()`

```
GET {apiBase}/trip/{vehicleId}/getLastTripsV2
    ?start={fromMs}
    &end={toMs}          ← raw midnight boundary (NOT end-of-day snapped)
    &psize=50
    &pnum={page}
    &duration=600000
    &acc_id={accId}
    &lang=en
```

**Auth**: header `Intangles-User-Token`

**Note**: uses raw `toMs`, NOT `toMsEod`. Pagination: stops when `len(items) < psize`.

---

## 5. Validate — Alerts

**File**: `pkg/simulator/validate.go` → `fetchAllAlerts()`

```
GET {apiBase}/alertlog/logsV2/{fromMs}/{toMs}
    ?psize=100
    &pnum={page}
    &types={allTypes}    ← URL-encoded comma list (see below)
    &vehicles={vehicleId}
    &acc_id={accId}
    &lang=en
    &sort=timestamp%20desc
    &no_total=true
```

**Auth**: header `Intangles-User-Token`

**Note**: uses `toMsEod` (end-of-day snapped, +86399999ms if midnight) to match web UI.

**Alert types queried**:
- Driving: `over_speed, speeding, idling, hard_brake, stoppage, freerun, unscheduled_driving, engine_over_running, geofence, over_acc, continuous_driving, slow_running, panick`
- System: `device_disconnected, device_connected, dtc, fuel_chori, fuel_bhara, def_bhara, def_chori, def_low_level, fuel_low_level`

Pagination: checks `paging.isLastPage` in response; fallback is `len(items) < psize`.

---

## 6. Validate — DTCs

**File**: `pkg/simulator/validate.go` → `fetchAllDtcs()`

Called **twice** per vehicle: once for `status=active`, once for `status=logged`.

```
GET {apiBase}/dtc/vehicle/{vehicleId}/history
    ?pnum={page}
    &psize=100
    &start_time={fromMs}
    &end_time={toMsEod}   ← end-of-day snapped
    &status={active|logged}
    &unique_by_type=false
    &get_count=true
    &sort=timestamp%20desc
    &acc_id={accId}
    &lang=en
```

**Auth**: header `Intangles-User-Token`

**Response shape**: `{ "result": [ {...dtc...} ] }`

---

## Cross-Region Mode Summary

When `test.srcRegionKey !== test.regionKey`:

| What | Base URL | Token |
|------|----------|-------|
| Fetch telemetry (SRC packets) | `srcRegion.apiBase` | `srcRegion.apiToken` |
| Validate: lookup SRC IMEI | `srcRegion.apiBase` | `srcRegion.userToken` |
| Validate: SRC trips/alerts/DTCs | `srcRegion.apiBase` | `srcRegion.userToken` |
| Batch upload | `tgtRegion.uploadUrl` | embedded TLS cert |
| MQTT publish | `tgtRegion.mqttBroker` | embedded client cert |
| Validate: lookup TGT IMEI | `tgtRegion.apiBase` | `tgtRegion.userToken` |
| Validate: TGT trips/alerts/DTCs | `tgtRegion.apiBase` | `tgtRegion.userToken` |

**Same-region mode**: SRC and TGT rows both use `tgtRegion.*`.

---

## Known Issues / Questions

- [ ] Are the EU and IN Central `apiBase` hostnames correct? Current seeds:
  - EU: `apis.intangles-aws-eu-north-1.eu.intangles.com`
  - IN: `apis.intangles-aws-in-central-1.in.intangles.com`
- [ ] `/idevice/listV3` — is this the right endpoint to resolve IMEI → vehicleId? (previously used `listV2`)
- [ ] Trips endpoint `/trip/{vid}/getLastTripsV2` — correct for all regions?
- [ ] Alerts endpoint `/alertlog/logsV2/{from}/{to}` — correct path format?
- [ ] DTCs endpoint `/dtc/vehicle/{vid}/history` — correct for all regions?
- [ ] Upload URL for IN Central: `device-history-server.in.intangles.com` — is `.in.intangles.com` right, or should it match EU's pattern (`device-history-server.intangles-aws-in-central-1.in.intangles.com`)?
