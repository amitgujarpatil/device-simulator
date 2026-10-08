# Tracing a mock IMEI through the EU pipeline (Dash0)

Dataset: **`prod-eu`**. Span/log attribute for the device: **`imei`** (plain key, not `device.imei`).
Use the Dash0 MCP tools directly (`getSpans`, `sql`, `getTraceDetails`, `getLogRecords`) — do not use Agent0 / `runTask`.

D0QL quirks: span name column is `name` (not `span_name`), log severity column is `severity`, map access is
`span_attributes['imei']` / `resource_attributes['service.name']`, `INTERVAL`, `groupUniqArray` and `toUnixTimestamp`
are unsupported (bucket with `floor(toInt64(timestamp) / 600) * 600`), and every SELECT is capped at 20 rows.

## 1. Did the IMEI reach each stage?

```sql
SELECT resource_attributes['service.name'] AS service, name, span_attributes['topic'] AS topic, status_code,
       count() AS spans, uniq(trace_id) AS traces, min(timestamp) AS first_seen, max(timestamp) AS last_seen
FROM spans WHERE span_attributes['imei'] = '<mock imei>'
GROUP BY service, name, topic, status_code ORDER BY service, topic
```

Live path (alerts worked): `parser` → `publish-parsed` → `alert-checker`, `geofence-checker`, `data-watcher-consumer`,
`dtc-checker`, `odometer-consumer`.
History path (batch-mode session on the IMEI, see `Parser/parser.js` `isVehicleInBatchMode`): `history-pre-parser` →
`alert-checker-history`, `geofence-checker-history`, `trip-decider-history`, `dtc-checker-history`,
`odometer-consumer-history`, `history-consolidator`. Live `publish-parsed` / `alert-checker` show **0** spans.

## 2. History consumers stalling mid-replay

```sql
SELECT span_attributes['imei'] AS imei, resource_attributes['service.name'] AS service, count() AS spans,
       min(timestamp) AS first_seen, max(timestamp) AS last_seen
FROM spans WHERE span_attributes['imei'] IN ('<mock imei 1>', '<mock imei 2>')
  AND resource_attributes['service.name'] IN ('alert-checker-history','geofence-checker-history','history-pre-parser',
      'trip-decider-history','dtc-checker-history','odometer-consumer-history','history-consolidator')
GROUP BY imei, service ORDER BY imei, service
```

Finding (2026-10-04, MK74ULC): geofence/odometer history consumers ran for the whole 14-minute replay, but
`alert-checker-history` / `dtc-checker-history` stopped ~90 s in and `trip-decider-history` ran for one second —
only geofence + data_loss alerts appeared.

## 3. Errors and their cause

```sql
SELECT resource_attributes['service.name'] AS service, severity, body, count() AS n, min(timestamp), max(timestamp)
FROM logs WHERE log_attributes['imei'] = '<mock imei>' GROUP BY service, severity, body ORDER BY n DESC
```

Then `getTraceDetails(traceId, spanId)` on an ERROR span gives the exception (e.g. parser
`connect ECONNREFUSED 172.20.249.240:80` at the start of the first replay).
`Message discarded: duplicate` warnings from the parser/history-pre-parser are replay re-sends — ignore them.

## Notes
- Traces are **sampled** (tracestate `th:e148`, roughly 1 in 8 kept), so span counts are not message counts.
- EU telemetry shows only RabbitMQ metrics and no Kafka spans; Kafka consumer lag for the history topics has to come
  from Grafana/Kafka tooling.
- EU Jaeger (`jaeger-telemetry.intangles-aws-eu-north-1.eu.intangles.com`) answers untagged searches, but searching by
  `imei` tag fails with an Elasticsearch `all shards failed` error. The EU VMUI URL returned 404 on every path.
