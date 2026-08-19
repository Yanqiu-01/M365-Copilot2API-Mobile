package web

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// Resolve 的相似度兜底：matchedBy 必须是 context_similar_%.2f 形式，
// 且只在同一 IP/UA 指纹下命中。
func TestResolveSimilarFallbackFormatAndFingerprint(t *testing.T) {
	t.Setenv("M365_CONTEXT_SIMILARITY", "0.6")
	hist := []oaiMsg{
		{Role: "user", Content: "alpha beta gamma delta"},
		{Role: "assistant", Content: "epsilon zeta eta theta"},
	}
	newResolver := func() *sessionResolver {
		sr := &sessionResolver{
			sessions:    map[string]sessionBinding{},
			byExplicit:  map[string]string{},
			ttl:         2 * time.Hour,
			contextTTL:  2 * time.Hour,
			maxSessions: defaultMaxSessions,
		}
		sr.persist = &persistStore{flush: func() error { return nil }}
		return sr
	}
	req := func(ua string) *http.Request {
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		r.RemoteAddr = "10.0.0.1:1234"
		r.Header.Set("User-Agent", ua)
		return r
	}

	// 相似但非严格前缀：删掉首条、改动尾部措辞
	partial := []oaiMsg{
		{Role: "assistant", Content: "epsilon zeta eta theta"},
		{Role: "user", Content: "alpha beta gamma"},
	}

	sr := newResolver()
	r := req("agent/1.0")
	sr.sessions["s1"] = sessionBinding{
		SessionID:      "s1",
		ConversationID: "c1",
		AccountID:      "a1",
		IPFingerprint:  clientIPFingerprint(r),
		ContextHistory: hist,
		LastUsedAt:     time.Now().UTC(),
	}
	got := sr.Resolve(r, &oaiReq{Messages: partial})
	if got.IsNew || got.SessionID != "s1" {
		t.Fatalf("expected similar match on s1, got %#v", got)
	}
	if !strings.HasPrefix(got.MatchedBy, "context_similar_") {
		t.Fatalf("matchedBy=%q want context_similar_ prefix", got.MatchedBy)
	}
	// %.2f 形式：前缀后应是形如 0.83 的两位小数
	suffix := strings.TrimPrefix(got.MatchedBy, "context_similar_")
	if _, err := strconv.ParseFloat(suffix, 64); err != nil {
		t.Fatalf("matchedBy suffix %q not a float: %v", suffix, err)
	}
	if len(suffix) < 3 || suffix[1] != '.' || len(suffix)-2 != 2 {
		t.Fatalf("matchedBy suffix %q is not %%.2f formatted", suffix)
	}
	if got.HistoryLen != 0 {
		t.Fatalf("similar fallback HistoryLen=%d want 0", got.HistoryLen)
	}

	// 不同 UA -> 指纹不同 -> 不得命中
	sr2 := newResolver()
	sr2.sessions["s1"] = sessionBinding{
		SessionID:      "s1",
		IPFingerprint:  clientIPFingerprint(req("agent/1.0")),
		ContextHistory: hist,
		LastUsedAt:     time.Now().UTC(),
	}
	if got := sr2.Resolve(req("other/2.0"), &oaiReq{Messages: partial}); !got.IsNew {
		t.Fatalf("fingerprint mismatch must not match, got %#v", got)
	}

	// 阈值抬高到 1.0 后，非全等内容不得命中
	t.Setenv("M365_CONTEXT_SIMILARITY", "1")
	sr3 := newResolver()
	r3 := req("agent/1.0")
	sr3.sessions["s1"] = sessionBinding{
		SessionID:      "s1",
		IPFingerprint:  clientIPFingerprint(r3),
		ContextHistory: hist,
		LastUsedAt:     time.Now().UTC(),
	}
	if got := sr3.Resolve(r3, &oaiReq{Messages: partial}); !got.IsNew {
		t.Fatalf("threshold=1 must reject partial match, got %#v", got)
	}
}

// 阈值经 M365_CONTEXT_SIMILARITY 覆盖，且对非法值回退默认 0.6。
// APK 证据：默认值取自 0x5be4d0 = 0x3fe3333333333333；
// FMOV d1,#1.0 + FCMP/B.LS 限定上界为 1.0。
func TestResolveSimilarThresholdEnvOverride(t *testing.T) {
	hist := []oaiMsg{
		{Role: "user", Content: "alpha beta gamma delta"},
		{Role: "assistant", Content: "epsilon zeta eta theta"},
	}
	// 与 hist 相似度 0.5，低于默认 0.6
	weak := []oaiMsg{{Role: "assistant", Content: "epsilon zeta eta theta"}}

	probe := func(env string, msgs []oaiMsg) ResolveResult {
		t.Setenv("M365_CONTEXT_SIMILARITY", env)
		sr := &sessionResolver{
			sessions:    map[string]sessionBinding{},
			byExplicit:  map[string]string{},
			ttl:         2 * time.Hour,
			contextTTL:  2 * time.Hour,
			maxSessions: defaultMaxSessions,
		}
		sr.persist = &persistStore{flush: func() error { return nil }}
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		r.RemoteAddr = "10.0.0.9:5555"
		r.Header.Set("User-Agent", "probe/1.0")
		sr.sessions["s1"] = sessionBinding{
			SessionID:      "s1",
			IPFingerprint:  clientIPFingerprint(r),
			ContextHistory: hist,
			LastUsedAt:     time.Now().UTC(),
		}
		return sr.Resolve(r, &oaiReq{Messages: msgs})
	}

	// 默认 0.6：0.5 分的样本不命中
	if got := probe("", weak); !got.IsNew {
		t.Fatalf("default threshold should reject 0.5 sample, got %#v", got)
	}
	// 放宽到 0.4：命中
	if got := probe("0.4", weak); got.IsNew {
		t.Fatalf("threshold 0.4 should accept 0.5 sample, got %#v", got)
	}
	// 非法值一律回退默认 0.6，故仍不命中
	for _, bad := range []string{"0", "-1", "1.5", "NaN", "abc"} {
		if got := probe(bad, weak); !got.IsNew {
			t.Fatalf("invalid threshold %q must fall back to 0.6 and reject, got %#v", bad, got)
		}
	}
}
