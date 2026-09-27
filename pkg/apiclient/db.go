package apiclient

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ── Types ──────────────────────────────────────────────────────────────────

type KVPair struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

type Workspace struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"createdAt"`
}

type Collection struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	ParentID    string `json:"parentId"`
	Name        string `json:"name"`
	SortOrder   int    `json:"sortOrder"`
}

type SavedRequest struct {
	ID           string            `json:"id"`
	CollectionID string            `json:"collectionId"`
	WorkspaceID  string            `json:"workspaceId"`
	Name         string            `json:"name"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Params       []KVPair          `json:"params"`
	Headers      []KVPair          `json:"headers"`
	BodyType     string            `json:"bodyType"`
	BodyContent  string            `json:"bodyContent"`
	AuthType     string            `json:"authType"`
	AuthData     map[string]string `json:"authData"`
	SortOrder    int               `json:"sortOrder"`
	UpdatedAt    int64             `json:"updatedAt"`
}

type HistoryEntry struct {
	ID          string            `json:"id"`
	RequestID   string            `json:"requestId"`
	WorkspaceID string            `json:"workspaceId"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Status      int               `json:"status"`
	StatusText  string            `json:"statusText"`
	RespHeaders map[string]string `json:"headers"`
	Body        string            `json:"body"`
	DurationMs  int64             `json:"durationMs"`
	SizeBytes   int               `json:"sizeBytes"`
	Timestamp   int64             `json:"timestamp"`
}

type Environment struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	IsActive    bool   `json:"isActive"`
}

type EnvVariable struct {
	ID      string `json:"id"`
	EnvID   string `json:"envId"`
	Key     string `json:"key"`
	Value   string `json:"value"`
	Enabled bool   `json:"enabled"`
}

// ── DB ─────────────────────────────────────────────────────────────────────

type DB struct {
	db *sql.DB
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%d%x", time.Now().UnixMilli(), b)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func Open(dir string) (*DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "apiclient.db"))
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	d := &DB{db: sqlDB}
	return d, d.migrate()
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS workspaces (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS collections (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL, sort_order INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS requests (
			id TEXT PRIMARY KEY, collection_id TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL,
			name TEXT NOT NULL, method TEXT NOT NULL DEFAULT 'GET', url TEXT NOT NULL DEFAULT '',
			params_json TEXT NOT NULL DEFAULT '[]', headers_json TEXT NOT NULL DEFAULT '[]',
			body_type TEXT NOT NULL DEFAULT 'none', body_content TEXT NOT NULL DEFAULT '',
			auth_type TEXT NOT NULL DEFAULT 'none', auth_json TEXT NOT NULL DEFAULT '{}',
			sort_order INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS response_history (
			id TEXT PRIMARY KEY, request_id TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL,
			method TEXT NOT NULL, url TEXT NOT NULL, status INTEGER NOT NULL,
			status_text TEXT NOT NULL, resp_headers_json TEXT NOT NULL DEFAULT '{}',
			body TEXT NOT NULL DEFAULT '', duration_ms INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL, timestamp INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS environments (
			id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, name TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS env_variables (
			id TEXT PRIMARY KEY, env_id TEXT NOT NULL, key TEXT NOT NULL,
			value TEXT NOT NULL DEFAULT '', enabled INTEGER NOT NULL DEFAULT 1)`,
	}
	for _, s := range stmts {
		if _, err := d.db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ── Workspaces ─────────────────────────────────────────────────────────────

func (d *DB) ListWorkspaces() ([]Workspace, error) {
	rows, err := d.db.Query(`SELECT id, name, created_at FROM workspaces ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		rows.Scan(&w.ID, &w.Name, &w.CreatedAt)
		out = append(out, w)
	}
	return out, nil
}

func (d *DB) SaveWorkspace(w Workspace) (Workspace, error) {
	if w.ID == "" {
		w.ID = newID()
	}
	w.CreatedAt = time.Now().UnixMilli()
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO workspaces(id,name,created_at) VALUES(?,?,?)`,
		w.ID, w.Name, w.CreatedAt)
	return w, err
}

func (d *DB) DeleteWorkspace(id string) error {
	for _, q := range []string{
		`DELETE FROM env_variables WHERE env_id IN (SELECT id FROM environments WHERE workspace_id=?)`,
		`DELETE FROM environments WHERE workspace_id=?`,
		`DELETE FROM response_history WHERE workspace_id=?`,
		`DELETE FROM requests WHERE workspace_id=?`,
		`DELETE FROM collections WHERE workspace_id=?`,
		`DELETE FROM workspaces WHERE id=?`,
	} {
		if _, err := d.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

// ── Collections ────────────────────────────────────────────────────────────

func (d *DB) ListCollections(workspaceID string) ([]Collection, error) {
	rows, err := d.db.Query(
		`SELECT id, workspace_id, parent_id, name, sort_order FROM collections WHERE workspace_id=? ORDER BY sort_order, name`,
		workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var c Collection
		rows.Scan(&c.ID, &c.WorkspaceID, &c.ParentID, &c.Name, &c.SortOrder)
		out = append(out, c)
	}
	return out, nil
}

func (d *DB) SaveCollection(c Collection) (Collection, error) {
	if c.ID == "" {
		c.ID = newID()
	}
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO collections(id,workspace_id,parent_id,name,sort_order) VALUES(?,?,?,?,?)`,
		c.ID, c.WorkspaceID, c.ParentID, c.Name, c.SortOrder)
	return c, err
}

func (d *DB) DeleteCollection(id string) error {
	// orphan requests (move to root) rather than delete them
	if _, err := d.db.Exec(`UPDATE requests SET collection_id='' WHERE collection_id=?`, id); err != nil {
		return err
	}
	_, err := d.db.Exec(`DELETE FROM collections WHERE id=?`, id)
	return err
}

// ── Requests ───────────────────────────────────────────────────────────────

func (d *DB) ListRequests(workspaceID string) ([]SavedRequest, error) {
	rows, err := d.db.Query(
		`SELECT id, collection_id, workspace_id, name, method, url,
		 params_json, headers_json, body_type, body_content, auth_type, auth_json,
		 sort_order, updated_at FROM requests WHERE workspace_id=? ORDER BY sort_order, name`,
		workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedRequest
	for rows.Next() {
		var r SavedRequest
		var pj, hj, aj string
		rows.Scan(&r.ID, &r.CollectionID, &r.WorkspaceID, &r.Name, &r.Method, &r.URL,
			&pj, &hj, &r.BodyType, &r.BodyContent, &r.AuthType, &aj, &r.SortOrder, &r.UpdatedAt)
		json.Unmarshal([]byte(pj), &r.Params)
		json.Unmarshal([]byte(hj), &r.Headers)
		json.Unmarshal([]byte(aj), &r.AuthData)
		if r.Params == nil {
			r.Params = []KVPair{}
		}
		if r.Headers == nil {
			r.Headers = []KVPair{}
		}
		if r.AuthData == nil {
			r.AuthData = map[string]string{}
		}
		out = append(out, r)
	}
	return out, nil
}

func (d *DB) SaveRequest(r SavedRequest) (SavedRequest, error) {
	if r.ID == "" {
		r.ID = newID()
	}
	pj, _ := json.Marshal(r.Params)
	hj, _ := json.Marshal(r.Headers)
	aj, _ := json.Marshal(r.AuthData)
	r.UpdatedAt = time.Now().UnixMilli()
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO requests(id,collection_id,workspace_id,name,method,url,params_json,headers_json,body_type,body_content,auth_type,auth_json,sort_order,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.CollectionID, r.WorkspaceID, r.Name, r.Method, r.URL,
		string(pj), string(hj), r.BodyType, r.BodyContent, r.AuthType, string(aj), r.SortOrder, r.UpdatedAt)
	return r, err
}

func (d *DB) DeleteRequest(id string) error {
	_, err := d.db.Exec(`DELETE FROM requests WHERE id=?`, id)
	return err
}

// ── History ────────────────────────────────────────────────────────────────

func (d *DB) SaveHistory(h HistoryEntry) error {
	if h.ID == "" {
		h.ID = newID()
	}
	if h.Timestamp == 0 {
		h.Timestamp = time.Now().UnixMilli()
	}
	hj, _ := json.Marshal(h.RespHeaders)
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO response_history(id,request_id,workspace_id,method,url,status,status_text,resp_headers_json,body,duration_ms,size_bytes,timestamp) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		h.ID, h.RequestID, h.WorkspaceID, h.Method, h.URL, h.Status, h.StatusText,
		string(hj), h.Body, h.DurationMs, h.SizeBytes, h.Timestamp)
	// trim to 200 entries
	d.db.Exec(`DELETE FROM response_history WHERE workspace_id=? AND id NOT IN (SELECT id FROM response_history WHERE workspace_id=? ORDER BY timestamp DESC LIMIT 200)`,
		h.WorkspaceID, h.WorkspaceID)
	return err
}

func (d *DB) ListHistory(workspaceID string, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 60
	}
	rows, err := d.db.Query(
		`SELECT id, request_id, workspace_id, method, url, status, status_text,
		 resp_headers_json, body, duration_ms, size_bytes, timestamp
		 FROM response_history WHERE workspace_id=? ORDER BY timestamp DESC LIMIT ?`,
		workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var h HistoryEntry
		var hj string
		rows.Scan(&h.ID, &h.RequestID, &h.WorkspaceID, &h.Method, &h.URL,
			&h.Status, &h.StatusText, &hj, &h.Body, &h.DurationMs, &h.SizeBytes, &h.Timestamp)
		json.Unmarshal([]byte(hj), &h.RespHeaders)
		if h.RespHeaders == nil {
			h.RespHeaders = map[string]string{}
		}
		out = append(out, h)
	}
	return out, nil
}

func (d *DB) ClearHistory(workspaceID string) error {
	_, err := d.db.Exec(`DELETE FROM response_history WHERE workspace_id=?`, workspaceID)
	return err
}

// ── Environments ───────────────────────────────────────────────────────────

func (d *DB) ListEnvironments(workspaceID string) ([]Environment, error) {
	rows, err := d.db.Query(
		`SELECT id, workspace_id, name, is_active FROM environments WHERE workspace_id=? ORDER BY name`,
		workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		var active int
		rows.Scan(&e.ID, &e.WorkspaceID, &e.Name, &active)
		e.IsActive = active == 1
		out = append(out, e)
	}
	return out, nil
}

func (d *DB) SaveEnvironment(e Environment) (Environment, error) {
	if e.ID == "" {
		e.ID = newID()
	}
	_, err := d.db.Exec(
		`INSERT OR REPLACE INTO environments(id,workspace_id,name,is_active) VALUES(?,?,?,?)`,
		e.ID, e.WorkspaceID, e.Name, boolInt(e.IsActive))
	return e, err
}

func (d *DB) SetActiveEnvironment(workspaceID, envID string) error {
	if _, err := d.db.Exec(`UPDATE environments SET is_active=0 WHERE workspace_id=?`, workspaceID); err != nil {
		return err
	}
	if envID == "" {
		return nil
	}
	_, err := d.db.Exec(`UPDATE environments SET is_active=1 WHERE id=?`, envID)
	return err
}

func (d *DB) DeleteEnvironment(id string) error {
	for _, q := range []string{
		`DELETE FROM env_variables WHERE env_id=?`,
		`DELETE FROM environments WHERE id=?`,
	} {
		if _, err := d.db.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ListEnvVariables(envID string) ([]EnvVariable, error) {
	rows, err := d.db.Query(
		`SELECT id, env_id, key, value, enabled FROM env_variables WHERE env_id=? ORDER BY rowid`,
		envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnvVariable
	for rows.Next() {
		var v EnvVariable
		var en int
		rows.Scan(&v.ID, &v.EnvID, &v.Key, &v.Value, &en)
		v.Enabled = en == 1
		out = append(out, v)
	}
	return out, nil
}

func (d *DB) SaveEnvVariables(envID string, vars []EnvVariable) error {
	if _, err := d.db.Exec(`DELETE FROM env_variables WHERE env_id=?`, envID); err != nil {
		return err
	}
	for _, v := range vars {
		if v.ID == "" {
			v.ID = newID()
		}
		if _, err := d.db.Exec(
			`INSERT INTO env_variables(id,env_id,key,value,enabled) VALUES(?,?,?,?,?)`,
			v.ID, envID, v.Key, v.Value, boolInt(v.Enabled)); err != nil {
			return err
		}
	}
	return nil
}
