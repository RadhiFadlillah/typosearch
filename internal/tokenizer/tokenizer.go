package tokenizer

import "slices"

// Tokenize runs processor on the s, then separate it into trigram runes.
func Tokenize(s string, processor func(r rune) []rune) []ProcessedRuneGroup {
	processedRunes := ProcessRunes(s, processor)
	trigrams := NGrams(processedRunes, 3)

	result := make([]ProcessedRuneGroup, len(trigrams))
	for i := range result {
		result[i] = ProcessedRuneGroup(trigrams[i])
	}

	return result
}

// NGrams splits an array into n-grams of specified size.
func NGrams[T any](array []T, n int) [][]T {
	// Make sure n is not zero
	if n <= 0 {
		return nil
	}

	// Make sure array is longer than n
	if len(array) < n {
		return nil
	}

	// Pre-allocate result with exact capacity needed
	size := len(array) - n + 1
	ngrams := make([][]T, 0, size)

	for i := 0; i <= len(array)-n; i++ {
		// Here we clone the array subset so we won't accidentally modified
		// the original array later.
		ngram := slices.Clone(array[i : i+n])
		ngrams = append(ngrams, ngram)
	}

	return ngrams
}
