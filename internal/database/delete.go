package database

import (
	"context"

	"github.com/jmoiron/sqlx"
)

// DeleteDocuments remove documents in the database.
func DeleteDocuments(ctx context.Context, db *sqlx.DB, identifiers ...string) (err error) {
	// If there are no identifiers submitted, stop early
	if len(identifiers) == 0 {
		return nil
	}

	// Prepare query
	stmt, args, err := sqlx.In(`
		DELETE FROM document
		WHERE identifier IN (?)`, identifiers)
	if err != nil {
		return
	}

	// Execute query
	_, err = db.ExecContext(ctx, stmt, args...)
	if err != nil {
		return
	}

	return
}
