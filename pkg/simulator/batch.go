package simulator

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	simcrypto "device-simulator/pkg/crypto"

	_ "modernc.org/sqlite"
)

func initDB(dbPath string, encrypted bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	datastringType := "CHAR(3000)"
	if encrypted {
		datastringType = "BLOB"
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS oData (
			DNO INTEGER PRIMARY KEY AUTOINCREMENT,
			DATASTRING ` + datastringType + ` NOT NULL
		);
		CREATE TABLE IF NOT EXISTS nSetting (
			NO INT, DRATE INT, LSPB REAL, MSPB REAL, HSPB REAL,
			LSPA REAL, MSPA REAL, HSPA REAL, HDOP REAL, ODRATE INT,
			SPEED INT, LEDON INT, URL CHAR(400), PORT INT
		);
		CREATE TABLE IF NOT EXISTS newAPN (
			NO INT, SSELECT INT, APN1 CHAR(50), APN2 CHAR(50)
		);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func createBatchFile(batchNum int, packets []map[string]interface{}, publicKeyPEM string, dbPath string, cfg Config, emit func(SimEvent), startT time.Time) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	db, err := initDB(dbPath, cfg.EncryptEnabled)
	if err != nil {
		return fmt.Errorf("init db: %w", err)
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	stmt, err := tx.Prepare("INSERT INTO oData(DATASTRING) VALUES (?)")
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for i, pkt := range packets {
		data, err := json.Marshal(pkt)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("marshal pkt %d: %w", i, err)
		}

		var row interface{}
		if cfg.EncryptEnabled {
			enc, err := simcrypto.EncryptPacket(data, publicKeyPEM, cfg.AESVersion)
			if err != nil {
				tx.Rollback()
				return fmt.Errorf("encrypt pkt %d: %w", i, err)
			}
			row = enc
		} else {
			row = string(data)
		}

		if _, err := stmt.Exec(row); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert pkt %d: %w", i, err)
		}

		if (i+1)%50 == 0 || i+1 == len(packets) {
			emit(SimEvent{
				Elapsed: time.Since(startT).Milliseconds(),
				Tag:     "BATCH",
				Cls:     "bt",
				Msg:     fmt.Sprintf("Batch %d: %d/%d rows", batchNum, i+1, len(packets)),
				Ty:      "info",
			})
		}
	}

	return tx.Commit()
}

// createOBDAccumDB opens or creates the live OBD accumulation database.
func createOBDAccumDB(dbPath string, encrypted bool) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	return initDB(dbPath, encrypted)
}

// insertOBDRow inserts a single encrypted/plain OBD packet into the accum DB.
func insertOBDRow(db *sql.DB, pkt map[string]interface{}, publicKeyPEM string, cfg Config) error {
	data, err := json.Marshal(pkt)
	if err != nil {
		return err
	}
	var row interface{}
	if cfg.EncryptEnabled {
		enc, err := simcrypto.EncryptPacket(data, publicKeyPEM, cfg.AESVersion)
		if err != nil {
			return err
		}
		row = enc
	} else {
		row = string(data)
	}
	_, err = db.Exec("INSERT INTO oData(DATASTRING) VALUES (?)", row)
	return err
}
