package typosearch

import (
	"strings"
	"unicode"
)

// Splitter is function to split string into several groups. Will be run before
// [Processor] applied.
type Splitter func(string) []string

// SplitByWord splits input into words, keeping any spaces that follow a word
// attached to that word (rather than dropping them or starting a new segment).
// For example:
//
// Input: "Hello.   How are you?I'm fine!   Great."
//
// Output:
// - 0: "Hello.   "
// - 1: "How are you?"
// - 2: "I'm fine!   "
// - 3: "Great."
func WordSplitter(input string) []string {
	var result []string
	var current strings.Builder
	runes := []rune(input)
	hasWord := false // true once current segment contains a non-space rune

	for i, r := range runes {
		isSpace := unicode.IsSpace(r)

		// A new segment starts when we hit a non-space rune that comes
		// right after a space, but only if the current segment already
		// has a word in it (so leading spaces don't cause a false split).
		if !isSpace && hasWord && i > 0 && unicode.IsSpace(runes[i-1]) {
			result = append(result, current.String())
			current.Reset()
			hasWord = false
		}

		current.WriteRune(r)
		if !isSpace {
			hasWord = true
		}
	}

	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

var normalSentenceSplitter = CustomSentenceSplitter(nil)

// SentenceSplitter splits input into sentences, keeping any whitespace that
// follows a sentence-ending punctuation mark attached to that sentence.
// It also keeps repeated punctuation (e.g. "...", "?!") and trailing closing
// quotes/brackets (e.g. the `"` in `He said "stop."`) as part of the same
// sentence, rather than splitting mid-punctuation or leaking a quote mark
// into the next sentence.
// For example:
//
// Input: "Is that so?! Yes...   Indeed."
//
// Output:
// - 0: "Is that so?! "
// - 1: "Yes...   "
// - 2: "Indeed."
func SentenceSplitter(input string) []string {
	return normalSentenceSplitter(input)
}

// CustomSentenceSplitter is same as SentenceSplitter, except we cand use
// custom-defined separator.
func CustomSentenceSplitter(isSeparator func(rune) bool) func(string) []string {
	if isSeparator == nil {
		isSeparator = func(r rune) bool {
			return r == '.' || r == '!' || r == '?'
		}
	}

	return func(input string) []string {
		var result []string
		var current strings.Builder
		runes := []rune(input)
		sentenceEnded := false // true once we've seen separator and are still in the "ending" run

		isClosing := func(r rune) bool {
			switch r {
			case '"', '\'', ')', ']', '}', '”', '’', '»':
				return true
			}
			return false
		}

		for _, r := range runes {
			if sentenceEnded {
				switch {
				case unicode.IsSpace(r), isSeparator(r), isClosing(r):
					// Still part of the sentence's "ending sequence":
					// more punctuation, a closing quote/bracket, or trailing whitespace.
					current.WriteRune(r)
					continue
				default:
					// A real new sentence starts here.
					result = append(result, current.String())
					current.Reset()
					sentenceEnded = false
				}
			}

			current.WriteRune(r)
			if isSeparator(r) {
				sentenceEnded = true
			}
		}

		if current.Len() > 0 {
			result = append(result, current.String())
		}

		return result
	}
}

func defaultSplitter(s string) []string {
	return []string{s}
}
