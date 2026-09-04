package typosearch

import (
	"testing"
)

func TestSnippet(t *testing.T) {
	text := "The quick brown fox jumps over the lazy dog in the park"

	tests := []struct {
		name              string
		position          [2]int
		wordCount         int
		wantSnippet       string
		wantPosition      [2]int
		expectedHighlight string // What should the returned marker actually select inside the snippet?
	}{
		{
			name:              "exact match in middle",
			position:          [2]int{16, 25}, // "fox jumps"
			wordCount:         2,
			wantSnippet:       "...fox jumps...",
			wantPosition:      [2]int{3, 12}, // Shifted by +3 for "..."
			expectedHighlight: "fox jumps",
		},
		{
			name:              "expands with ellipses (symmetric)",
			position:          [2]int{16, 25}, // "fox jumps"
			wordCount:         4,
			wantSnippet:       "...brown fox jumps over...",
			wantPosition:      [2]int{9, 18}, // "...brown " is 9 chars
			expectedHighlight: "fox jumps",
		},
		{
			name:              "hits left boundary (shifts quota right)",
			position:          [2]int{4, 9}, // "quick"
			wordCount:         4,
			wantSnippet:       "The quick brown fox...", // Needs 3. Hits left boundary early.
			wantPosition:      [2]int{4, 9},             // No left ellipsis, so position matches original offset from start (0)
			expectedHighlight: "quick",
		},
		{
			name:              "hits right boundary (shifts quota left)",
			position:          [2]int{51, 55}, // "park"
			wordCount:         4,
			wantSnippet:       "...dog in the park",
			wantPosition:      [2]int{14, 18},
			expectedHighlight: "park",
		},
		{
			name:              "word count 0 defaults to 30 (covers whole text)",
			position:          [2]int{16, 19}, // "fox"
			wordCount:         0,
			wantSnippet:       "The quick brown fox jumps over the lazy dog in the park",
			wantPosition:      [2]int{16, 19}, // No ellipses, exact original coordinates
			expectedHighlight: "fox",
		},
		{
			name:              "highlight partial word (preserves original marker)",
			position:          [2]int{17, 20}, // "ox " (inside "fox jumps")
			wordCount:         2,
			wantSnippet:       "...fox jumps...",
			wantPosition:      [2]int{4, 7},
			expectedHighlight: "ox ",
		},
		{
			name:              "original marker has leading spaces (clamping test)",
			position:          [2]int{15, 25}, // " fox jumps" (starts in the space before fox)
			wordCount:         2,
			wantSnippet:       "...fox jumps...",
			wantPosition:      [2]int{3, 12},
			expectedHighlight: "fox jumps",
		},
		{
			name:              "out of bounds marker gracefully clamped",
			position:          [2]int{-5, 100},
			wordCount:         5,
			wantSnippet:       "The quick brown fox jumps over the lazy dog in the park",
			wantPosition:      [2]int{0, 55}, // Clamped to bounds of the string
			expectedHighlight: "The quick brown fox jumps over the lazy dog in the park",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSnippet, gotPos := Snippet(text, tt.position, tt.wordCount)

			if gotSnippet != tt.wantSnippet {
				t.Errorf("\nsnippet got:\n%q\nwant:\n%q", gotSnippet, tt.wantSnippet)
			}

			if gotPos != tt.wantPosition {
				t.Errorf("\nposition got:\n%v\nwant:\n%v", gotPos, tt.wantPosition)
			}

			// Verify that applying the returned position to the snippet actually extracts the expected string
			if gotSnippet != "" {
				snippetRunes := []rune(gotSnippet)

				// Safety check to ensure tests don't panic due to bad bounds
				if gotPos[0] < 0 || gotPos[1] > len(snippetRunes) || gotPos[0] > gotPos[1] {
					t.Fatalf("returned position %v is invalid for snippet of length %d", gotPos, len(snippetRunes))
				}

				highlighted := string(snippetRunes[gotPos[0]:gotPos[1]])
				if highlighted != tt.expectedHighlight {
					t.Errorf("\nextracted highlight got:\n%q\nwant:\n%q", highlighted, tt.expectedHighlight)
				}
			}
		})
	}
}
