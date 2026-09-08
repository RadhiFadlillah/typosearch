package typosearch

import "slices"

// trigrams splits an array into trigrams.
func trigrams[T any](array []T) [][]T {
	// N = 3 for trigram. Change it to 2 for bigram.
	n := 3

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
