package typosearch

import (
	"reflect"
	"testing"
)

func Test_snapMarkersToWordBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		positions [][2]int
		want      [][2]int
	}{
		{
			name:      "already perfect boundaries",
			text:      "oh hello world",
			positions: [][2]int{{3, 8}, {9, 14}}, // "hello", "world"
			want:      [][2]int{{3, 8}, {9, 14}}, // "hello", "world"
		},
		{
			name:      "start mid-word expands backward",
			text:      "oh hello world",
			positions: [][2]int{{5, 8}}, // "llo"
			want:      [][2]int{{3, 8}}, // "hello"
		},
		{
			name:      "end mid-word expands forward",
			text:      "oh hello world",
			positions: [][2]int{{3, 6}}, // "hel"
			want:      [][2]int{{3, 8}}, // "hello"
		},
		{
			name:      "both mid-word expand outward",
			text:      "oh hello world",
			positions: [][2]int{{5, 11}}, // "llo wo"
			want:      [][2]int{{3, 14}}, // "hello world"
		},
		{
			name:      "start on leading space moves forward",
			text:      "oh hello world",
			positions: [][2]int{{8, 14}}, // " world"
			want:      [][2]int{{9, 14}}, // "world"
		},
		{
			name:      "end on trailing space moves backward",
			text:      "oh hello world",
			positions: [][2]int{{3, 9}}, // "hello "
			want:      [][2]int{{3, 8}}, // "hello"
		},
		{
			name:      "selection entirely whitespace collapses",
			text:      "oh hello   world",
			positions: [][2]int{{11, 11}}, // ""
			want:      [][2]int{{11, 11}}, // ""
		},
		{
			name:      "multiple positions processed independently",
			text:      "foo bar baz",
			positions: [][2]int{{1, 2}, {5, 6}}, // "o", "a"
			want:      [][2]int{{0, 3}, {4, 7}}, // "foo", "bar"
		},
		{
			name:      "start at 0 mid-word",
			text:      "oh hello world",
			positions: [][2]int{{3, 5}}, // "he"
			want:      [][2]int{{3, 8}}, // "hello"
		},
		{
			name:      "end at len(text) mid-word",
			text:      "oh hello world",
			positions: [][2]int{{12, 14}}, // "ld"
			want:      [][2]int{{9, 14}},  // "world"
		},
		{
			name:      "full string selected, no whitespace at edges",
			text:      "ohhelloworld",
			positions: [][2]int{{6, 10}}, // "owor"
			want:      [][2]int{{0, 12}}, // "ohhelloworld"
		},
		{
			name:      "empty range at word boundary stays put",
			text:      "oh hello world",
			positions: [][2]int{{8, 8}}, // ""
			want:      [][2]int{{8, 8}}, // ""
		},
		{
			name:      "single character word",
			text:      "a b c",
			positions: [][2]int{{2, 3}}, // "b"
			want:      [][2]int{{2, 3}}, // "b"
		},
		{
			name:      "tabs and newlines count as space",
			text:      "foo\tbar\nbaz",
			positions: [][2]int{{4, 7}}, // "bar"
			want:      [][2]int{{4, 7}}, // "bar"
		},
		{
			name:      "expand to cover multiple words",
			text:      "one two three",
			positions: [][2]int{{2, 9}},  // "e two t"
			want:      [][2]int{{0, 13}}, // "one two three"
		},
		{
			name:      "punctuation is grouped with words",
			text:      "Hello, world!",
			positions: [][2]int{{1, 4}}, // "ell"
			want:      [][2]int{{0, 6}}, // "Hello,"
		},
		{
			name:      "selection is entirely whitespace",
			text:      "     ",
			positions: [][2]int{{1, 4}},
			want:      [][2]int{{4, 4}}, // Collapses to an empty range
		},
		{
			name:      "out of bounds safety",
			text:      "test",
			positions: [][2]int{{-5, 20}, {5, 2}}, // Below 0/Above N, and reversed
			want:      [][2]int{{0, 4}, {4, 4}},
		},
		{
			name:      "multibyte runes handling",
			text:      "こんにちは 世界",               // "Hello world" in Japanese
			positions: [][2]int{{1, 3}, {7, 8}}, // "んに", "界"
			want:      [][2]int{{0, 5}, {6, 8}}, // "こんにちは", "世界"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runes := []rune(tt.text)
			got := snapMarkersToWordBoundaries(runes, tt.positions)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"text=%q input=%v\n got: %v\nwant: %v",
					tt.text, tt.positions, got, tt.want,
				)
			}
		})
	}
}

func Test_mergeOverlappingMarkers(t *testing.T) {
	tests := []struct {
		name      string
		positions [][2]int
		want      [][2]int
	}{
		{
			name:      "Empty slice",
			positions: [][2]int{},
			want:      nil,
		},
		{
			name:      "Single marker",
			positions: [][2]int{{0, 5}},
			want:      [][2]int{{0, 5}},
		},
		{
			name:      "No overlaps",
			positions: [][2]int{{0, 5}, {7, 10}, {12, 15}},
			want:      [][2]int{{0, 5}, {7, 10}, {12, 15}},
		},
		{
			name:      "Partial overlaps",
			positions: [][2]int{{0, 5}, {3, 8}, {7, 10}},
			want:      [][2]int{{0, 10}},
		},
		{
			name:      "Touching markers (adjacent)",
			positions: [][2]int{{0, 5}, {5, 10}},
			want:      [][2]int{{0, 10}}, // Merges seamlessly
		},
		{
			name:      "Fully engulfed marker",
			positions: [][2]int{{0, 10}, {2, 5}},
			want:      [][2]int{{0, 10}}, // [2, 5] is absorbed
		},
		{
			name:      "Multiple distinct merge groups",
			positions: [][2]int{{1, 4}, {2, 6}, {8, 10}, {9, 12}},
			want:      [][2]int{{1, 6}, {8, 12}},
		},
		{
			name:      "All overlapping into one large chunk",
			positions: [][2]int{{0, 2}, {1, 4}, {3, 6}, {5, 8}},
			want:      [][2]int{{0, 8}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeOverlappingMarkers(tt.positions)

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"input=%v\n got: %v\nwant: %v",
					tt.positions, got, tt.want,
				)
			}
		})
	}
}
