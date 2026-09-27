# MongoDB Client — Implementation Plan

Lightweight Studio 3T alternative built into the app. Target: fast connection, browse, query, and CRUD — no bloat.

---

## Tech Stack

| Layer | Choice |
|---|---|
| Go driver | `go.mongodb.org/mongo-driver v1.x` |
| Backend package | `pkg/mongodb/` |
| Frontend | `mongo.html` (same app style) |
| Nav entry | `sim-nav.js` — icon `⌬`, key `mongo` |
| State | Connection pool held in Go, JS calls Go for all data |

---

## What We Skip (vs Studio 3T)

- No IntelliShell / full Mongo shell
- No schema analysis / data masking
- No visual query builder drag-drop
- No compare / diff collections
- No SQL migration layer
- No team sharing / cloud sync

---

## Phase 1 — Connection Manager

**Goal:** Save, load, and establish MongoDB connections.

### Backend (`pkg/mongodb/`)
```
MongoConnect(opts ConnectionOpts) error
MongoDisconnect(connID string) error
MongoTestConnection(opts ConnectionOpts) TestResult
MongoListConnections() []SavedConnection
MongoSaveConnection(conn SavedConnection) SavedConnection
MongoDeleteConnection(id string) error
MongoGetActiveConnection() *SavedConnection
```

### `ConnectionOpts` struct
```go
type ConnectionOpts struct {
    ID          string
    Name        string
    URI         string          // primary input
    Host        string          // or manual host/port
    Port        int
    AuthDB      string
    Username    string
    Password    string
    TLS         bool
    TLSCert     string          // path or PEM string
    ReplicaSet  string
    Timeout     int             // seconds
}
```

### UI (`mongo.html` — Connection screen)
- Left panel: saved connections list (name + host chip + status dot)
- Right panel: connection form (URI mode toggle ↔ manual mode)
- "Test Connection" button → shows latency + server version
- "Connect" → opens the database browser
- Connections persisted in SQLite (reuse existing DB infra)

### Deliverable
Working connect/disconnect with saved connections. Opens to empty DB browser on success.

---

## Phase 2 — Database & Collection Browser

**Goal:** Tree view of databases → collections with basic stats.

### Backend
```
MongoListDatabases(connID string) []DatabaseInfo
MongoListCollections(connID, dbName string) []CollectionInfo
MongoCreateDatabase(connID, dbName string) error        // via create collection
MongoDropDatabase(connID, dbName string) error
MongoCreateCollection(connID, dbName, colName string) error
MongoDropCollection(connID, dbName, colName string) error
MongoCollectionStats(connID, dbName, colName string) CollectionStats
```

### `CollectionInfo` struct
```go
type CollectionInfo struct {
    Name        string
    Type        string   // collection | view | timeseries
    DocCount    int64
    StorageSize int64
    AvgDocSize  float64
    Indexes     int
}
```

### UI
- Left sidebar: tree — `Connection > Database > Collection`
  - Expand/collapse nodes
  - Right-click context menu: Create / Drop / Rename / Stats
  - Search/filter tree input
  - Doc count badge on each collection
- Right: collection stats card when collection selected (size, doc count, index count, avg doc size)
- Tab bar at top (each opened collection = tab, max 8 tabs)

### Deliverable
Full tree browser. Can create/drop databases and collections.

---

## Phase 3 — Document Viewer

**Goal:** Browse documents in a collection with pagination.

### Backend
```
MongoFind(req FindRequest) FindResult
MongoFindOne(connID, db, col, id string) map[string]any
MongoCountDocuments(connID, db, col string, filter string) int64
```

### `FindRequest` struct
```go
type FindRequest struct {
    ConnID     string
    DB         string
    Collection string
    Filter     string   // JSON string, default {}
    Sort       string   // JSON string, default {}
    Projection string   // JSON string, default {}
    Skip       int64
    Limit      int64    // default 50
}
```

### `FindResult` struct
```go
type FindResult struct {
    Documents []map[string]any
    Total     int64
    Page      int
    PageSize  int
    Took      int64   // ms
}
```

### UI
- Three view modes (toggle in toolbar):
  - **Tree view** — collapsible JSON tree per document (like Studio 3T)
  - **JSON view** — raw pretty-printed JSON, syntax highlighted
  - **Table view** — auto-detected columns, rows/columns grid
- Pagination bar: `< 1 … 5 6 7 … 20 >` with page size selector (20 / 50 / 100 / 200)
- Top toolbar: Filter input, Sort input, Projection input (all JSON), Refresh button
- Document count + query time shown in status bar
- Click any `_id` → copy to clipboard
- Click document row → opens document detail drawer

### Deliverable
Full read-only document browser with all three views and pagination.

---

## Phase 4 — CRUD Operations

**Goal:** Insert, edit, and delete documents.

### Backend
```
MongoInsertOne(connID, db, col, doc string) InsertResult
MongoInsertMany(connID, db, col string, docs []string) InsertManyResult
MongoUpdateOne(connID, db, col, filter, update string, upsert bool) UpdateResult
MongoReplaceOne(connID, db, col, filter, doc string) UpdateResult
MongoDeleteOne(connID, db, col, filter string) DeleteResult
MongoDeleteMany(connID, db, col, filter string) DeleteResult
```

### UI
- **Insert** button in toolbar → modal with JSON editor (syntax highlighting, validate on type)
  - Toggle: single doc / array of docs
  - "Insert Many" from file (JSON array file picker)
- **Edit** — click edit icon on any document row → modal opens with document JSON pre-filled
  - Shows diff of changes before saving
  - Mode: Replace doc / `$set` patch
- **Delete** — click delete icon → confirm dialog showing the document
- **Bulk delete** — checkbox select rows → "Delete N documents" button
- All operations show result toast: `Inserted 1 · 4ms` / `Modified 3 · 12ms`

### Deliverable
Full CRUD. Insert/edit/delete single and bulk.

---

## Phase 5 — Query Editor

**Goal:** Run arbitrary find queries with full control.

### UI (new tab type: "Query")
- Four-pane editor layout:
  ```
  [ Filter ]   [ Sort ]
  [ Projection ]  [ Options: limit / skip / hint ]
  ```
- Each pane is a mini JSON editor with syntax highlighting
- "Run" button (or Cmd+Enter) → results shown in document viewer below
- Query history (last 50 queries per collection, stored in SQLite)
- "Explain" button → runs `explain("executionStats")` and shows index usage, docs examined, ms

### Backend
```
MongoExplain(req FindRequest) map[string]any
```

### Deliverable
Full query editor with explain plan viewer.

---

## Phase 6 — Aggregation Pipeline

**Goal:** Build and run aggregation pipelines.

### UI
- Stage list (left) + stage editor (right)
- Each stage: `$match`, `$group`, `$sort`, `$project`, `$lookup`, `$unwind`, `$limit`, `$skip`, `$addFields`, `$count`
- Add stage button → dropdown of stage types with description
- Drag to reorder stages
- Live preview: run pipeline up to selected stage
- Export pipeline as: JSON array / `mongosh` command / Go code snippet

### Backend
```
MongoAggregate(connID, db, col string, pipeline string, opts AggOpts) FindResult
```

### Deliverable
Visual pipeline builder with live preview.

---

## Phase 7 — Index Manager

**Goal:** View, create, and drop indexes.

### Backend
```
MongoListIndexes(connID, db, col string) []IndexInfo
MongoCreateIndex(connID, db, col string, def IndexDef) error
MongoDropIndex(connID, db, col, indexName string) error
```

### `IndexInfo` struct
```go
type IndexInfo struct {
    Name       string
    Keys       map[string]int   // field → 1/-1/text/2dsphere
    Unique     bool
    Sparse     bool
    TTL        int              // expireAfterSeconds, 0 = no TTL
    Size       int64
    UsageOps   int64
    Background bool
}
```

### UI
- Table: Name | Keys | Type | Unique | Size | Usage | Actions
- Create index modal: key fields builder (add field → pick direction), unique/sparse/TTL toggles
- Drop with confirmation
- Highlight unused indexes (0 ops since last restart)

### Deliverable
Full index management panel.

---

## Phase 8 — Import / Export

**Goal:** Import JSON/CSV into a collection; export query results.

### Backend
```
MongoExportCollection(connID, db, col, filter, format, outPath string) ExportResult
MongoImportFile(connID, db, col, filePath, format string, opts ImportOpts) ImportResult
```

### Formats
- Export: JSON array, NDJSON (newline-delimited), CSV (flat docs only)
- Import: JSON array, NDJSON, CSV

### UI
- Export: toolbar button → modal (format, filter to export subset, output path picker)
- Import: toolbar button → file picker → preview first 10 rows → options (upsert / insert / replace) → run with progress bar
- Result: `Imported 4,231 documents · 3 errors · 1.2s`

---

## File Layout

```
pkg/
  mongodb/
    client.go       — connect/disconnect/pool
    crud.go         — find/insert/update/delete
    schema.go       — list DBs/collections/indexes/stats
    aggregate.go    — aggregation pipeline runner
    importexport.go — import/export logic

frontend/
  mongo.html        — full page
  mongo-tree.js     — reusable tree component
  mongo-editor.js   — reusable JSON editor component

app.go              — Mongo* bridge methods
sim-nav.js          — add mongo nav entry
```

---

## Execution Order

| Phase | Effort | Unblocks |
|---|---|---|
| 1 — Connections | Medium | everything |
| 2 — Tree Browser | Medium | 3, 4, 5, 7 |
| 3 — Document Viewer | Large | 4, 5 |
| 4 — CRUD | Medium | daily use |
| 5 — Query Editor | Medium | power users |
| 6 — Aggregation | Large | analytics |
| 7 — Index Manager | Small | perf tuning |
| 8 — Import/Export | Small | data migration |

**Phases 1–4 = MVP.** Phases 5–8 = v2 additions.
