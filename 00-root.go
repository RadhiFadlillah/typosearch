package typosearch

import (
	"math"
	"sort"
	"strings"

	"github.com/RadhiFadlillah/typosearch/internal/database"
	"github.com/RadhiFadlillah/typosearch/internal/tokenizer"
	"github.com/jmoiron/sqlx"
	"github.com/xrash/smetrics"

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
	// Markers is list of rune-based marker position inside Document. Represented
	// as 2-tuple of `[start, end]`.
	Markers [][2]int
	// WordMarkers is list of word-based marker position inside Document. Represented
	// as 2-tuple of `[start, end]`.
	WordMarkers [][2]int
	// Score is confidence level for this [Document].
	Score float64
}

// _ScoredTokenGroup is internal object to track score for a token group.
type _ScoredTokenGroup struct {
	Tokens     []database.DocumentToken
	Score      float64
	Marker     [2]int
	WordMarker [2]int
}

// Config is the configuration for this search engine.
type Config struct {
	// Processor is function to process a rune into another rune(s). The result also
	// can be an empty slice, if that rune is supposed to be removed.
	//
	// This processor later will be used on the submitted [Document], but won't be
	// used on user queries. For user queries, developer responsible to process it
	// themselves.
	Processor Processor
	// Threshold is the minimum confidence score for search result.
	Threshold float64
}

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db        *sqlx.DB
	processor func(rune) []rune
	threshold float64
}

// Open the search storage in the specified path.
func OpenStorage(path string, cfg Config) (*Storage, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}

	return &Storage{
		db:        db,
		processor: cfg.Processor,
		threshold: cfg.Threshold,
	}, nil
}

// ApplyConfig applies the [Config] to the [Storage].
func (s *Storage) ApplyConfig(cfg Config) *Storage {
	s.processor = cfg.Processor
	s.threshold = cfg.Threshold
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
// themselves using norm.NFC. Developer should normalize the query before submitting
// it to this function.
func (s *Storage) Search(query string) ([]MatchedDocument, error) {
	// Clear up spaces from query
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, nil
	}

	// Convert the query into tokens. Notice we don't pass any processor.
	// Developer should normalize the query before submitting it to this function.
	queryTokens, query := tokenizer.Tokenize(query, nil)
	queryLength := len(queryTokens) + 3 - 1 // it was in trigram, so we revert it

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
		// Extract content of doc
		docContent := []rune(doc.Content)

		// Score each token group
		tokenGroups := make([]_ScoredTokenGroup, 0, len(doc.TokenGroups))

		for _, tg := range doc.TokenGroups {
			// Do coverage scoring for quick check, using geometric mean
			precision := calcCompactness(tg)                 // did we get unneeded stuff?
			recall := calcCompleteness(len(tg), nQueryToken) // did we capture everything?
			coverage := precision * recall

			// If the coverage is too bad, skip it
			if coverage < 0.5 {
				continue
			}

			// Create marker for this group
			start := tg[0].Start
			end := tg[len(tg)-1].End
			marker := [2]int{start, end}
			wordMarker := snapMarkerToWordBoundaries(docContent, marker)

			// Check edit distance for this group
			tgRunes := docContent[wordMarker[0]:wordMarker[1]]
			processedRunes, processedText := tokenizer.ProcessRunes(tgRunes, s.processor)
			editDistance := smetrics.Ukkonen(query, processedText, 1, 1, 1)

			// Calculate accuracy using edit distance
			maxLength := max(queryLength, len(processedRunes))
			accuracy := 1.0 - float64(editDistance)/float64(maxLength)

			// Slightly reward accuracy where marker already located in word
			if marker[0] == wordMarker[0] && marker[1] == wordMarker[1] {
				accuracy += (1 - accuracy) * 0.2
			}

			// Calculate confidence score using coverage, then reward its accuracy
			score := coverage + (1-coverage)*0.5*accuracy
			if score < scoreThreshold {
				continue
			}

			// Save the group
			tokenGroups = append(tokenGroups, _ScoredTokenGroup{
				Tokens:     tg,
				Score:      score,
				Marker:     marker,
				WordMarker: wordMarker,
			})
		}

		// If there are no good enough group, continue to next document
		nTokenGroups := len(tokenGroups)
		if nTokenGroups == 0 {
			continue
		}

		// Sort the token groups
		sort.Slice(tokenGroups, func(i, j int) bool {
			tg1 := tokenGroups[i]
			tg2 := tokenGroups[j]

			// Score
			if tg1.Score != tg2.Score {
				return tg1.Score > tg2.Score
			}

			// Count of tokens
			return len(tg1.Tokens) > len(tg2.Tokens)
		})

		// Calc combined score
		combinedScore := calcCombinedScore(tokenGroups)

		// Extract markers
		markers := make([][2]int, nTokenGroups)
		wordMarkers := make([][2]int, nTokenGroups)
		for i, tg := range tokenGroups {
			markers[i] = tg.Marker
			wordMarkers[i] = tg.WordMarker
		}

		// Save the result
		searchResults = append(searchResults, MatchedDocument{
			ID:          doc.Identifier,
			Content:     doc.Content,
			Markers:     markers,
			WordMarkers: wordMarkers,
			Score:       combinedScore,
		})
	}

	// If there are no search results, stop
	if len(searchResults) == 0 {
		return nil, nil
	}

	// Sort the search result
	sort.Slice(searchResults, func(i, j int) bool {
		fr1 := searchResults[i]
		fr2 := searchResults[j]

		// By combined score
		if fr1.Score != fr2.Score {
			return fr1.Score > fr2.Score
		}

		// By match counts
		if len(fr1.Markers) != len(fr2.Markers) {
			return len(fr1.Markers) > len(fr2.Markers)
		}

		// By identifier
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

	// Get position in document from these tokens
	positions := make([]int, nTokens)
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
	// gap between token position is at most 3 (since we use trigram, so gap of 3
	// means we only miss one token).
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
