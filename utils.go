package typosearch

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/RadhiFadlillah/typosearch/internal/database"
)

// Adjust positions of a marker to word boundaries. It doesn't mutate the original position.
func snapMarkerToWordBoundaries(textRunes []rune, marker [2]int) [2]int {
	n := len(textRunes)
	start, end := marker[0], marker[1]

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
func snapMarkersToWordBoundaries(textRunes []rune, markers [][2]int) [][2]int {
	newPositions := make([][2]int, len(markers))
	for i, position := range markers {
		newPositions[i] = snapMarkerToWordBoundaries(textRunes, position)
	}
	return newPositions
}

// Merges any marker with overlapping positions. This function assume that
// positions already sorted. It doesn't mutate the original positions.
func mergeOverlappingMarkers(markers [][2]int) [][2]int {
	if len(markers) == 0 {
		return nil
	}

	mergedMarkers := make([][2]int, 0, len(markers))
	mergedMarkers = append(mergedMarkers, markers[0])

	for _, current := range markers[1:] {
		last := &mergedMarkers[len(mergedMarkers)-1]

		if current[0] <= last[1] { // overlaps or touches
			if current[1] > last[1] {
				last[1] = current[1] // extend the end
			}
		} else {
			mergedMarkers = append(mergedMarkers, current)
		}
	}

	return mergedMarkers
}

// Private debug function to print DocumentToken.
func debugPrintDocumentTokens(prefix string, tokens ...database.DocumentToken) {
	var strTokens, strStarts, strEnds, strQueryIndexes []string
	for _, token := range tokens {
		strTokens = append(strTokens, token.Token)
		strStarts = append(strStarts, strconv.Itoa(token.Start))
		strEnds = append(strEnds, strconv.Itoa(token.End))
		strQueryIndexes = append(strQueryIndexes, strconv.Itoa(token.IndexInQuery))
	}

	fmt.Printf("%s%s\n", prefix, strings.Join(strTokens, "\t"))
	fmt.Printf("%s%s\n", prefix, strings.Join(strStarts, "\t"))
	fmt.Printf("%s%s\n", prefix, strings.Join(strEnds, "\t"))
	fmt.Printf("%s%s\n", prefix, strings.Join(strQueryIndexes, "\t"))
}
