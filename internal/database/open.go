package database

import (
	_ "embed"
	"fmt"
	"net/url"
	"time"

	"github.com/jmoiron/sqlx"
)

//go:embed ddl.sql
var ddlQueries string

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
	_, err = tx.Exec(ddlQueries)
	if err != nil {
		err = fmt.Errorf("failed to exec ddl: %v", err)
		return
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		err = fmt.Errorf("failed to commit transaction: %v", err)
		return
	}

	return
}
