package typosearch

import (
	"slices"
	"sort"
	"strings"
)

// Highlight the matches in text.
func Highlight(
	text string,
	leftMarker string,
	rightMarker string,
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
