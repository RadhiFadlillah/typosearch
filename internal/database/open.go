package database

import (
	_ "embed"
	"fmt"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
)

// Open opens database and initiate the tables if they don't exist.
func Open(path string) (db *sqlx.DB, err error) {
	// Prepare DSN
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	dsn := "file:" + path + "?" + q.Encode()

	// Open database
	db, err = sqlx.Connect("sqlite", dsn)
	if err != nil {
		err = fmt.Errorf("failed to open database: %v", err)
		return
	}

	// Adjust connection pool
	db.SetConnMaxLifetime(time.Minute)

	// Create transaction
	var tx *sqlx.Tx
	tx, err = db.Beginx()
	if err != nil {
		err = fmt.Errorf("failed to start transaction: %v", err)
		return
	}

	// If error ever happened, rollback and close database
	defer func() {
		if err != nil {
			if tx != nil {
				tx.Rollback()
			}

			if db != nil {
				db.Close()
			}

			db = nil
		}
	}()

	// Run DDL queries
	ddlQueries := []string{
		ddlCreateDocument,
		ddlCreateDocumentToken,
		ddlCreateDocumentTokenIndexToken}

	for _, query := range ddlQueries {
		_, err = tx.Exec(query)
		if err != nil {
			return
		}
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		err = fmt.Errorf("failed to commit transaction: %v", err)
		return
	}

	return
}

const ddlCreateDocument = `
CREATE TABLE IF NOT EXISTS document (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	identifier TEXT    NOT NULL UNIQUE,
	content    TEXT    NOT NULL,
	UNIQUE (identifier)
)`

const ddlCreateDocumentToken = `
CREATE TABLE IF NOT EXISTS document_token (
	document_id INTEGER NOT NULL,
	start       INTEGER NOT NULL,
	end         INTEGER NOT NULL,
	token       TEXT    NOT NULL,
	UNIQUE (document_id, start),
	CHECK (start <= end),
	FOREIGN KEY (document_id) REFERENCES document (id) ON DELETE CASCADE
)`

const ddlCreateDocumentTokenIndexToken = `
CREATE INDEX IF NOT EXISTS document_token_idx_token ON document_token (token)`
