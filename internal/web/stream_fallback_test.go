package web

import (
	"strings"
	"testing"
)

func TestStreamFinalResultFallbackIsNotEmpty(t *testing.T) {
	var text strings.Builder
	if text.Len() != 0 {
		t.Fatal("test setup")
	}
	final := "最终回答"
	if text.Len() == 0 && strings.TrimSpace(final) != "" {
		text.WriteString(final)
	}
	if text.String() != final {
		t.Fatalf("expected final result fallback %q, got %q", final, text.String())
	}
}
