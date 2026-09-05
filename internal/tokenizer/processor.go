package tokenizer

import (
	"strings"
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
// both the processed runes and its string. Every rune is attached with its source
// position in the original string.
func ProcessRunes(originalRunes []rune, processor func(r rune) []rune) (ProcessedRuneGroup, string) {
	// Apply default processor
	if processor == nil {
		processor = func(r rune) []rune { return []rune{r} }
	}

	// Prepare result
	var sb strings.Builder
	result := make([]ProcessedRune, 0, len(originalRunes))

	var idx int
	for _, r := range originalRunes {
		for _, rr := range processor(r) {
			sb.WriteRune(rr)
			result = append(result, ProcessedRune{
				R:     rr,
				Index: idx,
			})
		}

		// Increase the index
		idx++
	}

	return result, sb.String()
}
