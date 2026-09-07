package database

import (
	"database/sql"
	"fmt"

	"github.com/RadhiFadlillah/typosearch/internal/tokenizer"
	"github.com/jmoiron/sqlx"
	"golang.org/x/text/unicode/norm"
)

// InsertArg is argument for inserting Document.
type InsertDocumentArg struct {
	Identifier string `db:"identifier"`
	Content    string `db:"content"`
}

// InsertDocuments save the documents into the database.
func InsertDocuments(
	db *sqlx.DB,
	splitter func([]rune) [][]rune,
	processor func(r rune) []rune,
	args []InsertDocumentArg,
) (err error) {
	// If there are no args submitted, stop early
	if len(args) == 0 {
		return nil
	}

	// Remove index, and create it once it over
	_, err = db.Exec(`DROP INDEX IF EXISTS document_token_idx_covering`)
	if err != nil {
		return
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

		// Recreate index
		if err == nil {
			_, err = db.Exec(ddlCreateDocumentTokenIndexToken)
		}
	}()

	// Prepare statement
	stmtGetDoc, err := tx.Preparex(`
		SELECT id FROM document
		WHERE identifier = ?`)
	if err != nil {
		return
	}

	stmtInsertDoc, err := tx.Preparex(`
		INSERT INTO document (identifier, content)
		VALUES (?, ?)
		ON CONFLICT (identifier) DO UPDATE
		SET content = excluded.content`)
	if err != nil {
		return
	}

	stmtDeleteDocToken, err := tx.Preparex(`
		DELETE FROM document_token
		WHERE document_id = ?`)
	if err != nil {
		return
	}

	stmtInsertDocToken, err := tx.Preparex(`
		INSERT INTO document_token (document_id, start, end, token)
		VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`)
	if err != nil {
		return
	}

	// Insert the document
	for _, arg := range args {
		// Get document ID if it's exist
		var documentID int64
		documentExist := true
		err = stmtGetDoc.Get(&documentID, arg.Identifier)
		if err != nil {
			if err == sql.ErrNoRows {
				documentExist = false
				err = nil
			} else {
				return
			}
		}

		// Normalize and decompose document's content.
		// We store the NFD version of content so it can be transformed by processor.
		nfdContent := norm.NFD.String(arg.Content)

		// Save document
		var res sql.Result
		res, err = stmtInsertDoc.Exec(
			arg.Identifier,
			nfdContent)
		if err != nil {
			return
		}

		// If document not exist, use ID from last inserted
		if !documentExist {
			documentID, err = res.LastInsertId()
			if err != nil {
				return
			}
		}

		// Remove any token that associated with this document
		_, err = stmtDeleteDocToken.Exec(documentID)
		if err != nil {
			return
		}

		// Save tokens
		tokens, _ := tokenizer.Tokenize(nfdContent, splitter, processor)
		for _, token := range tokens {
			text := token.String()
			start, end := token.Range()

			_, err = stmtInsertDocToken.Exec(
				documentID,
				start, end,
				text)
			if err != nil {
				return
			}
		}
	}

	// Commit to database
	err = tx.Commit()
	return
}
