package typosearch

import (
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Highlight the matches in text.
func Highlight(
	text string,
	leftMarker string,
	rightMarker string,
	wordBased bool,
	positions ...[2]int,
) string {
	// Early check for empty positions
	if len(positions) == 0 {
		return text
	}

	// Convert text to runes, since highlight is char-based
	textRunes := []rune(text)
	nTextRune := len(textRunes)

	// Clone positions then sort
	sortedPositions := slices.Clone(positions)
	sort.Slice(sortedPositions, func(i, j int) bool {
		sp1 := sortedPositions[i]
		sp2 := sortedPositions[j]
		if sp1[0] != sp2[0] {
			return sp1[0] < sp2[0]
		}

		return sp1[1] < sp2[1]
	})

	// If needed, adjust positions so it wraps word by word
	if wordBased {
		sortedPositions = snapMarkersToWordBoundaries(textRunes, sortedPositions)
	}

	// Merge any overlapping positions
	sortedPositions = mergeOverlappingMarkers(sortedPositions)

	// Create helper function
	var sb strings.Builder
	writeRunesToBuilder := func(start, end int) {
		for i := start; i < end; i++ {
			sb.WriteRune(textRunes[i])
		}
	}

	// Write marker and text for each position
	var startCursor int
	for _, pos := range sortedPositions {
		// Write text before marker position
		writeRunesToBuilder(startCursor, pos[0])

		// Write text inside marker
		sb.WriteString(leftMarker)
		writeRunesToBuilder(pos[0], pos[1])
		sb.WriteString(rightMarker)

		// Update cursor
		startCursor = pos[1]
	}

	// Write leftover text
	writeRunesToBuilder(startCursor, nTextRune)

	return sb.String()
}

// Adjust positions of a marker to word boundaries. It doesn't mutate the original position.
func snapMarkerToWordBoundaries(textRunes []rune, position [2]int) [2]int {
	n := len(textRunes)
	start, end := position[0], position[1]

	// Safety check: clamp initial bounds
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

	// 1. Adjust Start
	if start < n && unicode.IsSpace(textRunes[start]) {
		for start < end && unicode.IsSpace(textRunes[start]) {
			start++
		}
	} else if start < n {
		for start > 0 && !unicode.IsSpace(textRunes[start-1]) {
			start--
		}
	}

	// 2. Adjust End
	if end > start && unicode.IsSpace(textRunes[end-1]) {
		for end > start && unicode.IsSpace(textRunes[end-1]) {
			end--
		}
	} else {
		for end < n && end > start && !unicode.IsSpace(textRunes[end]) {
			end++
		}
	}

	return [2]int{start, end}
}

// Adjust positions of several markers to closest word boundaries. It doesn't mutate
// the original positions.
func snapMarkersToWordBoundaries(textRunes []rune, positions [][2]int) [][2]int {
	newPositions := make([][2]int, len(positions))
	for i, position := range positions {
		newPositions[i] = snapMarkerToWordBoundaries(textRunes, position)
	}
	return newPositions
}

// Merges any marker with overlapping positions. This function assume that
// positions already sorted. It doesn't mutate the original positions.
func mergeOverlappingMarkers(positions [][2]int) [][2]int {
	if len(positions) == 0 {
		return nil
	}

	mergedPositions := make([][2]int, 0, len(positions))
	mergedPositions = append(mergedPositions, positions[0])

	for _, current := range positions[1:] {
		last := &mergedPositions[len(mergedPositions)-1]

		if current[0] <= last[1] { // overlaps or touches
			if current[1] > last[1] {
				last[1] = current[1] // extend the end
			}
		} else {
			mergedPositions = append(mergedPositions, current)
		}
	}

	return mergedPositions
}
