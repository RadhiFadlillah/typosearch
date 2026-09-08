package typosearch

import (
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

var dmp = diffmatchpatch.New()

// Container for a processed rune and its position in the original string.
type IndexedRune struct {
	R     rune
	Index int
}

// Group of [IndexedRune], with various helper function.
type IndexedRuneGroup []IndexedRune

// Returns the runes as combined string.
func (irg IndexedRuneGroup) String() string {
	var sb strings.Builder
	for _, pr := range irg {
		sb.WriteRune(pr.R)
	}
	return sb.String()
}

// Returns the start and end index for these runes.
func (irg IndexedRuneGroup) Range() (int, int) {
	if len(irg) == 0 {
		return 0, 0
	}

	start := irg[0].Index
	end := irg[len(irg)-1].Index + 1 // +1 because in Go upper bound is exclusive
	return start, end
}

// Processor is function to process a string, then return the processed string.
type Processor func(string) string

// IndexedProcessor is like the [Processor], but returns the processed string
// as list of [IndexedRune], which is a rune + its index in the original string.
type IndexedProcessor func(string) []IndexedRune

// IndexProcessedString is a helper function to calculate the difference between
// the original and the processed string, then return list of [IndexedRune].
func IndexProcessedString(original, processed string) []IndexedRune {
	// Get diffs from both strings
	diffs := dmp.DiffMain(original, processed, false)

	// Use diffs to track position changes
	var cursor int
	runes := make([]IndexedRune, 0, utf8.RuneCountInString(processed))

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
				runes = append(runes, IndexedRune{R: r, Index: cursor})
				if isReplace {
					cursor++
				}
			}

		case diffmatchpatch.DiffEqual:
			for _, r := range diff.Text {
				runes = append(runes, IndexedRune{R: r, Index: cursor})
				cursor++
			}
		}
	}

	return runes
}

func defaultProcessor(s string) string {
	return s
}

func defaultIndexedProcessor(s string) []IndexedRune {
	runes := []rune(s)
	result := make([]IndexedRune, len(runes))
	for i, r := range runes {
		result[i] = IndexedRune{R: r, Index: i}
	}
	return result
}
