package web

import (
	"strings"
	"testing"
)

func streamTestTools(names ...string) []map[string]any {
	tools := make([]map[string]any, 0, len(names))
	for _, name := range names {
		tools = append(tools, map[string]any{"type": "function", "function": map[string]any{"name": name}})
	}
	return tools
}

func TestStreamTextSuppressesDeclaredFenceAcrossFragments(t *testing.T) {
	var pending strings.Builder
	var emitted strings.Builder
	emit := func(part string) error { emitted.WriteString(part); return nil }
	tools := streamTestTools("bash")

	for _, fragment := range []string{"Preparing now.\n```ba", "sh\n{\"command\":\"echo hi\"}", "\n```"} {
		if err := streamTextWithToolLookahead(&pending, fragment, tools, "auto", emit); err != nil {
			t.Fatal(err)
		}
	}
	if err := flushStreamText(&pending, tools, "auto", true, emit); err != nil {
		t.Fatal(err)
	}
	if got := emitted.String(); strings.Contains(got, "echo hi") || strings.Contains(got, "```bash") {
		t.Fatalf("declared tool block leaked: %q", got)
	}
	if !strings.Contains(emitted.String(), "Preparing now.") {
		t.Fatalf("prose was lost: %q", emitted.String())
	}
}

func TestStreamTextLeavesOrdinaryCodeFenceVisible(t *testing.T) {
	var pending strings.Builder
	var emitted strings.Builder
	emit := func(part string) error { emitted.WriteString(part); return nil }
	text := "Example:\n```go\nfmt.Println(\"ok\")\n```"
	if err := streamTextWithToolLookahead(&pending, text, streamTestTools("bash"), "auto", emit); err != nil {
		t.Fatal(err)
	}
	if err := flushStreamText(&pending, streamTestTools("bash"), "auto", true, emit); err != nil {
		t.Fatal(err)
	}
	if got := emitted.String(); got != text {
		t.Fatalf("ordinary code block changed: %q", got)
	}
}

func TestStreamTextPreservesUTF8Tail(t *testing.T) {
	var pending strings.Builder
	var emitted strings.Builder
	emit := func(part string) error { emitted.WriteString(part); return nil }
	text := "你好，流式输出必须保持 UTF-8 边界。"
	if err := streamTextWithToolLookahead(&pending, text, nil, "auto", emit); err != nil {
		t.Fatal(err)
	}
	if err := flushStreamText(&pending, nil, "auto", true, emit); err != nil {
		t.Fatal(err)
	}
	if got := emitted.String(); got != text {
		t.Fatalf("UTF-8 text changed: %q", got)
	}
}

func TestStreamTextFinalFlushKeepsMalformedDeclaredFence(t *testing.T) {
	var pending strings.Builder
	var emitted strings.Builder
	emit := func(part string) error { emitted.WriteString(part); return nil }
	text := "I tried:\n```bash\nnot valid json"
	if err := streamTextWithToolLookahead(&pending, text, streamTestTools("bash"), "auto", emit); err != nil {
		t.Fatal(err)
	}
	if err := flushStreamText(&pending, streamTestTools("bash"), "auto", true, emit); err != nil {
		t.Fatal(err)
	}
	if got := emitted.String(); got != text {
		t.Fatalf("malformed fence must remain visible: %q", got)
	}
}

func TestDeclaredFenceStartSkipsUndeclaredFence(t *testing.T) {
	if got := declaredFenceStart("```python\nprint(1)", streamTestTools("bash"), "auto"); got != -1 {
		t.Fatalf("undeclared fence start=%d", got)
	}
	if got := declaredFenceStart("```ba", streamTestTools("bash"), "auto"); got != 0 {
		t.Fatalf("partial declared fence start=%d", got)
	}
}
