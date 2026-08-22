package observability

import "testing"

func TestEstimateTokensIsStableAndDoesNotDependOnByteWidth(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("empty token estimate=%v", got)
	}
	if got := EstimateTokens("abcd"); got != 1 {
		t.Fatalf("ascii token estimate=%v, want 1", got)
	}
	if got := EstimateTokens("你好世界"); got != 1 {
		t.Fatalf("unicode token estimate=%v, want 1", got)
	}
}
