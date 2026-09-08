package typosearch

import (
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

var dmp = diffmatchpatch.New()

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

// Processor is function to process a string, then return the processed string as
// list of [ProcessedRune], which is a rune + its index in the original string.
type Processor func(string) []ProcessedRune

// IndexProcessedString is a helper function to calculate the difference between
// the original and the processed string, then return list of [ProcessedRune].
func IndexProcessedString(original, processed string) []ProcessedRune {
	// Get diffs from both strings
	diffs := dmp.DiffMain(original, processed, false)

	// Use diffs to track position changes
	var cursor int
	runes := make([]ProcessedRune, 0, utf8.RuneCountInString(processed))

	for i, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffDelete:
			cursor += utf8.RuneCountInString(diff.Text)

		case diffmatchpatch.DiffInsert:
			insertLength := utf8.RuneCountInString(diff.Text)

			isReplace := i > 0 &&
				diffs[i-1].Type == diffmatchpatch.DiffDelete &&
				utf8.RuneCountInString(diffs[i-1].Text) == insertLength

			if isReplace {
				cursor -= insertLength
			}

			for _, r := range diff.Text {
				runes = append(runes, ProcessedRune{R: r, Index: cursor})
				if isReplace {
					cursor++
				}
			}

		case diffmatchpatch.DiffEqual:
			for _, r := range diff.Text {
				runes = append(runes, ProcessedRune{R: r, Index: cursor})
				cursor++
			}
		}
	}

	return runes
}

func defaultProcessor(s string) []ProcessedRune {
	runes := []rune(s)
	result := make([]ProcessedRune, len(runes))
	for i, r := range runes {
		result[i] = ProcessedRune{R: r, Index: i}
	}
	return result
}
