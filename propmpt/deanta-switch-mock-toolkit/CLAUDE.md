# CLAUDE.md — operating context for this toolkit

You are in the **switch mock-account toolkit**. It clones an EU customer account into a POC mock account, creates
mock devices/vehicles that mirror real ones, plans replay windows so real traffic can be replayed onto the mock
IMEIs, and debugs which alerts come through. `README.md` is the human guide (setup, run order, gotchas table) — read it
before running anything.

## Mental model

```
 REAL vehicle (Deanta, read-only)          MOCK vehicle (intangles-switch-test-deanta)
 imei 866308064390602  plate AY74ULE   ->  imei 866308060277944  plate MK74ULE   (same spec/protocol/type/tags/group)
 raw data for a time window            ->  replayed by the team's service as the mock imei -> parser -> alerts
```

The mock must match its source in everything except account, IMEI, plate and ids. It is a fidelity exercise.

## Current state (2026-10-05)

- Source `1482046680973967360` (Deanta) → mock `1606685851746566144` (`intangles-switch-test-deanta`, stage poc).
- 29 mock pairs in `data/deanta-mock-seeded.json` (source/mock IMEI, plate, vid, dev id, spec, source groups).
- `data/*replay-windows*.jsonc` are the agreed replay plans (24 h max per window).
- All mock vehicles: `tracker_attach_time = 1767225600000` (ms).
- Open issue: mocks whose data goes through the **history pipeline** only get geofence/data_loss alerts because
  `alert-checker-history` and `trip-decider-history` stop ~90 s into a replay. Not a vehicle-config problem.

## Rules

1. **Everything is EU production.** Reads of source accounts are fine; never write to a source account. Writes go
   only to the mock account, and only when the user asks.
2. **Dry-run first, then one, then the rest.** Scripts that write default to dry run (`APPLY=1` / no `DRY_RUN`), and
   `03-seed-vehicles.js` takes `LIMIT=1`. There is **no delete route** for accounts, devices or vehicles.
3. **Verify by read-back, not by response code.** `baseinfoV2` returns 200 for fields it silently drops; attach can
   return `CROSSSLOT` and still commit. Read with `GET /idevice/<imei>/allinfoV2`.
4. **Token from the environment only** (`INTANGLES_TOKEN`). Never write it into a file, script, memory or commit. If the
   user pastes credentials, use them for the task and suggest rotating them afterwards.
5. **No alert recipients on the mock.** Copy thresholds only; never copy `users` lists from the source alert config.
6. **Parity means matching the source**, not maximising features (e.g. `obd_attached:false` stays false).
7. **`tracker_attach_time` is milliseconds.** No HTTP route can set it; use `fix/setMockTrackerAttachTime.js` inside an
   intangles `Backend/` pod (SDK write + cache clear) or the `fix/data_apis/` variant inside a data_apis pod.
8. **Customer data stays internal.** `data/` holds real IMEIs/plates; `data/source-dump/` (from `collect-account.sh`)
   holds customer users/emails — don't publish or bundle it.

## Debugging playbook (in order)

1. "Mock has no/fewer alerts": run `alerts/alerts-compare.js` for the replayed windows — are only some types missing?
2. Compare config: `alerts/compare-mocks.js` with a working mock, the failing mock and its source.
3. Check routing in Dash0 (`debug/DASH0-QUERIES.md`, dataset `prod-eu`, attribute `imei`): live path
   (`publish-parsed` → `alert-checker`) vs history path (`history-pre-parser` → `*-history`). Use the Dash0 tools
   directly; do not delegate to Agent0.
4. Packets missing entirely: compare packet time with `vehicle.tracker_attach_time` (parser drops older packets).
5. Device not visible in Device Management: `manufacturing_batch` is null (cosmetic only).

## Key code references

- Parser drop-by-attach-time: `intangles/Backend/Parser/parser.js:1110`, `:1186`; history routing `:764` (`isVehicleInBatchMode`).
- Attach stamps now: `data_apis/stores/vehicleStore.js:4724`.
- baseinfoV2 route allowlist: `data_apis/routes/vehicleRoutes.js:5850-5912`; store accepts any field: `stores/vehicleStore.js:3702`.
- Device Management filter: `data_apis/stores/ideviceStore.js:1815` (`valid_intangles_device` → `manufacturing_batch` not null).
- AC alert uses attach time as registration date (Mahindra only): `intangles/Backend/Notifications/alertChecks/air_conditioner/config.js:37`.
