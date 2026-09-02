package tokenizer

import "unicode/utf8"

// Container for a processed rune and its position in the original string.
type ProcessedRune struct {
	R     rune
	Index int
}

// ProcessRunes applies each processor to every rune in s and returns the resulting
// runes together with its source position in the original string. Processors may
// delete, replace, or expand runes.
func ProcessRunes(s string, processors ...func(r rune) []rune) []ProcessedRune {
	var idx int
	result := make([]ProcessedRune, 0, utf8.RuneCountInString(s))

	for _, r := range s {
		processed := []rune{r}

		// Apply the processors to this rune
		for _, processor := range processors {
			next := make([]rune, 0, len(processed))
			for _, rr := range processed {
				next = append(next, processor(rr)...)
			}
			processed = next
		}

		// Once all processors applied, save to the result
		for _, rr := range processed {
			result = append(result, ProcessedRune{
				R:     rr,
				Index: idx,
			})
		}

		// Increase the index
		idx++
	}

	return result
}
