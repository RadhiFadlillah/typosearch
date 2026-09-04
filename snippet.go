package typosearch

import (
	"strings"
	"unicode"
)

// Snippet extracts a string from text around the marker position. It expands
// outward up to wordCount and adds ellipsis "..." if bounds are not reached.
func Snippet(text string, position [2]int, wordCount int) (string, [2]int) {
	// Adjust word count
	if wordCount == 0 {
		wordCount = 30
	}

	// Convert text to runes, since snippet is char-based
	textRunes := []rune(text)

	// Adjust marker position to word based.
	wordBoundPosition := snapMarkerToWordBoundaries(textRunes, position)

	// Safety check: clamp initial bounds
	n := len(textRunes)
	start, end := wordBoundPosition[0], wordBoundPosition[1]

	if start < 0 {
		start = 0
	}

	if start > n {
		start = n
	}

	if end < start {
		end = start
	}

	if end > n {
		end = n
	}

	// Count words currently inside marker.
	inWord := false
	var insideCount int
	for i := start; i < end; i++ {
		if unicode.IsSpace(textRunes[i]) {
			inWord = false
		} else if !inWord {
			inWord = true
			insideCount++
		}
	}

	// Expand if we need more words
	if insideCount < wordCount {
		needed := wordCount - insideCount
		leftNeeded := needed / 2
		rightNeeded := needed - leftNeeded

		// Expand left
		for leftNeeded > 0 && start > 0 {
			// Move back as long the prev char is space.
			for start > 0 && unicode.IsSpace(textRunes[start-1]) {
				start--
			}

			// Safety check if we reached the beginning of text
			if start == 0 {
				break
			}

			// At this point we stopped at the last space, right after a letter.
			// Move back as long the prev char is a letter (non-space).
			for start > 0 && !unicode.IsSpace(textRunes[start-1]) {
				start--
			}

			// Once we finished that loop, we have consumed one word.
			leftNeeded--
		}

		// Shift unused left quota to the right
		rightNeeded += leftNeeded

		// Expand right
		for rightNeeded > 0 && end < n {
			// Move forward as long the char is space. We don't do +1 like when
			// expanding left, because in Go the high bound is exclusive.
			for end < n && unicode.IsSpace(textRunes[end]) {
				end++
			}

			// Safety check if we reached the end of text
			if end == n {
				break
			}

			// At this point We stopped at a letter, right after a space.
			// Move forward as long the char is a letter (non-space).
			for end < n && !unicode.IsSpace(textRunes[end]) {
				end++
			}

			// Once we finished that loop, we have consumed one word.
			rightNeeded--
		}

		// Update leftNeeded to capture any unused rightNeeded quota
		leftNeeded = rightNeeded

		// Expand left again in case we hit the right boundary and have remaining quota
		for leftNeeded > 0 && start > 0 {
			for start > 0 && unicode.IsSpace(textRunes[start-1]) {
				start--
			}

			if start == 0 {
				break
			}

			for start > 0 && !unicode.IsSpace(textRunes[start-1]) {
				start--
			}

			leftNeeded--
		}
	}

	// Trim any remaining leading/trailing spaces directly on the indices
	for start < end && unicode.IsSpace(textRunes[start]) {
		start++
	}

	for end > start && unicode.IsSpace(textRunes[end-1]) {
		end--
	}

	// Calculate relative position based on the ORIGINAL marker, not the word-bounded one.
	relStart := position[0] - start
	relEnd := position[1] - start
	snippetRuneLen := end - start

	// Clamp the bounds. This prevents negative indices if the snippet
	// trimmed leading/trailing spaces that were part of the original marker.
	if relStart < 0 {
		relStart = 0
	}

	if relStart > snippetRuneLen {
		relStart = snippetRuneLen
	}

	if relEnd < relStart {
		relEnd = relStart
	}

	if relEnd > snippetRuneLen {
		relEnd = snippetRuneLen
	}

	relPosition := [2]int{relStart, relEnd}

	// Build the final string using strings.Builder
	// Pre-allocate capacity: text length + max 6 bytes for ellipses
	var sb strings.Builder
	sb.Grow(snippetRuneLen + 6)

	// Add ellipsis on left
	if start > 0 {
		sb.WriteString("...")
		relPosition[0] += 3
		relPosition[1] += 3
	}

	// Put the snippet inside strings.Builder
	if end > start {
		for i := start; i < end; i++ {
			sb.WriteRune(textRunes[i])
		}
	}

	// Add ellipsis on right
	if end < n {
		sb.WriteString("...")
	}

	return sb.String(), relPosition
}
