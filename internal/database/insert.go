package database

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// maxSQLiteVariables is how many variables allowed in single query. Here we use
// 900, a bit below SQLite 3.32.0 limit (999).
const maxSQLiteVariables = 900

// InsertArg is argument for inserting Document.
type InsertDocumentArg struct {
	Identifier string
	Type       string
	Content    string
	Tokens     []DocumentToken
}

// InsertDocuments save the documents into the database.
func InsertDocuments(db *sqlx.DB, args []InsertDocumentArg) (err error) {
	// If there are no args submitted, stop early
	if len(args) == 0 {
		return nil
	}

	// Start transaction
	tx, err := db.Beginx()
	if err != nil {
		err = fmt.Errorf("failed to start transaction: %v", err)
		return
	}

	// Make sure to rollback if error ever happened
	defer func() {
		if err != nil && tx != nil {
			tx.Rollback()
		}
	}()

	// For each batch, we do 300 documents. This is because each document uses
	// 3 variables, and our limit is 900.
	docBatchSize := maxSQLiteVariables / 3

	// Exec each batch
	for start := 0; start < len(args); start += docBatchSize {
		end := min(start+docBatchSize, len(args))
		if err = insertDocumentBatch(tx, args[start:end]); err != nil {
			return
		}
	}

	// Commit to database
	err = tx.Commit()
	return
}

// insertDocumentBatch upserts one batch of documents and their tokens.
func insertDocumentBatch(tx *sqlx.Tx, args []InsertDocumentArg) error {
	docIDs, err := upsertDocuments(tx, args)
	if err != nil {
		return fmt.Errorf("failed to upsert documents: %w", err)
	}

	if err := deleteDocumentTokens(tx, docIDs); err != nil {
		return fmt.Errorf("failed to delete document tokens: %w", err)
	}

	if err := insertDocumentTokens(tx, args, docIDs); err != nil {
		return fmt.Errorf("failed to insert document tokens: %w", err)
	}

	return nil
}

// upsertDocuments inserts/updates all documents in the batch with a single multi-row
// statement and returns their ids, keyed by identifier+type, via RETURNING.
func upsertDocuments(tx *sqlx.Tx, args []InsertDocumentArg) (map[string]int, error) {
	// Prepare query
	placeholders := make([]string, 0, len(args))
	values := make([]any, 0, len(args)*3)
	for _, arg := range args {
		placeholders = append(placeholders, "(?, ?, ?)")
		values = append(values, arg.Identifier, arg.Type, arg.Content)
	}

	query := fmt.Sprintf(`
		INSERT INTO document (identifier, type, content)
		VALUES %s
		ON CONFLICT (identifier, type) DO UPDATE
		SET content = excluded.content
		RETURNING id, identifier, type`,
		strings.Join(placeholders, ", "))

	// Run the query
	var upsertedDocuments []Document
	err := tx.Select(&upsertedDocuments, query, values...)
	if err != nil {
		return nil, err
	}

	// Map document identifier+type to id
	docIDs := make(map[string]int, len(args))
	for _, ud := range upsertedDocuments {
		docIDs[ud.Identifier+"\x00"+ud.Type] = ud.ID
	}

	return docIDs, nil
}

// deleteDocumentTokens removes existing tokens for every document in the
// batch with a single statement.
func deleteDocumentTokens(tx *sqlx.Tx, docIDs map[string]int) error {
	// Make sure it's not empty
	if len(docIDs) == 0 {
		return nil
	}

	// Get the document ids from map
	ids := make([]int, 0, len(docIDs))
	for _, id := range docIDs {
		ids = append(ids, id)
	}

	// Prepare query
	query, args, err := sqlx.In(`
		DELETE FROM document_token 
		WHERE document_id IN (?)`, ids)
	if err != nil {
		return err
	}

	// Exec delete
	_, err = tx.Exec(query, args...)
	return err
}

// insertDocumentTokens bulk-inserts all tokens for the batch, chunking into multiple
// statements only when needed to stay under the SQL variable limit (token counts per
// document are not known beforehand, unlike the fixed 3-column document rows).
func insertDocumentTokens(tx *sqlx.Tx, args []InsertDocumentArg, docIDs map[string]int) error {
	// How many args can be inserted per chunk?
	const paramsPerRow = 4
	rowsPerChunk := maxSQLiteVariables / paramsPerRow

	// Prepare variable for query's chunk
	placeholders := make([]string, 0, rowsPerChunk)
	values := make([]interface{}, 0, rowsPerChunk*paramsPerRow)

	// Prepare helper function to exec
	execInsert := func() error {
		// Make sure args not empty
		if len(placeholders) == 0 {
			return nil
		}

		// Prepare query
		query := fmt.Sprintf(`
			INSERT INTO document_token (document_id, start, end, token)
			VALUES %s
			ON CONFLICT DO NOTHING`,
			strings.Join(placeholders, ", "))

		// Exec the insert query
		if _, err := tx.Exec(query, values...); err != nil {
			return err
		}

		// Reset slice
		placeholders = placeholders[:0]
		values = values[:0]
		return nil
	}

	// Process each args
	for _, a := range args {
		// Get the document id via identifier+type
		docID := docIDs[a.Identifier+"\x00"+a.Type]

		for _, t := range a.Tokens {
			// Apply each token to placeholder
			placeholders = append(placeholders, "(?, ?, ?, ?)")
			values = append(values, docID, t.Start, t.End, t.Token)

			// If we reach chunk limit, exec the query
			if len(placeholders) == rowsPerChunk {
				if err := execInsert(); err != nil {
					return err
				}
			}
		}
	}

	// Insert the leftover
	return execInsert()
}
