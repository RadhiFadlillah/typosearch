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

// DocumentToken is object representing table DocumentToken. There is an additional
// field that not exist in database, for tracking token from query.
type DocumentToken struct {
	DocumentID int    `db:"document_id"`
	Start      int    `db:"start"`
	End        int    `db:"end"`
	Token      string `db:"token"`

	IndexInQuery int
}

// DocumentWithTokens represent Document and its matching tokens.
type DocumentWithTokens struct {
	DocumentID int
	Tokens     []DocumentToken
}

// MatchedDocument represent list of Document that matched token search.
type MatchedDocument struct {
	Document
	TokenGroups [][]DocumentToken
}

// GetDocuments fetch list of document based of its ids. It will be sorted according
// to the submitted ids order.
func GetDocuments(db *sqlx.DB, ids ...int) (docs map[int]Document, err error) {
	// Prepare query
	stmt, args, err := sqlx.In(`
		SELECT id, identifier, content
		FROM document
		WHERE id IN (?)`, ids)
	if err != nil {
		return
	}

	// Fetch list of documents from database
	var listDocs []Document
	err = db.Select(&listDocs, stmt, args...)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	// Convert list to map
	docs = make(map[int]Document)
	for _, doc := range listDocs {
		docs[doc.ID] = doc
	}

	return
}

// GetDocumentsByTokens fetch list of Documents based on the specified tokens.
func GetDocumentsByTokens(db *sqlx.DB, queryTokens ...string) (
	finalDocuments []MatchedDocument,
	err error,
) {
	// Map each query tokens to its index. Notice here we use []int instead of int,
	// because we might get multiple identical token in one query.
	nQueryTokens := len(queryTokens)
	tokenQueryIndexes := make(map[string][]int)
	for i, token := range queryTokens {
		tokenQueryIndexes[token] = append(tokenQueryIndexes[token], i)
	}

	// Fetch list of document tokens from database
	stmt, args, err := sqlx.In(`
		SELECT document_id, start, end, token
		FROM document_token
		WHERE token IN (?)
		ORDER BY document_id, start`, queryTokens)
	if err != nil {
		return
	}

	var documentTokens []DocumentToken
	err = db.Select(&documentTokens, stmt, args...)
	if err != nil && err != sql.ErrNoRows {
		return
	}

	if len(documentTokens) == 0 {
		return
	}

	// Group the document tokens by document id.
	// While on it, also saves the token index in query.
	documentsWithMatchedTokens := groupTokensByDocument(
		documentTokens,
		nQueryTokens,
		tokenQueryIndexes,
	)

	// Fetch the content for those documents
	documentIDs := make([]int, len(documentsWithMatchedTokens))
	for i, d := range documentsWithMatchedTokens {
		documentIDs[i] = d.DocumentID
	}

	documentContents, err := GetDocuments(db, documentIDs...)
	if err != nil {
		return nil, err
	}

	// Final step
	// For every document with matched tokens, apply content to it, and group
	// the token by its query index.
	finalDocuments = make([]MatchedDocument, 0, len(documentsWithMatchedTokens))
	for _, dwmt := range documentsWithMatchedTokens {
		doc, exist := documentContents[dwmt.DocumentID]
		if !exist {
			continue
		}

		tokenGroups := groupTokensByQueryIndex(dwmt.Tokens, nQueryTokens)
		finalDocuments = append(finalDocuments, MatchedDocument{
			Document:    doc,
			TokenGroups: tokenGroups,
		})
	}

	return
}

// Group the document tokens by document id. While on it, also saves the token
// index from query.
func groupTokensByDocument(
	documentTokens []DocumentToken,
	nQueryTokens int,
	tokenQueryIndexes map[string][]int,
) []DocumentWithTokens {
	// Check how many Documents are there, and how many tokens per document.
	// Will be used later for allocating slice.
	tokensPerDocument := make(map[int]int)
	for _, token := range documentTokens {
		tokensPerDocument[token.DocumentID]++
	}

	// Prepare variable to store result
	documents := make([]DocumentWithTokens, 0, len(tokensPerDocument))

	currentDocID := -1
	currentDocLastQueryIndex := nQueryTokens
	var currentDocument DocumentWithTokens

	for _, token := range documentTokens {
		// If document is changed, save current to list
		if token.DocumentID != currentDocID {
			if len(currentDocument.Tokens) > 0 {
				documents = append(documents, currentDocument)
			}

			// Reset current document
			currentDocID = token.DocumentID
			currentDocLastQueryIndex = nQueryTokens

			nTokensInDocument := tokensPerDocument[currentDocID]
			currentDocument = DocumentWithTokens{
				DocumentID: currentDocID,
				Tokens:     make([]DocumentToken, 0, nTokensInDocument),
			}
		}

		// Save the token to document, while applying query's token index
		currentDocLastQueryIndex = firstLarger(
			currentDocLastQueryIndex,
			tokenQueryIndexes[token.Token],
		)

		token.IndexInQuery = currentDocLastQueryIndex
		currentDocument.Tokens = append(currentDocument.Tokens, token)
	}

	// Save the trailing documents
	if len(currentDocument.Tokens) > 0 {
		documents = append(documents, currentDocument)
	}

	return documents
}

// For each document, group its tokens by token's query index. For example, let's
// say we have bigram tokens like this (top is token text, bottom is index of
// that token in the query)
//
// ["at", "la", "ka", "at", "ma", "ma", "al", "la", "ka", "at", "ak", "ak"]
// [   5,    2,    4,    5,    0,    0,    1,    2,    4,    5,    3,    3]
//
// The expected groups are:
//
// 0: ["at"]
// 1: ["la", "ka", "at"]
// 2: ["ma"]
// 3: ["ma", "al", "la", "ka", "at"]
// 4: ["ak"]
// 5: ["ak"]
func groupTokensByQueryIndex(
	documentTokens []DocumentToken,
	nQueryTokens int,
) [][]DocumentToken {
	var tokenGroups [][]DocumentToken

	maxLength := len(documentTokens)
	var currentGroup []DocumentToken
	currentLastQueryIndex := nQueryTokens

	for _, token := range documentTokens {
		// If query index smaller than the last, save current group to list
		if token.IndexInQuery <= currentLastQueryIndex {
			if len(currentGroup) > 0 {
				tokenGroups = append(tokenGroups, currentGroup)
			}
			currentGroup = make([]DocumentToken, 0, maxLength)
		}

		// Save the token to current group
		currentLastQueryIndex = token.IndexInQuery
		currentGroup = append(currentGroup, token)
	}

	// Save the trailing group
	if len(currentGroup) > 0 {
		tokenGroups = append(tokenGroups, currentGroup)
	}

	return tokenGroups
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
