package typosearch

import (
	"reflect"
	"testing"
)

func TestWordSplitter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "basic multiple spaces between words",
			input: "Hello my      world",
			want:  []string{"Hello ", "my      ", "world"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single word no space",
			input: "Hello",
			want:  []string{"Hello"},
		},
		{
			name:  "single word with trailing space",
			input: "Hello ",
			want:  []string{"Hello "},
		},
		{
			name:  "only spaces",
			input: "   ",
			want:  []string{"   "},
		},
		{
			name:  "leading spaces before first word",
			input: "   Hello world",
			want:  []string{"   Hello ", "world"},
		},
		{
			name:  "single space between words",
			input: "Hello world",
			want:  []string{"Hello ", "world"},
		},
		{
			name:  "tabs and newlines count as space",
			input: "Hello\tworld\nfoo",
			want:  []string{"Hello\t", "world\n", "foo"},
		},
		{
			name:  "trailing spaces at end of string",
			input: "Hello world   ",
			want:  []string{"Hello ", "world   "},
		},
		{
			name:  "multiple words each with varying gaps",
			input: "a  b   c    d",
			want:  []string{"a  ", "b   ", "c    ", "d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WordSplitter(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitByWord(%q):\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSentenceSplitter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "basic multiple sentences with varying spacing",
			input: "Hello.   How are you?I'm fine!   Great.",
			want:  []string{"Hello.   ", "How are you?", "I'm fine!   ", "Great."},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "no terminal punctuation",
			input: "Hello world",
			want:  []string{"Hello world"},
		},
		{
			name:  "single sentence no trailing space",
			input: "Hello world.",
			want:  []string{"Hello world."},
		},
		{
			name:  "repeated punctuation ellipsis",
			input: "Wait... really?",
			want:  []string{"Wait... ", "really?"},
		},
		{
			name:  "mixed repeated punctuation",
			input: "Is that so?! Yes...   Indeed.",
			want:  []string{"Is that so?! ", "Yes...   ", "Indeed."},
		},
		{
			name:  "closing quote after period stays attached",
			input: `He said "stop." Then left.`,
			want:  []string{`He said "stop." `, "Then left."},
		},
		{
			name:  "closing paren after punctuation stays attached",
			input: "Done now (finally!) Next up.",
			want:  []string{"Done now (finally!) ", "Next up."},
		},
		{
			name:  "no space after sentence end still splits",
			input: "you?I'm fine",
			want:  []string{"you?", "I'm fine"},
		},
		{
			name:  "typographic closing quote",
			input: "She said “done.” Then left.",
			want:  []string{"She said “done.” ", "Then left."},
		},
		{
			name:  "multiple closers stacked",
			input: `He asked "really?" Then left.`,
			want:  []string{`He asked "really?" `, "Then left."},
		},
		{
			name:  "trailing sentence with no following text",
			input: "Just one sentence.",
			want:  []string{"Just one sentence."},
		},
		{
			name:  "only punctuation",
			input: "...",
			want:  []string{"..."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SentenceSplitter(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitBySentence(%q):\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}
