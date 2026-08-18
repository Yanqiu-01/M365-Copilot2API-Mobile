package web

import (
	"strings"
	"unicode/utf8"
)

// streamTextWithToolLookahead holds a small trailing window of a streamed
// assistant response. This prevents a declared fenced tool call split across
// SignalR text fragments from being emitted to the user before it can be
// converted into an OpenAI tool call.
//
// APK evidence: stream_text.go calls flushStreamText, which in turn calls
// declaredFenceStart and fencedToolCalls.
func streamTextWithToolLookahead(pending *strings.Builder, fragment string, tools []map[string]any, choice any, emit func(string) error) error {
	if fragment == "" {
		return nil
	}
	pending.WriteString(fragment)
	return flushStreamText(pending, tools, choice, false, emit)
}

// flushStreamText emits safe prose from pending. A complete, declared fenced
// tool call is removed from user-visible output; the caller retains the full
// raw stream separately and later converts it with fencedToolCalls.
func flushStreamText(pending *strings.Builder, tools []map[string]any, choice any, final bool, emit func(string) error) error {
	for pending.Len() > 0 {
		text := pending.String()
		if start := declaredFenceStart(text, tools, choice); start >= 0 {
			if start > 0 {
				if err := emit(text[:start]); err != nil {
					return err
				}
				pending.Reset()
				pending.WriteString(text[start:])
				continue
			}

			if end := completeFenceEnd(text); end > 0 {
				block := text[:end]
				if len(fencedToolCalls(block, tools, choice)) > 0 {
					pending.Reset()
					pending.WriteString(text[end:])
					continue
				}
			}
			// A declared fence that has not completed must remain buffered until
			// more upstream text arrives. At final flush, retain it as prose so a
			// malformed model response is not silently lost.
			if !final {
				return nil
			}
		}

		if final {
			pending.Reset()
			return emit(text)
		}
		cut := keepRuneTail(text, 8)
		if cut == 0 {
			return nil
		}
		if err := emit(text[:cut]); err != nil {
			return err
		}
		pending.Reset()
		pending.WriteString(text[cut:])
	}
	return nil
}

// declaredFenceStart returns the first fence whose language is a declared
// client tool (or a shell alias convertible to one). Ordinary markdown code
// fences intentionally return -1 and continue to stream as visible prose.
func declaredFenceStart(text string, tools []map[string]any, choice any) int {
	allowed := allowedToolNames(tools)
	shell := declaredShell(allowed)
	for offset := 0; ; {
		index := strings.Index(text[offset:], "```")
		if index < 0 {
			return -1
		}
		index += offset
		after := text[index+3:]
		lineEnd := strings.IndexByte(after, '\n')
		if lineEnd < 0 {
			// Buffer only a partial header that could still become a declared
			// tool. Ordinary partial markdown fences should remain streamable.
			partial := strings.ToLower(strings.TrimSpace(after))
			if declaredFencePrefix(partial, allowed, shell) {
				return index
			}
			offset = index + 3
			continue
		}
		name := strings.ToLower(strings.TrimSpace(after[:lineEnd]))
		if name != "" && toolChoiceAllows(choice, name) && allowed[name] {
			return index
		}
		if (name == "bash" || name == "sh" || name == "shell" || name == "powershell" || name == "cmd") && shell != "" {
			return index
		}
		offset = index + 3
	}
}

func declaredFencePrefix(partial string, allowed map[string]bool, shell string) bool {
	if partial == "" {
		return true
	}
	for name := range allowed {
		if strings.HasPrefix(name, partial) {
			return true
		}
	}
	for _, name := range []string{"bash", "sh", "shell", "powershell", "cmd"} {
		if shell != "" && strings.HasPrefix(name, partial) {
			return true
		}
	}
	return false
}

func completeFenceEnd(text string) int {
	if !strings.HasPrefix(text, "```") {
		return 0
	}
	lineEnd := strings.IndexByte(text[3:], '\n')
	if lineEnd < 0 {
		return 0
	}
	bodyStart := 3 + lineEnd + 1
	if close := strings.Index(text[bodyStart:], "\n```"); close >= 0 {
		return bodyStart + close + len("\n```")
	}
	return 0
}

// keepRuneTail returns the byte index that leaves the requested rune tail in
// the buffer, preserving UTF-8 boundaries across SignalR fragments.
func keepRuneTail(text string, tail int) int {
	if tail <= 0 || utf8.RuneCountInString(text) <= tail {
		return 0
	}
	remaining := utf8.RuneCountInString(text) - tail
	for index := range text {
		if remaining == 0 {
			return index
		}
		remaining--
	}
	return 0
}
