package typosearch

import (
	"math"
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
	// Markers is list of rune-based marker position inside Document. Represented
	// as 2-tuple of `[start, end]`.
	Markers [][2]int
	// WordMarkers is list of word-based marker position inside Document. Represented
	// as 2-tuple of `[start, end]`.
	WordMarkers [][2]int
	// dbID is private field to represent Document IDs (not identifier) in database
	dbID int
}

// _ScoredTokenGroup is internal object to track score for a token group.
type _ScoredTokenGroup struct {
	Tokens []database.DocumentToken
	Score  float64
}

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db        *sqlx.DB
	processor func(rune) []rune
	threshold float64
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
func (s *Storage) ApplyProcessor(processor Processor) *Storage {
	s.processor = processor
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

	return database.InsertDocuments(s.db, s.processor, dbDocs)
}

// DeleteDocuments remove the documents in the storage.
func (st *Storage) DeleteDocuments(ids ...string) error {
	return database.DeleteDocuments(st.db, ids...)
}

// Search the storage for suitable documents. The returned documents will have its
// content normalized in NFD format. If users need NFC, they need to normalize it
// themselves using norm.NFC.
func (s *Storage) Search(query string) ([]MatchedDocument, error) {
	// Clear up spaces from query
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, nil
	}

	// Convert the query into tokens
	queryTokens := tokenizer.Tokenize(query, s.processor)

	// Convert tokens into strings
	tokenStrings := make([]string, len(queryTokens))
	for i, token := range queryTokens {
		tokenStrings[i] = token.String()
	}

	// Fetch list of matching documents from database
	nQueryToken := len(tokenStrings)
	documents, err := database.GetDocumentsByTokens(s.db, tokenStrings...)
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

	// Create scores for each document
	searchResults := make([]MatchedDocument, 0, len(documents))

	for _, doc := range documents {
		// Score each token group
		tokenGroups := make([]_ScoredTokenGroup, 0, len(doc.TokenGroups))
		for _, tg := range doc.TokenGroups {
			compactness := calcCompactness(tg)
			completeness := calcCompleteness(len(tg), nQueryToken)
			score := compactness * completeness

			// If the score is good enough, save it
			if score >= 0.5 {
				tokenGroups = append(tokenGroups, _ScoredTokenGroup{
					Tokens: tg,
					Score:  score,
				})
			}
		}

		// If there are no good enough group, continue to next document
		if len(tokenGroups) == 0 {
			continue
		}

		// Sort the token groups by the best score
		sort.Slice(tokenGroups, func(i, j int) bool {
			if tokenGroups[i].Score != tokenGroups[j].Score {
				return tokenGroups[i].Score > tokenGroups[j].Score
			}
			return len(tokenGroups[i].Tokens) > len(tokenGroups[j].Tokens)
		})

		// Calc combined score and check if it pass
		combinedScore := calcCombinedScore(tokenGroups)
		if combinedScore <= scoreThreshold {
			continue
		}

		// Convert token groups into marker positions
		markers := make([][2]int, len(tokenGroups))
		for i := range tokenGroups {
			tokens := tokenGroups[i].Tokens
			start := tokens[0].Start
			end := tokens[len(tokens)-1].End
			markers[i] = [2]int{start, end}
		}

		// Convert rune-based markers into word-based markers
		wordMarkers := snapMarkersToWordBoundaries([]rune(doc.Content), markers)

		// Save the result
		searchResults = append(searchResults, MatchedDocument{
			ID:          doc.Identifier,
			Content:     doc.Content,
			Score:       combinedScore,
			Markers:     markers,
			WordMarkers: wordMarkers,
		})
	}

	// If there are no search results, stop
	if len(searchResults) == 0 {
		return nil, nil
	}

	// Sort the search result
	sort.Slice(searchResults, func(i, j int) bool {
		// By best score
		fr1 := searchResults[i]
		fr2 := searchResults[j]
		if fr1.Score != fr2.Score {
			return fr1.Score > fr2.Score
		}

		// By most group counts
		n1 := len(fr1.Markers)
		n2 := len(fr2.Markers)
		if n1 != n2 {
			return n1 > n2
		}

		return fr1.ID < fr2.ID
	})

	return searchResults, nil
}

func calcCompleteness(currentCount, expectedCount int) float64 {
	// Penalize when completeness is too small.
	// Use formula 3s^2 - 2s^3 so score < 0.5 is penalized smoothly.
	score := float64(currentCount) / float64(expectedCount)
	return 3*math.Pow(score, 2) - 2*math.Pow(score, 3)
}

func calcCompactness(documentTokens []database.DocumentToken) float64 {
	// Handle edge cases: empty positions or single element
	// Single elements have no gaps, so they're perfectly compact
	nTokens := len(documentTokens)
	if nTokens <= 1 {
		return 1.0
	}

	// Get position from this tokens
	positions := make([]int, len(documentTokens))
	for i := range positions {
		positions[i] = documentTokens[i].Start
	}

	// Calculate sum of gaps
	var gapSum int
	nGap := nTokens - 1
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

// Create combined score using formula:
// score = top_score + (1 - top_score) * leftover_scores * alpha
//
//   - top_score is the base, we just want the leftover to reward the top_score
//     so the leftover are not useless
//   - (1 - top_score) is how much can we add to the base score
//   - leftover_scores is a normalized weighted sum of the leftover
//   - alpha is how much the leftover_scores affect the top_score
func calcCombinedScore(tokenGroups []_ScoredTokenGroup) float64 {
	combinedScore := tokenGroups[0].Score

	if len(tokenGroups) > 1 {
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

	return combinedScore
}
