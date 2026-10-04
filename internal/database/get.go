package database

import (
	"context"
	"database/sql"
	"slices"
	"sort"

	"github.com/jmoiron/sqlx"
)

// Document represent Document in database.
type Document struct {
	ID         int    `db:"id"`
	Identifier string `db:"identifier"`
	Type       string `db:"type"`
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

// DocumentWithTokensGroups represent Document and its matching tokens, grouped by
// query indexes.
type DocumentWithTokensGroups struct {
	DocumentID  int
	TokenGroups [][]DocumentToken
}

// GetDocuments fetch list of document based of its ids. It will be sorted according
// to the submitted ids order.
func GetDocuments(ctx context.Context, db *sqlx.DB, ids ...int) (docs map[int]Document, err error) {
	// Prepare query
	stmt, args, err := sqlx.In(`
		SELECT id, identifier, type, content
		FROM document
		WHERE id IN (?)`, ids)
	if err != nil {
		return
	}

	// Fetch list of documents from database
	var listDocs []Document
	err = db.SelectContext(ctx, &listDocs, stmt, args...)
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
func GetDocumentsByTokens(ctx context.Context, db *sqlx.DB, queryTokens []string, types ...string) (
	finalDocuments []DocumentWithTokensGroups,
	err error,
) {
	// Map each query tokens to its index. Notice here we use []int instead of int,
	// because we might get multiple identical token in one query.
	nQueryTokens := len(queryTokens)
	tokenQueryIndexes := make(map[string][]int)
	for i, token := range queryTokens {
		tokenQueryIndexes[token] = append(tokenQueryIndexes[token], i)
	}

	// Calculate minimal token per document to be considered a match.
	// For now we want at least a third of query is exist.
	minimalTokenHit := nQueryTokens / 3

	// Prepare query
	var inQuery string
	var inArgs []any
	if len(types) == 0 {
		inQuery = `
			WITH document_candidate AS (
				SELECT dt.document_id, COUNT(DISTINCT dt.token) AS hits
				FROM document_token dt
				WHERE 1
				AND dt.token IN (?)
				GROUP BY dt.document_id
				HAVING hits >= ?
				ORDER BY hits DESC
				LIMIT 50)
			SELECT dt.document_id, dt.start, dt.end, dt.token
			FROM document_token dt
			JOIN document_candidate dc ON dt.document_id = dc.document_id
			WHERE 1
			AND dt.token IN (?)
			ORDER BY dt.document_id, dt.start;`
		inArgs = []any{queryTokens, minimalTokenHit, queryTokens}
	} else {
		inQuery = `
			WITH document_candidate AS (
				SELECT dt.document_id, COUNT(DISTINCT dt.token) AS hits
				FROM document_token dt
				JOIN document d ON dt.document_id = d.id
				WHERE 1
				AND dt.token IN (?)
				AND d.type IN (?)
				GROUP BY dt.document_id
				HAVING hits >= ?
				ORDER BY hits DESC
				LIMIT 50)
			SELECT dt.document_id, dt.start, dt.end, dt.token
			FROM document_token dt
			JOIN document_candidate dc ON dt.document_id = dc.document_id
			WHERE 1
			AND dt.token IN (?)
			ORDER BY dt.document_id, dt.start`
		inArgs = []any{queryTokens, types, minimalTokenHit, queryTokens}
	}

	// Fetch list of document tokens from database
	stmt, args, err := sqlx.In(inQuery, inArgs...)
	if err != nil {
		return
	}

	var documentTokens []DocumentToken
	err = db.SelectContext(ctx, &documentTokens, stmt, args...)
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

	// Final step
	// For every document with matched tokens, group the token by its query index.
	finalDocuments = make([]DocumentWithTokensGroups, 0, len(documentsWithMatchedTokens))
	for _, dwmt := range documentsWithMatchedTokens {
		tokenGroups := groupTokensByQueryIndex(dwmt.Tokens, 9, nQueryTokens/3)

		finalDocuments = append(finalDocuments, DocumentWithTokensGroups{
			DocumentID:  dwmt.DocumentID,
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

func groupTokensByQueryIndex(documentTokens []DocumentToken, maxDrift, minSize int) [][]DocumentToken {
	// Prepare result
	var tokenGroups [][]DocumentToken

	// Process tokens one by one
	for _, token := range documentTokens {
		// best is the index of the cluster that we think this token belongs to.
		// -1 means "we haven't found a suitable cluster yet".
		//
		// bestDrift stores the drift of that selected cluster.
		best, bestDrift := -1, 0

		// Check the new match against every existing group.
		for i, tokenGroup := range tokenGroups {
			// Look at the last token in this group
			lastToken := tokenGroup[len(tokenGroup)-1]

			// How far did we move in the document?
			dDoc := token.Start - lastToken.Start

			// How far did we move in the query?
			dQuery := token.IndexInQuery - lastToken.IndexInQuery

			// The new token must move forward in query
			if dQuery <= 0 {
				continue
			}

			// Compare how much we moved in the doc with how much we in the query.
			//
			// If both moved by the same amount, e.g. dDoc = 5 and dQuery = 5,
			// then drift = 0, which is a perfect continuation of the same diagonal.
			//
			// If dDoc = 7 and dQuery = 5, then drift = 2.
			drift := abs(dDoc - dQuery)

			// If the difference is too large, this token does not belong to this group.
			if drift > maxDrift {
				continue
			}

			// We found a group that this token COULD belong to. But there might be
			// several eligible groups. The algorithm chooses:
			// 1. The group with MORE tokens.
			// 2. If they have the same size, the group with SMALLER drift.
			if best == -1 ||
				len(tokenGroup) > len(tokenGroups[best]) ||
				(len(tokenGroup) == len(tokenGroups[best]) && drift < bestDrift) {

				best = i
				bestDrift = drift
			}
		}

		// Check if we find a suitable existing cluster. If no (best == -1) then
		// start a brand-new cluster containing only this token.
		if best == -1 {
			tokenGroups = append(tokenGroups, []DocumentToken{token})
		} else {
			tokenGroups[best] = append(tokenGroups[best], token)
		}
	}

	// Remove token groups that too small
	tokenGroups = slices.DeleteFunc(tokenGroups, func(tg []DocumentToken) bool {
		return len(tg) < minSize
	})

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

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
