package tokenizer

import (
	"strings"
	"unicode/utf8"
)

// Container for a processed rune and its position in the original string.
type ProcessedRune struct {
	R     rune
	Index int
}

// Group of [ProcessedRune], with various helper function.
type ProcessedRuneGroup []ProcessedRune

// Returns the runes as combined string.
func (prg ProcessedRuneGroup) String() string {
	var sb strings.Builder
	for _, pr := range prg {
		sb.WriteRune(pr.R)
	}
	return sb.String()
}

// Returns the start and end index for these runes.
func (prg ProcessedRuneGroup) Range() (int, int) {
	if len(prg) == 0 {
		return 0, 0
	}

	start := prg[0].Index
	end := prg[len(prg)-1].Index + 1 // +1 because in Go upper bound is exclusive
	return start, end
}

// ProcessRunes applies each processor to every rune in s and returns the resulting
// runes together with its source position in the original string. Processors may
// delete, replace, or expand runes.
func ProcessRunes(s string, processors ...func(r rune) []rune) ProcessedRuneGroup {
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
