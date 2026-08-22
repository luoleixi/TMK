package evaluation

import "testing"

func TestCompareNormalizesCaseWhitespaceAndPunctuation(t *testing.T) {
	metrics := Compare("Hello, world!", "hello world")
	if metrics.CharDistance != 0 || metrics.CharUnits != 10 || metrics.WordDistance != 0 || metrics.WordUnits != 2 {
		t.Fatalf("metrics=%+v", metrics)
	}
	metrics = Compare("你好世界", "你好世")
	if metrics.CharDistance != 1 || metrics.CharUnits != 4 {
		t.Fatalf("Chinese metrics=%+v", metrics)
	}
}

func TestCompareCountsSubstitutionsInsertionsAndDeletions(t *testing.T) {
	tests := []struct {
		name       string
		reference  string
		hypothesis string
		charDist   int64
		wordDist   int64
	}{
		{name: "substitution", reference: "the quick fox", hypothesis: "the slow fox", charDist: 5, wordDist: 1},
		{name: "insertion", reference: "hello world", hypothesis: "hello brave world", charDist: 5, wordDist: 1},
		{name: "deletion", reference: "one two three", hypothesis: "one three", charDist: 3, wordDist: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			metrics := Compare(test.reference, test.hypothesis)
			if metrics.CharDistance != test.charDist || metrics.WordDistance != test.wordDist {
				t.Fatalf("metrics=%+v, want char distance %d and word distance %d", metrics, test.charDist, test.wordDist)
			}
		})
	}
}

func TestCompareHandlesEmptyReferenceAndUnicodePunctuation(t *testing.T) {
	empty := Compare("", "unexpected words")
	if empty.CharUnits != 0 || empty.CharDistance != 15 || empty.WordUnits != 0 || empty.WordDistance != 2 {
		t.Fatalf("empty reference metrics=%+v", empty)
	}
	punctuation := Compare("你好，世界！", "你好 世界")
	if punctuation.CharDistance != 0 || punctuation.WordDistance != 0 {
		t.Fatalf("Unicode punctuation metrics=%+v", punctuation)
	}
}

func TestEditDistanceUsesBoundedWorkingMemoryWithoutChangingResult(t *testing.T) {
	left := []rune("kitten")
	right := []rune("sitting")
	if got := editDistance(left, right); got != 3 {
		t.Fatalf("distance=%d, want 3", got)
	}
	if got := editDistance(right, left); got != 3 {
		t.Fatalf("reversed distance=%d, want 3", got)
	}
	if got := editDistance([]rune{}, []rune{}); got != 0 {
		t.Fatalf("empty distance=%d, want 0", got)
	}
}
