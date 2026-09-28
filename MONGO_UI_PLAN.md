# MongoDB Studio — Lightweight GUI Plan

> Embedded in the existing Wails v2 + Go + plain HTML/JS desktop app.
> Style matches the current dark theme. No Electron, no Node, no extra runtime.

---

## Target scope (Studio 3T → cut to essentials)

| Studio 3T feature | Include? | Notes |
|---|---|---|
| Connection manager (save/auth) | ✅ | Multiple saved connections |
| Database + Collection tree | ✅ | Collapsible, lazy-loaded |
| Document viewer (table + JSON) | ✅ | Toggle between views |
| Find query (filter / sort / project / limit) | ✅ | JSON bar, not visual builder |
| Document CRUD | ✅ | Insert / edit / delete / duplicate |
| Aggregation pipeline | ✅ Phase 3 | Stage-by-stage, result preview |
| Index viewer + create/drop | ✅ Phase 4 | Simple list UI |
| Import / Export (JSON, JSONL, CSV) | ✅ Phase 4 | Reuses SaveFile bridge |
| Schema analyzer | ✅ Phase 4 | Field frequency table only |
| Query history | ✅ Phase 3 | Last 50 per collection |
| Visual query builder | ❌ | Adds complexity, low ROI |
| SQL → MQL translator | ❌ | Out of scope |
| Diff / compare | ❌ | Out of scope |

---

## Layout (matches app style)

```
[44px side-nav] [200px tree panel] [flex: query bar + document area] [320px detail panel]
```

- Side-nav gets a new **Mongo** icon (leaf/cluster SVG) added to `sim-nav.js`
- New file: `frontend/sim-mongo.html`
- New Go package: `pkg/mongoclient/`
- New bridge methods on `App` in `app.go` (all prefixed `MC*`)

---

## Phase 1 — Foundation: Connection Manager + Tree

**Goal:** connect to a MongoDB instance, see the database/collection tree.

### Go (`pkg/mongoclient/`)

- `mongoclient.go`
  - `Connect(id, uri string) error` — creates `*mongo.Client`, stores in a map keyed by `id`
  - `Disconnect(id string) error`
  - `ListDatabases(id string) []string`
  - `ListCollections(id, db string) []CollectionMeta` — name + doc count
  - `CollectionStats(id, db, coll string) CollStats` — count, storageSize, avgDocSize

- `store.go` — SQLite (`mongoclient.db`) for saved connections
  - table `connections(id TEXT PK, label TEXT, uri TEXT, created_at INT)`
  - Go funcs: `SaveConnection`, `ListConnections`, `DeleteConnection`

### Bridge methods (`app.go`)

```go
MCListSavedConnections() []MCConnection
MCSaveConnection(label, uri string) MCConnection
MCDeleteConnection(id string) error
MCConnect(id string) error          // returns error with message
MCDisconnect(id string)
MCListDatabases(id string) []string
MCListCollections(id, db string) []MCCollectionMeta
MCCollectionStats(id, db, coll string) MCCollStats
```

### Frontend (`sim-mongo.html`)

**Left tree panel (200px)**
- "+" button → Connection modal (label + URI + Test button)
- Each connection: expand → databases → expand → collections
- Collection row shows doc count chip
- Right-click context menu: Refresh / Drop Collection / Stats

**Main area (initial state)**
- Empty state: "Select a collection to browse documents"

**Connection modal**
- Label input
- URI input (e.g. `mongodb://localhost:27017`)
- "Test Connection" button → calls `MCConnect` + `MCDisconnect` just to validate
- Save / Cancel

---

## Phase 2 — Document Browser + Basic CRUD

**Goal:** view, insert, edit, delete documents in a collection.

### Go additions

```go
MCFind(id, db, coll string, filter, sort, projection string, skip, limit int) MCFindResult
// MCFindResult { docs []json.RawMessage, total int64, page int, pages int }

MCInsertOne(id, db, coll, docJSON string) string   // returns inserted _id
MCUpdateOne(id, db, coll, filter, update string) int64  // matchedCount
MCDeleteOne(id, db, coll, filter string) int64
MCDeleteMany(id, db, coll, filter string) int64
```

- `filter`, `sort`, `projection`, `update` are JSON strings — parsed server-side via `bson.UnmarshalExtJSON`
- All errors returned as Go `error` (Wails marshals to JS rejected Promise)

### Frontend additions

**Query bar (above document area)**
```
[Filter: {}________] [Sort: {}__] [Proj: {}__] [Skip: 0] [Limit: 20▼] [▶ Run]
```
- Each field is a single-line code input with monospace font
- Limit dropdown: 20 / 50 / 100 / 250
- "Run" calls `MCFind`, stores last query in localStorage per collection

**Document area — two view modes (toggle top-right)**

*Table view*
- Columns auto-detected from first N docs; extra fields shown as `…`
- Each row: checkbox | fields… | [Edit] [Dup] [Del] actions on hover
- Sticky header with column names

*JSON view*
- Virtualized list of JSON blocks (expandable, syntax-highlighted via CSS)
- Each block has [Edit] [Dup] [Del] buttons top-right

**Pagination bar**
```
Showing 1–20 of 1,432  [< Prev]  [page 1 of 72]  [Next >]
```

**Insert document button** (top-right of query bar)
- Opens a modal with a JSON textarea pre-filled with `{}`
- Submit → `MCInsertOne` → refresh

**Edit modal**
- Full JSON editor (textarea, monospace, auto-resize)
- Save → `MCUpdateOne` with `{ $set: <edited doc> }` after stripping `_id`

**Delete confirmation**
- Small inline confirmation (don't use `window.confirm`) — "Delete this document?" [Cancel] [Delete]

---

## Phase 3 — Aggregation Pipeline + Query History

**Goal:** run multi-stage aggregation pipelines with per-stage preview.

### Go additions

```go
MCAggregate(id, db, coll string, pipeline string, limit int) MCFindResult
// pipeline is a JSON array string: [{"$match":{...}}, {"$group":{...}}]

MCQueryHistory(db, coll string) []MCQueryEntry  // last 50, stored in mongoclient.db
MCSaveQueryHistory(db, coll, filter, sort, proj string)
MCClearQueryHistory(db, coll string)
```

- `history` table in `mongoclient.db`: `(id INT PK, conn_id, db, coll, filter, sort, proj, ran_at INT)`

### Frontend additions

**Aggregation tab** (tab bar above document area: `Find | Aggregate`)

*Pipeline panel*
- "+ Add Stage" button → dropdown of common stages: `$match` `$group` `$project` `$sort` `$limit` `$lookup` `$unwind` `$addFields` `$count`
- Each stage: stage-type label chip + JSON textarea for stage body
- Drag to reorder (simple up/down arrows, no DnD library needed)
- [▶ Preview this stage] — runs pipeline truncated to this stage with `limit: 5`
- [▶ Run All] — runs full pipeline, shows results in document area (JSON view only)
- [Copy as JS] — copies `db.collection.aggregate([...])` to clipboard

**Query history drawer** (history icon button in query bar)
- Slide-in panel (right side, 280px) showing last 50 queries for current collection
- Each entry: timestamp + filter preview + [Re-run] [Delete]

---

## Phase 4 — Indexes + Import/Export + Schema

**Goal:** manage indexes, move data in/out, understand document shape.

### Go additions

```go
MCListIndexes(id, db, coll string) []MCIndex
MCCreateIndex(id, db, coll, keysJSON, optionsJSON string) string  // returns index name
MCDropIndex(id, db, coll, indexName string) error

MCImport(id, db, coll, format, filepath string, upsert bool) MCImportResult
// format: "json" (array) | "jsonl" | "csv"
// MCImportResult { inserted, updated, failed int, errors []string }

MCExport(id, db, coll, filter, format string) string  // returns JSON/JSONL/CSV string → SaveFile
MCSchemaAnalyze(id, db, coll, filter string, sampleSize int) []MCFieldStat
// MCFieldStat { path, type string, frequency float64, nullPct float64 }
```

### Frontend additions

**Indexes tab** (tab bar: `Find | Aggregate | Indexes | Schema`)

*Index list*
- Table: Name | Keys | Unique | Sparse | Size
- [+ Create Index] button → modal with keys JSON + options (unique, sparse, name)
- Each row has [Drop] button with confirmation

**Import/Export panel** (button in collection context menu or toolbar)
- Export: filter input + format picker (JSON / JSONL / CSV) → `MCExport` → `App.SaveFile`
- Import: `App.ACPickFile` to pick file → format auto-detect from extension → options (upsert toggle) → progress + result summary

**Schema tab**
- Sample size slider (100 / 500 / 1000 / all)
- Table: Field path | Type | Presence % | Null %
- Sorted by presence descending
- Read-only, informational

---

## Phase 5 — Polish + Persistence

**Goal:** make it feel complete and reliable.

- **Tab survival**: `_mongoConnected`, `_mongoActiveConn`, `_mongoActiveDb`, `_mongoActiveColl` flags in `SimState.global` — restore tree selection on page reload
- **Error toasts**: Go errors surface as dismissable red toast (reuse existing toast pattern from `api-client.html`)
- **Connection status chip**: in tree panel header — green dot "Connected" / red "Disconnected"
- **Auto-reconnect**: on page load, if a connection was active, call `MCConnect` again silently
- **Keyboard shortcuts**:
  - `Cmd+Enter` → Run query
  - `Cmd+R` → Refresh collection
  - `Cmd+N` → New document
  - `Escape` → Close modal
- **Result count accuracy**: for large collections use `estimatedDocumentCount` for the chip in the tree (fast), `countDocuments(filter)` only when a filter is active
- **Security**: URI is stored in SQLite in plaintext — add a note in the UI ("URIs stored locally, unencrypted")
- **mongoclient.db** lives alongside `apiclient.db` in the same output dir (`APIClientInit` sets this dir already; reuse it)

---

## File checklist (new files only)

```
pkg/mongoclient/
  mongoclient.go    Connection pool, find, insert, update, delete, aggregate
  store.go          SQLite: saved connections + query history
  indexes.go        List / create / drop indexes
  schema.go         Schema analysis via sampling
  importexport.go   Import (JSON/JSONL/CSV) + Export

frontend/
  sim-mongo.html    The full MongoDB UI page
```

Modified files:
- `app.go` — add MC* bridge methods
- `frontend/sim-nav.js` — add Mongo icon to nav
- `go.mod / go.sum` — add `go.mongodb.org/mongo-driver`

---

## Go dependencies

```
go get go.mongodb.org/mongo-driver/mongo
go get go.mongodb.org/mongo-driver/bson
```

No other new dependencies. CSV import/export uses stdlib `encoding/csv`.

---

## Execution order

| # | Phase | Estimated complexity | Deliverable |
|---|---|---|---|
| 1 | Foundation: connection + tree | Medium | Connect, browse DBs/colls |
| 2 | Document browser + CRUD | High | Full read/write on documents |
| 3 | Aggregation + query history | Medium | Pipeline runner |
| 4 | Indexes + import/export + schema | Medium | Data movement |
| 5 | Polish + persistence | Low | Production-ready feel |

Each phase is independently shippable — the app stays working after every phase.
