package typosearch

import (
	"unicode"
)

// Processor is function to process a rune into another rune(s). The result also can
// be zero, if that rune is supposed to be removed.
type Processor func(r rune) []rune

// LowerCaser is processor that change a rune into its lower case form.
func LowerCaser() Processor {
	return func(r rune) []rune {
		return []rune{unicode.ToLower(r)}
	}
}

// UpperCaser is processor that change a rune into its upper case form.
func UpperCaser() Processor {
	return func(r rune) []rune {
		return []rune{unicode.ToUpper(r)}
	}
}

// RuneRemover is processor that removes every rune that submitted in parameter.
func RuneRemover(removableRunes ...rune) Processor {
	mapRemovableRunes := make(map[rune]struct{})
	for _, r := range removableRunes {
		mapRemovableRunes[r] = struct{}{}
	}

	return func(r rune) []rune {
		if _, exist := mapRemovableRunes[r]; exist {
			return nil
		}
		return []rune{r}
	}
}

// RuneRemoverFn is processor that accepts a filter function. Every rune that
// returned true by filter will be removed.
func RuneRemoverFn(filter func(r rune) bool) Processor {
	return func(r rune) []rune {
		if filter(r) {
			return nil
		}
		return []rune{r}
	}
}
