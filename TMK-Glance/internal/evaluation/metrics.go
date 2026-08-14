package evaluation

import (
	"strings"
	"unicode"
)

type Metrics struct {
	CharDistance int64
	CharUnits    int64
	WordDistance int64
	WordUnits    int64
}

func Compare(reference, hypothesis string) Metrics {
	referenceChars := normalizedChars(reference)
	hypothesisChars := normalizedChars(hypothesis)
	referenceWords := normalizedWords(reference)
	hypothesisWords := normalizedWords(hypothesis)
	return Metrics{
		CharDistance: int64(editDistance(referenceChars, hypothesisChars)),
		CharUnits:    int64(len(referenceChars)),
		WordDistance: int64(editDistance(referenceWords, hypothesisWords)),
		WordUnits:    int64(len(referenceWords)),
	}
}

func normalizedChars(value string) []rune {
	result := make([]rune, 0, len([]rune(value)))
	for _, r := range strings.ToLower(value) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		result = append(result, r)
	}
	return result
}

func normalizedWords(value string) []string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return ' '
		}
		return unicode.ToLower(r)
	}, value)
	return strings.Fields(value)
}

func editDistance[T comparable](left, right []T) int {
	if len(left) < len(right) {
		left, right = right, left
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for i := range previous {
		previous[i] = i
	}
	for i, a := range left {
		current[0] = i + 1
		for j, b := range right {
			cost := 0
			if a != b {
				cost = 1
			}
			current[j+1] = min3(current[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(right)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
