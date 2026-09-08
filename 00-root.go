package typosearch

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/RadhiFadlillah/typosearch/internal/database"
	"github.com/RadhiFadlillah/typosearch/internal/tokenizer"
	"github.com/jmoiron/sqlx"
	"github.com/xrash/smetrics"

	_ "modernc.org/sqlite"
)

// Document is the text document that will be indexed to be later searched on.
type Document struct {
	// Identifier is the unique identifier for this Document.
	Identifier string
	// Type is the type of this Document.
	Type string
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

// _MatchCandidate is internal object to track a match candidate.
type _MatchCandidate struct {
	ID          int
	TokenGroups []_ScoredTokenGroup
}

// _ScoredTokenGroup is internal object to track score for a token group.
type _ScoredTokenGroup struct {
	Tokens     []database.DocumentToken
	Score      float64
	Marker     [2]int
	WordMarker [2]int
}

// Transformer is pair of function to handle [Document] before indexed.
type Transformer struct {
	// Splitter is function to split runes into several groups before it being
	// processed and tokenized.
	Splitter Splitter
	// Processor is function to process a rune into another rune(s). The result also
	// can be an empty slice, if that rune is supposed to be removed.
	Processor Processor
}

// Config is the configuration for this search engine.
type Config struct {
	// Transformers is map between Document type and its Transformer. Transformer
	// itself is object to preprocess the document content, before it's tokenized
	// and stored in indexes [Storage].
	//
	// This transformer later will be used on the submitted [Document], but won't be
	// used on user queries. For user queries, developer responsible to process it
	// themselves.
	Transformers map[string]Transformer
	// Threshold is the minimum confidence score for search result.
	Threshold float64
}

// Storage is the container for storing trigram indexes for documents that will be
// searched later. Use sqlite3 as database engine.
type Storage struct {
	db           *sqlx.DB
	transformers map[string]Transformer
	threshold    float64
}

// Open the search storage in the specified path.
func OpenStorage(path string, cfg Config) (*Storage, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}

	return &Storage{
		db:           db,
		transformers: cfg.Transformers,
		threshold:    cfg.Threshold,
	}, nil
}

// ApplyConfig applies the [Config] to the [Storage].
func (s *Storage) ApplyConfig(cfg Config) *Storage {
	s.transformers = cfg.Transformers
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
		if doc.Type == "" {
			return fmt.Errorf("document with id %q has no type", doc.Identifier)
		}

		transformer, exist := s.transformers[doc.Type]
		if !exist {
			return fmt.Errorf("transformer for type %q has not registered", doc.Type)
		}

		dbDocs[i] = database.InsertDocumentArg{
			Identifier: doc.Identifier,
			Type:       doc.Type,
			Content:    doc.Content,
			Splitter:   transformer.Splitter,
			Processor:  transformer.Processor,
		}
	}

	return database.InsertDocuments(s.db, dbDocs)
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

	// Convert the query into tokens. Notice we don't pass any splitter or processor.
	// Developer should normalize the query before submitting it to this search function.
	queryTokens, query := tokenizer.Tokenize(query, nil, nil)

	// Convert tokens into strings
	tokenStrings := make([]string, len(queryTokens))
	for i, token := range queryTokens {
		tokenStrings[i] = token.String()
	}

	// Fetch list of matching candidates from database
	nQueryToken := len(tokenStrings)
	candidates, err := database.GetDocumentsByTokens(s.db, tokenStrings...)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// 1st layer: filter by coverage (precision + recall)
	goodCandidates := s.filterInitialCandidates(candidates, nQueryToken)

	// 2nd layer: filter by accuracy and combined score
	return s.filterGoodCandidates(goodCandidates, query)
}

func (s Storage) filterInitialCandidates(
	candidates []database.DocumentWithTokensGroups,
	nQueryToken int,
) []_MatchCandidate {
	// Prepare variable to store result
	goodCandidates := make([]_MatchCandidate, 0, len(candidates))

	for _, candidate := range candidates {
		// Score each token group
		tokenGroups := make([]_ScoredTokenGroup, 0, len(candidate.TokenGroups))

		for _, tg := range candidate.TokenGroups {
			// Do coverage scoring for quick check, using geometric mean
			precision := s.calcCompactness(tg)                 // did we get unneeded stuff?
			recall := s.calcCompleteness(len(tg), nQueryToken) // did we capture everything?
			coverage := precision * recall

			// If the coverage is not good, skip it
			if coverage < 0.5 {
				continue
			}

			// Create marker for this group
			start := tg[0].Start
			end := tg[len(tg)-1].End
			marker := [2]int{start, end}

			// Save the group
			tokenGroups = append(tokenGroups, _ScoredTokenGroup{
				Tokens:     tg,
				Score:      coverage, // for now we only use coverage
				Marker:     marker,
				WordMarker: [2]int{}, // we will calculate them in 2nd filter
			})
		}

		// If there are no decent group, continue to next candidate
		nTokenGroups := len(tokenGroups)
		if nTokenGroups == 0 {
			continue
		}

		// Save the good candidate
		goodCandidates = append(goodCandidates, _MatchCandidate{
			ID:          candidate.DocumentID,
			TokenGroups: tokenGroups,
		})
	}

	return goodCandidates
}

func (s Storage) filterGoodCandidates(
	goodCandidates []_MatchCandidate,
	query string,
) ([]MatchedDocument, error) {
	// Get content for the candidates, since we need it to measure accuracy
	documentIDs := make([]int, len(goodCandidates))
	for i, candidate := range goodCandidates {
		documentIDs[i] = candidate.ID
	}

	documents, err := database.GetDocuments(s.db, documentIDs...)
	if err != nil {
		return nil, err
	}

	// Prepare default confidence threshold
	scoreThreshold := s.threshold
	if scoreThreshold <= 0 || scoreThreshold > 1 {
		scoreThreshold = 0.5
	}

	// Create scores for each candidate
	searchResults := make([]MatchedDocument, 0, len(goodCandidates))

	for _, candidate := range goodCandidates {
		// Make sure candidate's content was found
		doc, exist := documents[candidate.ID]
		if !exist {
			continue
		}

		// Get transformet for this doc
		transformer := s.transformers[doc.Type]

		// Cast content to []rune
		content := []rune(doc.Content)

		// Score each token group
		tokenGroups := make([]_ScoredTokenGroup, 0, len(candidate.TokenGroups))

		for _, tg := range candidate.TokenGroups {
			// Create word marker using marker that already created before
			tg.WordMarker = snapMarkerToWordBoundaries(content, tg.Marker)

			// Check edit distance for this group
			tgRunes := content[tg.WordMarker[0]:tg.WordMarker[1]]
			_, processedText := tokenizer.ProcessRunes(tgRunes,
				transformer.Splitter,
				transformer.Processor)
			editDistance := smetrics.Ukkonen(query, processedText, 1, 1, 1)

			// Calculate accuracy using edit distance
			maxLength := max(len(query), len(processedText))
			accuracy := 1.0 - float64(editDistance)/float64(maxLength)

			// Slightly reward accuracy where marker already located in word
			if tg.Marker[0] == tg.WordMarker[0] && tg.Marker[1] == tg.WordMarker[1] {
				accuracy += (1 - accuracy) * 0.2
			}

			// Calculate confidence score using coverage, then reward its accuracy
			coverage := tg.Score // from before
			tg.Score = coverage + (1-coverage)*0.5*accuracy
			if tg.Score < scoreThreshold {
				continue
			}

			// Save the group
			tokenGroups = append(tokenGroups, tg)
		}

		// If there are no decent group, continue to next candidate
		nTokenGroups := len(tokenGroups)
		if nTokenGroups == 0 {
			continue
		}

		// Sort the token groups by best score
		slices.SortFunc(tokenGroups, func(tg1, tg2 _ScoredTokenGroup) int {
			if tg1.Score != tg2.Score {
				return -cmp.Compare(tg1.Score, tg2.Score) // tg1 > tg2
			}
			return -cmp.Compare(len(tg1.Tokens), len(tg2.Tokens)) // tg1 > tg2
		})

		// Extract markers
		markers := make([][2]int, nTokenGroups)
		wordMarkers := make([][2]int, nTokenGroups)
		for i, tg := range tokenGroups {
			markers[i] = tg.Marker
			wordMarkers[i] = tg.WordMarker
		}

		// Save the result
		searchResults = append(searchResults, MatchedDocument{
			Identifier:  doc.Identifier,
			Type:        doc.Type,
			Content:     doc.Content,
			Markers:     markers,
			WordMarkers: wordMarkers,
			Score:       s.calcCombinedScore(tokenGroups),
		})
	}

	// If there are no search results, stop
	if len(searchResults) == 0 {
		return nil, nil
	}

	// Sort the search result
	slices.SortFunc(searchResults, func(sr1, sr2 MatchedDocument) int {
		// By combined score
		if sr1.Score != sr2.Score {
			return -cmp.Compare(sr1.Score, sr2.Score) // sr1 > sr2
		}

		// By match counts
		if n1, n2 := len(sr1.Markers), len(sr2.Markers); n1 != n2 {
			return -cmp.Compare(n1, n2) // n1 > n2
		}

		// By identifier
		return cmp.Compare(sr1.Identifier, sr2.Identifier)
	})

	return searchResults, nil
}

func (s Storage) calcCompleteness(currentCount, expectedCount int) float64 {
	// Penalize when completeness is too small.
	// Use formula 3s^2 - 2s^3 so score < 0.5 is penalized smoothly.
	score := float64(currentCount) / float64(expectedCount)
	return 3*score*score - 2*score*score*score
}

func (s Storage) calcCompactness(documentTokens []database.DocumentToken) float64 {
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
func (s Storage) calcCombinedScore(tokenGroups []_ScoredTokenGroup) float64 {
	combinedScore := tokenGroups[0].Score

	if len(tokenGroups) > 1 {
		alpha := 0.3
		decay := 0.5
		topScore := tokenGroups[0].Score

		weight := 1.0
		var weightedSum, sumOfWeight float64
		for i := 1; i < len(tokenGroups); i++ {
			score := tokenGroups[i].Score
			weightedSum += score * weight
			sumOfWeight += weight
			weight *= decay
		}

		leftoverScores := weightedSum / sumOfWeight
		combinedScore = topScore + (1-topScore)*leftoverScores*alpha
	}

	return combinedScore
}
