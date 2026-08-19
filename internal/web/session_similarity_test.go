package web

import (
	"math"
	"testing"
	"time"
)

func TestTokenizeLowercasesAndSplitsOnWhitespace(t *testing.T) {
	got := tokenize("  Hello   World\tFoo\nBar  ")
	want := []string{"hello", "world", "foo", "bar"}
	if len(got) != len(want) {
		t.Fatalf("tokenize len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tokenize[%d]=%q want %q", i, got[i], want[i])
		}
	}
	if len(tokenize("")) != 0 {
		t.Fatal("empty input must yield no tokens")
	}
	if len(tokenize("   \t\n ")) != 0 {
		t.Fatal("whitespace-only input must yield no tokens")
	}
}

func TestJaccardSimilarityBounds(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 1},
		{"disjoint", []string{"a", "b"}, []string{"c", "d"}, 0},
		{"half", []string{"a", "b"}, []string{"b", "c"}, 1.0 / 3.0},
		{"emptyA", nil, []string{"a"}, 0},
		{"emptyB", []string{"a"}, nil, 0},
		{"dupesCollapse", []string{"a", "a", "b"}, []string{"a", "b", "b"}, 1},
	}
	for _, tc := range cases {
		got := jaccardSimilarity(tc.a, tc.b)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
		if got < 0 || got > 1 {
			t.Errorf("%s: similarity %v out of [0,1]", tc.name, got)
		}
	}
}

func TestJaccardSimilarityIsSymmetric(t *testing.T) {
	a := []string{"user", "hello", "assistant", "hi"}
	b := []string{"user", "hello", "assistant", "there"}
	if jaccardSimilarity(a, b) != jaccardSimilarity(b, a) {
		t.Fatal("jaccard must be symmetric")
	}
}

func TestContextSimilarityIdenticalAndDivergent(t *testing.T) {
	hist := []oaiMsg{
		{Role: "user", Content: "deploy the staging cluster"},
		{Role: "assistant", Content: "starting rollout now"},
	}
	if got := contextSimilarity(hist, hist); math.Abs(got-1) > 1e-9 {
		t.Fatalf("identical context similarity=%v want 1", got)
	}

	// A locally truncated history should still score high.
	truncated := hist[1:]
	if got := contextSimilarity(hist, truncated); got <= 0 {
		t.Fatalf("truncated history similarity=%v want >0", got)
	}

	divergent := []oaiMsg{{Role: "user", Content: "unrelated billing question"}}
	if got := contextSimilarity(hist, divergent); got > 0.5 {
		t.Fatalf("divergent similarity=%v want <=0.5", got)
	}
}

func TestContextSimilarityEmptyInputs(t *testing.T) {
	msgs := []oaiMsg{{Role: "user", Content: "x"}}
	if contextSimilarity(nil, msgs) != 0 {
		t.Fatal("empty hist must score 0")
	}
	if contextSimilarity(msgs, nil) != 0 {
		t.Fatal("empty msgs must score 0")
	}
	if contextSimilarity(nil, nil) != 0 {
		t.Fatal("both empty must score 0")
	}
}

// APK 的 matchedBy 前缀只有 context_prefix_ 与 context_similar_，
// 不存在 context_suffix_；相似度兜底必须使用前者之外的 similar 语义。
func TestMatchSimilarLockedRespectsThresholdAndFingerprint(t *testing.T) {
	sr := &sessionResolver{
		sessions:   map[string]sessionBinding{},
		contextTTL: 2 * time.Hour,
	}
	hist := []oaiMsg{
		{Role: "user", Content: "alpha beta gamma delta"},
		{Role: "assistant", Content: "epsilon zeta eta theta"},
	}
	sr.sessions["s1"] = sessionBinding{
		SessionID:      "s1",
		IPFingerprint:  "fp-a",
		ContextHistory: hist,
		LastUsedAt:     time.Now().UTC(),
	}

	// Same fingerprint, identical content -> match at 100.
	id, pct := sr.matchSimilarLocked("fp-a", hist)
	if id != "s1" || pct != 100 {
		t.Fatalf("expected s1/100, got %q/%d", id, pct)
	}

	// Different fingerprint must not match even with identical content.
	if id, _ := sr.matchSimilarLocked("fp-b", hist); id != "" {
		t.Fatalf("fingerprint mismatch must not match, got %q", id)
	}

	// Unrelated content falls below threshold.
	if id, _ := sr.matchSimilarLocked("fp-a", []oaiMsg{{Role: "user", Content: "totally different words here"}}); id != "" {
		t.Fatalf("below-threshold content must not match, got %q", id)
	}

	// Empty request never matches.
	if id, _ := sr.matchSimilarLocked("fp-a", nil); id != "" {
		t.Fatalf("empty messages must not match, got %q", id)
	}
}
