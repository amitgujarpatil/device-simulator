package mongoclient

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ── Types ──────────────────────────────────────────────────────────────────

type Connection struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	URI       string `json:"uri"`
	CreatedAt int64  `json:"createdAt"`
}

type QueryEntry struct {
	ID        string `json:"id"`
	ConnID    string `json:"connId"`
	DB        string `json:"db"`
	Coll      string `json:"coll"`
	Filter    string `json:"filter"`
	Sort      string `json:"sort"`
	Proj      string `json:"proj"`
	RanAt     int64  `json:"ranAt"`
}

// ── Store ──────────────────────────────────────────────────────────────────

type Store struct {
	db *sql.DB
}

var globalStore *Store

func InitStore(dir string) error {
	if globalStore != nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", filepath.Join(dir, "mongoclient.db"))
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	s := &Store{db: sqlDB}
	if err := s.migrate(); err != nil {
		return err
	}
	globalStore = s
	return nil
}

func getStore() (*Store, error) {
	if globalStore == nil {
		return nil, fmt.Errorf("mongo store not initialised")
	}
	return globalStore, nil
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%d%x", time.Now().UnixMilli(), b)
}

func (s *Store) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS connections (
			id TEXT PRIMARY KEY, label TEXT NOT NULL, uri TEXT NOT NULL, created_at INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS query_history (
			id TEXT PRIMARY KEY, conn_id TEXT NOT NULL, db TEXT NOT NULL, coll TEXT NOT NULL,
			filter_json TEXT NOT NULL DEFAULT '{}', sort_json TEXT NOT NULL DEFAULT '{}',
			proj_json TEXT NOT NULL DEFAULT '{}', ran_at INTEGER NOT NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ── Connections ────────────────────────────────────────────────────────────

func ListSavedConnections() ([]Connection, error) {
	s, err := getStore()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, label, uri, created_at FROM connections ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		var c Connection
		rows.Scan(&c.ID, &c.Label, &c.URI, &c.CreatedAt)
		out = append(out, c)
	}
	if out == nil {
		out = []Connection{}
	}
	return out, nil
}

func SaveConnection(label, uri string) (Connection, error) {
	s, err := getStore()
	if err != nil {
		return Connection{}, err
	}
	c := Connection{
		ID:        newID(),
		Label:     label,
		URI:       uri,
		CreatedAt: time.Now().UnixMilli(),
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO connections(id,label,uri,created_at) VALUES(?,?,?,?)`,
		c.ID, c.Label, c.URI, c.CreatedAt)
	return c, err
}

func UpdateConnection(id, label, uri string) error {
	s, err := getStore()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE connections SET label=?, uri=? WHERE id=?`, label, uri, id)
	return err
}

func DeleteConnection(id string) error {
	s, err := getStore()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM connections WHERE id=?`, id)
	return err
}

func GetConnection(id string) (Connection, error) {
	s, err := getStore()
	if err != nil {
		return Connection{}, err
	}
	var c Connection
	err = s.db.QueryRow(`SELECT id, label, uri, created_at FROM connections WHERE id=?`, id).
		Scan(&c.ID, &c.Label, &c.URI, &c.CreatedAt)
	return c, err
}

// ── Query History ──────────────────────────────────────────────────────────

func SaveQueryHistory(connID, db, coll, filter, sort, proj string) error {
	s, err := getStore()
	if err != nil {
		return err
	}
	entry := QueryEntry{
		ID:     newID(),
		ConnID: connID,
		DB:     db,
		Coll:   coll,
		Filter: filter,
		Sort:   sort,
		Proj:   proj,
		RanAt:  time.Now().UnixMilli(),
	}
	_, err = s.db.Exec(
		`INSERT INTO query_history(id,conn_id,db,coll,filter_json,sort_json,proj_json,ran_at) VALUES(?,?,?,?,?,?,?,?)`,
		entry.ID, entry.ConnID, entry.DB, entry.Coll, entry.Filter, entry.Sort, entry.Proj, entry.RanAt)
	if err != nil {
		return err
	}
	// trim to 50 per collection
	s.db.Exec(`DELETE FROM query_history WHERE conn_id=? AND db=? AND coll=? AND id NOT IN
		(SELECT id FROM query_history WHERE conn_id=? AND db=? AND coll=? ORDER BY ran_at DESC LIMIT 50)`,
		connID, db, coll, connID, db, coll)
	return nil
}

func ListQueryHistory(connID, db, coll string) ([]QueryEntry, error) {
	s, err := getStore()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id,conn_id,db,coll,filter_json,sort_json,proj_json,ran_at
		 FROM query_history WHERE conn_id=? AND db=? AND coll=? ORDER BY ran_at DESC LIMIT 50`,
		connID, db, coll)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueryEntry
	for rows.Next() {
		var e QueryEntry
		rows.Scan(&e.ID, &e.ConnID, &e.DB, &e.Coll, &e.Filter, &e.Sort, &e.Proj, &e.RanAt)
		out = append(out, e)
	}
	if out == nil {
		out = []QueryEntry{}
	}
	return out, nil
}

func ClearQueryHistory(connID, db, coll string) error {
	s, err := getStore()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM query_history WHERE conn_id=? AND db=? AND coll=?`, connID, db, coll)
	return err
}
