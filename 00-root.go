package typosearch

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/RadhiFadlillah/typosearch/internal/database"
	"github.com/RadhiFadlillah/typosearch/internal/tokenizer"
	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite"
)

// Document is the text document that will be indexed to be later searched on.
type Document struct {
	// ID is the unique identifier for this Document.
	ID string
	// Content is the text body of this Document.
	Content string
}

// MatchedDocument is the document that matched with the search query.
type MatchedDocument struct {
	Document
	// Score is confidence level for this [Document].
	Score float64
	// Positions is list of position of matched keyword inside Document. Represented
	// as 2-tuple of `[start, end]`.
	Positions [][2]int
	// dbID is private field to represent Document IDs (not identifier) in database
	dbID int
}

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db         *sqlx.DB
	processors []func(rune) []rune
	threshold  float64
}

// Open the search storage in the specified path.
func OpenStorage(path string) (*Storage, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}

	return &Storage{db: db}, nil
}

// Apply one or more [Processor] function to the [Storage]. These processors later
// will be used on the submitted [Document] and on search queries. These processors
// are not saved inside Storage, so make sure to re-apply it whenever you open the
// storage.
func (s *Storage) ApplyProcessors(processors ...Processor) *Storage {
	s.processors = make([]func(rune) []rune, len(processors))
	for i, p := range processors {
		s.processors[i] = p
	}
	return s
}

// Apply confidence threshold for this [Storage].
func (s *Storage) ApplyThreshold(score float64) *Storage {
	s.threshold = score
	return s
}

// Close closes the underlying database for the search storage.
func (s *Storage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// AddDocuments save and index the documents into the storage. If the document with
// matching ID already exist, it will be updated.
func (s *Storage) AddDocuments(docs ...Document) error {
	// Cast Document to insert arg
	dbDocs := make([]database.InsertDocumentArg, len(docs))
	for i, doc := range docs {
		dbDocs[i] = database.InsertDocumentArg{
			Identifier: doc.ID,
			Content:    doc.Content,
		}
	}

	return database.InsertDocuments(s.db, s.processors, dbDocs)
}

// DeleteDocuments remove the documents in the storage.
func (st *Storage) DeleteDocuments(ids ...string) error {
	return database.DeleteDocuments(st.db, ids...)
}

// Search the storage for suitable documents.
func (s *Storage) Search(query string) ([]MatchedDocument, error) {
	// Clear up spaces from query
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, nil
	}

	// Convert the query into tokens
	queryTokens := tokenizer.Tokenize(query, s.processors...)

	// Convert tokens into strings
	tokenStrings := make([]string, len(queryTokens))
	for i, token := range queryTokens {
		tokenStrings[i] = token.String()
	}

	// Fetch list of matching document tokens from database
	nQueryToken := len(tokenStrings)
	documents, err := database.GetDocumentTokens(s.db, tokenStrings...)
	if err != nil {
		return nil, err
	}

	if len(documents) == 0 {
		return nil, nil
	}

	// Prepare default confidence threshold
	scoreThreshold := s.threshold
	if scoreThreshold <= 0 || scoreThreshold > 1 {
		scoreThreshold = 0.5
	}

	// Process each document
	type ScoredTokens struct {
		Tokens []database.DocumentToken
		Score  float64
	}

	searchResults := make([]MatchedDocument, 0, len(documents))
	for _, doc := range documents {
		// Group tokens by checking its index in query. For example, we have this
		// bigram token (top is token text, bottom is index of that token in query)
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

		// Initiate variables and helper function
		var tokenGroups []ScoredTokens
		var currentGroup []database.DocumentToken
		currentLastQueryIndex := nQueryToken

		saveCurrentGroup := func() {
			// Make sure current group not empty
			groupSize := len(currentGroup)
			if groupSize == 0 {
				return
			}

			// Calc score of this group
			positions := make([]int, groupSize)
			for i := range positions {
				positions[i] = currentGroup[i].Start
			}

			compactness := calcCompactness(positions)
			completeness := calcCompleteness(groupSize, nQueryToken)
			score := compactness * completeness

			// If the score is above threshold, save it
			if score >= scoreThreshold {
				tokenGroups = append(tokenGroups, ScoredTokens{
					Tokens: currentGroup,
					Score:  score,
				})
			}
		}

		// Process each token in this document
		for _, token := range doc.Tokens {
			if token.IndexInQuery <= currentLastQueryIndex {
				saveCurrentGroup()
				currentGroup = make([]database.DocumentToken, 0)
			}

			currentLastQueryIndex = token.IndexInQuery
			currentGroup = append(currentGroup, token)
		}

		// Save any trailing group
		saveCurrentGroup()

		// If there are no match, continue to next document
		nTokenGroup := len(tokenGroups)
		if nTokenGroup == 0 {
			continue
		}

		// Sort the token groups by best score
		sort.Slice(tokenGroups, func(i, j int) bool {
			if tokenGroups[i].Score != tokenGroups[j].Score {
				return tokenGroups[i].Score > tokenGroups[j].Score
			}
			return len(tokenGroups[i].Tokens) > len(tokenGroups[j].Tokens)
		})

		// Create combined score using formula:
		// score = top_score + (1 - top_score) * leftover_scores * alpha
		//
		// - top_score is the base, we just want the leftover to reward the top_score
		//   so the leftover are not useless
		// - (1 - top_score) is how much can we add to the base score
		// - leftover_scores is a normalized weighted sum of the leftover
		// - alpha is how much the leftover_scores affect the top_score
		combinedScore := tokenGroups[0].Score

		if nTokenGroup > 1 {
			alpha := 0.3
			decay := 0.5
			topScore := tokenGroups[0].Score

			var weightedSum, sumOfWeight float64
			for i := 1; i < len(tokenGroups); i++ {
				score := tokenGroups[i].Score
				weight := math.Pow(decay, float64(i-1))
				weightedSum += score * weight
				sumOfWeight += weight
			}

			leftoverScores := weightedSum / sumOfWeight
			combinedScore = topScore + (1-topScore)*leftoverScores*alpha
		}

		// Convert token groups into positions
		positions := make([][2]int, len(tokenGroups))
		for i := range tokenGroups {
			tokens := tokenGroups[i].Tokens
			start := tokens[0].Start
			end := tokens[len(tokens)-1].End
			positions[i] = [2]int{start, end}
		}

		// Save the result
		searchResults = append(searchResults, MatchedDocument{
			Score:     combinedScore,
			Positions: positions,
			dbID:      doc.DocumentID,
		})
	}

	// If there are no search results, stop
	if len(searchResults) == 0 {
		return nil, nil
	}

	// Fetch content for search result
	dbIDs := make([]int, len(searchResults))
	for i, sr := range searchResults {
		dbIDs[i] = sr.dbID
	}

	dbDocs, err := database.GetDocuments(s.db, dbIDs...)
	if err != nil {
		return nil, err
	}

	// Apply content to search result
	finalResults := make([]MatchedDocument, 0, len(searchResults))
	for _, sr := range searchResults {
		idx, found := slices.BinarySearchFunc(dbDocs, sr.dbID, func(doc database.Document, id int) int {
			switch {
			case doc.ID == id:
				return 0
			case doc.ID < id:
				return -1
			default:
				return 1
			}
		})

		if found {
			dbDoc := dbDocs[idx]
			sr.Document = Document{
				ID:      dbDoc.Identifier,
				Content: dbDoc.Content,
			}

			finalResults = append(finalResults, sr)
		}
	}

	// Sort the search result
	sort.Slice(finalResults, func(i, j int) bool {
		// By best score
		fr1 := finalResults[i]
		fr2 := finalResults[j]
		if fr1.Score != fr2.Score {
			return fr1.Score > fr2.Score
		}

		// By most group counts
		n1 := len(fr1.Positions)
		n2 := len(fr2.Positions)
		if n1 != n2 {
			return n1 > n2
		}

		return fr1.ID < fr2.ID
	})

	return finalResults, nil
}

func calcCompleteness(currentCount, expectedCount int) float64 {
	// Penalize when completeness is too small.
	// Use formula 3s^2 - 2s^3 so score < 0.5 is penalized smoothly.
	score := float64(currentCount) / float64(expectedCount)
	return 3*math.Pow(score, 2) - 2*math.Pow(score, 3)
}

func calcCompactness(positions []int) float64 {
	// Handle edge cases: empty positions or single element
	// Single elements have no gaps, so they're perfectly compact
	nPosition := len(positions)
	if nPosition <= 1 {
		return 1.0
	}

	// Calculate gaps average
	var gapSum int
	nGap := nPosition - 1
	for i := range nGap {
		gapSum += positions[i+1] - positions[i]
	}

	// Calculate mean of gaps
	mean := float64(gapSum) / float64(nGap)
	if mean == 0 {
		return 1
	}

	// Calculate compactness by comparing the mean with ideal gap value. Ideally,
	// gap between token position is at most 3 (since we use trigram).
	return min(1, 3.0/mean)
}
