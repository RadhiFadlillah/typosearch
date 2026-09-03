package database

import (
	"database/sql"
	"sort"

	"github.com/jmoiron/sqlx"
)

// Document represent Document in database.
type Document struct {
	ID         int    `db:"id"`
	Identifier string `db:"identifier"`
	Content    string `db:"content"`
}

// MatchedDocument represent list of Document that matched token search.
type MatchedDocument struct {
	DocumentID int
	Tokens     []DocumentToken
}

// DocumentToken is object representing table DocumentToken. There is an additional
// field that not exist in database, for tracking token from query.
type DocumentToken struct {
	DocumentID   int    `db:"document_id"`
	Start        int    `db:"start"`
	End          int    `db:"end"`
	Token        string `db:"token"`
	IndexInQuery int
}

// GetDocuments fetch list of document based of its ids. It will be sorted according
// to the submitted ids order.
func GetDocuments(db *sqlx.DB, ids ...int) (docs []Document, err error) {
	// Prepare query
	stmt, args, err := sqlx.In(`
		SELECT id, identifier, content
		FROM document
		WHERE id IN (?)
		ORDER BY id ASC`, ids)
	if err != nil {
		return
	}

	// Fetch list of documents from database
	err = db.Select(&docs, stmt, args...)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	if len(docs) == 0 {
		return
	}

	return
}

// GetDocumentTokens fetch list of DocumentToken based on the specified tokens.
func GetDocumentTokens(db *sqlx.DB, queryTokens ...string) (matchedDocuments []MatchedDocument, err error) {
	// Prepare query
	stmt, args, err := sqlx.In(`
		SELECT document_id, start, end, token
		FROM document_token
		WHERE token IN (?)
		ORDER BY document_id, start`, queryTokens)
	if err != nil {
		return
	}

	// Fetch list of tokens from database
	var matchedTokens []DocumentToken
	err = db.Select(&matchedTokens, stmt, args...)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	// If there are no match, stop early
	if len(matchedTokens) == 0 {
		return
	}

	// Map each query tokens to its index.
	// Notice here we use []int instead of int, because we might get multiple
	// identical token in one query.
	mapTokenQueryIndexes := make(map[string][]int)
	for i, token := range queryTokens {
		mapTokenQueryIndexes[token] = append(mapTokenQueryIndexes[token], i)
	}

	// Check how many Document found, and how many token found per document.
	// Will be used later for allocating slice.
	mapDocumentTokenCount := make(map[int]int)
	for _, match := range matchedTokens {
		mapDocumentTokenCount[match.DocumentID]++
	}

	// Group the tokens by Document
	matchedDocuments = make([]MatchedDocument, 0, len(mapDocumentTokenCount))

	currentDocID := -1
	currentDocLastQueryIndex := len(queryTokens)
	var currentDocTokenCount int
	var currentDocument MatchedDocument

	for _, token := range matchedTokens {
		if token.DocumentID != currentDocID {
			// If document is changed, save current to list
			if len(currentDocument.Tokens) > 0 {
				matchedDocuments = append(matchedDocuments, currentDocument)
			}

			// Reset current document
			currentDocID = token.DocumentID
			currentDocTokenCount = mapDocumentTokenCount[currentDocID]
			currentDocLastQueryIndex = len(queryTokens)

			currentDocument = MatchedDocument{
				DocumentID: currentDocID,
				Tokens:     make([]DocumentToken, 0, currentDocTokenCount),
			}
		}

		// Save the token to document, while applying index from query
		currentDocLastQueryIndex = firstLarger(
			currentDocLastQueryIndex,
			mapTokenQueryIndexes[token.Token])

		token.IndexInQuery = currentDocLastQueryIndex
		currentDocument.Tokens = append(currentDocument.Tokens, token)
	}

	// Save the trailing documents
	if len(currentDocument.Tokens) > 0 {
		matchedDocuments = append(matchedDocuments, currentDocument)
	}

	return
}

func firstLarger(a int, numbers []int) int {
	i := sort.Search(len(numbers), func(i int) bool {
		return numbers[i] > a
	})

	if i == len(numbers) {
		return numbers[0]
	}

	return numbers[i]
}
