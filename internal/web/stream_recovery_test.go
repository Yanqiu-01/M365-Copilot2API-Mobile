package web

import (
	"strings"
	"testing"
)

func TestStreamRecoveryPromptAPKLayoutAndTail(t *testing.T) {
	prompt := streamRecoveryPrompt("  delivered text  ")
	if !strings.HasPrefix(prompt, streamRecoveryPrefix+"delivered text"+streamRecoverySuffix) {
		t.Fatalf("unexpected prompt=%q", prompt)
	}

	large := strings.Repeat("a", streamRecoveryTailBytes+100) + "考"
	prompt = streamRecoveryPrompt(large)
	if !strings.Contains(prompt, "<delivered>\n"+strings.Repeat("a", streamRecoveryTailBytes-3)+"考\n</delivered>") {
		t.Fatalf("tail was not retained safely: len=%d", len(prompt))
	}
}

func TestTrimTrailingIncompleteRune(t *testing.T) {
	input := "hello考"[:7] // deliberately cuts the three-byte rune after its first byte.
	if got, want := trimTrailingIncompleteRune(input), "hello"; got != want {
		t.Fatalf("trimTrailingIncompleteRune(%q)=%q want %q", input, got, want)
	}
}

func TestRetryTextReconcilerFullReplay(t *testing.T) {
	r := &retryTextReconciler{delivered: "hello world"}
	if got := r.push("hello ", false); got != "" {
		t.Fatalf("first replay fragment=%q", got)
	}
	if got := r.push("world again", false); got != " again" {
		t.Fatalf("replayed prefix result=%q", got)
	}
}

func TestRetryTextReconcilerSuffixOverlap(t *testing.T) {
	r := &retryTextReconciler{delivered: "first line\nsecond line\nthird line"}
	if got := r.push("second line\nthird line\nfourth", false); got != "\nfourth" {
		t.Fatalf("suffix overlap result=%q", got)
	}
}

func TestRetryTextReconcilerShortFreshContinuation(t *testing.T) {
	r := &retryTextReconciler{delivered: "already sent"}
	if got := r.push("next", false); got != "" {
		t.Fatalf("short nonfinal fragment=%q", got)
	}
	if got := r.push("", true); got != "next" {
		t.Fatalf("final short continuation=%q", got)
	}
}

func TestRetryTextReconcilerDoesNotBreakUTF8Overlap(t *testing.T) {
	r := &retryTextReconciler{delivered: "甲乙丙丁戊己庚辛壬癸"}
	got := r.push("丁戊己庚辛壬癸子丑", false)
	if got != "子丑" {
		t.Fatalf("UTF-8 overlap result=%q", got)
	}
}
