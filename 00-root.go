package typosearch

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/RadhiFadlillah/typosearch/internal/database"
	"github.com/jmoiron/sqlx"
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
	// Processor is function to process a string into a normalized string. Used when
	// querying, so must have the same output as IndexedProcessor.
	Processor Processor
	// IndexedProcessor is like Processor, but returns a list of runes + its
	// original position in string. Used when indexing document, so must have the
	// same output as Processor.
	IndexedProcessor IndexedProcessor
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
func (s *Storage) AddDocuments(ctx context.Context, docs ...Document) error {
	// Cast Document to insert arg
	dbDocs := make([]database.InsertDocumentArg, len(docs))
	for i, doc := range docs {
		// Check for document validity
		if doc.Type == "" {
			return fmt.Errorf("document with id %q has no type", doc.Identifier)
		}

		// Make sure transformer is registered
		transformer, exist := s.transformers[doc.Type]
		if !exist {
			return fmt.Errorf("transformer for type %q has not registered", doc.Type)
		}

		// Run transformer
		processedSegments := s.runIndexedTransformer(doc.Content, transformer)

		// Count how many trigrams will be generated later
		var nTrigrams int
		for _, segment := range processedSegments {
			if nRunes := len(segment); nRunes >= 3 { // 3 for trigram
				nTrigrams += nRunes - 3 + 1
			}
		}

		// Create trigrams for each segment
		allTrigrams := make([][]IndexedRune, 0, nTrigrams)
		for _, segment := range processedSegments {
			segmentTrigrams := trigrams(segment)
			allTrigrams = append(allTrigrams, segmentTrigrams...)
		}

		// Convert the trigrams into document tokens
		docTokens := make([]database.DocumentToken, len(allTrigrams))
		for i := range allTrigrams {
			tri := IndexedRuneGroup(allTrigrams[i])
			start, end := tri.Range()
			docTokens[i] = database.DocumentToken{
				Start: start,
				End:   end,
				Token: tri.String(),
			}
		}

		dbDocs[i] = database.InsertDocumentArg{
			Identifier: doc.Identifier,
			Type:       doc.Type,
			Content:    doc.Content,
			Tokens:     docTokens,
		}
	}

	return database.InsertDocuments(ctx, s.db, dbDocs)
}

// DeleteDocuments remove the documents in the storage.
func (st *Storage) DeleteDocuments(ctx context.Context, ids ...string) error {
	return database.DeleteDocuments(ctx, st.db, ids...)
}

// Search the storage for suitable documents. Developer should normalize the query
// before submitting it to this function.
func (s *Storage) Search(ctx context.Context, query string, types ...string) ([]MatchedDocument, error) {
	// Clear up spaces from query
	query = strings.Join(strings.Fields(query), " ")
	if query == "" {
		return nil, nil
	}

	// Convert the query into trigram tokens.
	queryTokens := trigrams([]rune(query))

	// Convert tokens frome runes into strings
	tokenStrings := make([]string, len(queryTokens))
	for i, token := range queryTokens {
		tokenStrings[i] = string(token)
	}

	// Fetch list of matching candidates from database
	nQueryToken := len(tokenStrings)
	candidates, err := database.GetDocumentsByTokens(ctx, s.db, tokenStrings, types...)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// 1st layer: filter by coverage (precision + recall)
	goodCandidates := s.filterInitialCandidates(candidates, nQueryToken)

	// 2nd layer: filter by accuracy and combined score
	return s.filterGoodCandidates(ctx, goodCandidates, query)
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
	ctx context.Context,
	goodCandidates []_MatchCandidate,
	query string,
) ([]MatchedDocument, error) {
	if len(goodCandidates) == 0 {
		return nil, nil
	}

	// Get content for the candidates, since we need it to measure accuracy
	documentIDs := make([]int, len(goodCandidates))
	for i, candidate := range goodCandidates {
		documentIDs[i] = candidate.ID
	}

	documents, err := database.GetDocuments(ctx, s.db, documentIDs...)
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
			processedText := s.runTransformer(string(tgRunes), transformer)

			diffs := dmp.DiffMain(query, processedText, false)
			editDistance := dmp.DiffLevenshtein(diffs)

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

// runTransformer apply transformer to the original string. It will run [Splitter]
// to separate string to several segments, then each segment will be processed by
// the [Processor], then the output will be combined to one final string.
func (s Storage) runTransformer(original string, transformer Transformer) string {
	// Prepare default splitter and processor
	splitter := transformer.Splitter
	processor := transformer.Processor

	if splitter == nil {
		splitter = defaultSplitter
	}

	if processor == nil {
		processor = defaultProcessor
	}

	// Run splitter
	segments := splitter(original)

	// Process each segments
	var sb strings.Builder
	for _, segment := range segments {
		sb.WriteString(processor(segment))
	}

	return sb.String()
}

// runIndexedTransformer apply transformer to the original string. It will run
// [Splitter] to separate string to several segments, then each segment will be
// processed by the [IndexedProcessor], so each segments will have their own
// [IndexedRune].
func (s Storage) runIndexedTransformer(original string, transformer Transformer) []IndexedRuneGroup {
	// Prepare default splitter and processor
	splitter := transformer.Splitter
	processor := transformer.IndexedProcessor

	if splitter == nil {
		splitter = defaultSplitter
	}

	if processor == nil {
		processor = defaultIndexedProcessor
	}

	// Run splitter
	segments := splitter(original)

	// Process each segments
	var start int
	processedSegments := make([]IndexedRuneGroup, 0, len(segments))

	for _, segment := range segments {
		// Process the segment
		var processedSegment IndexedRuneGroup
		processedSegment = processor(segment)

		// Adjust index for the processed
		for i := range processedSegment {
			processedSegment[i].Index += start
		}

		// Increase start position
		start += utf8.RuneCountInString(segment)

		// Save the processed runes
		if len(processedSegment) > 0 {
			processedSegments = append(processedSegments, processedSegment)
		}
	}

	return processedSegments
}
