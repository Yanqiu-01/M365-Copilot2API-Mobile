package chathub

import (
	"encoding/json"
	"testing"
)

func TestReasoningFromFramesAPKShapes(t *testing.T) {
	frames := []json.RawMessage{
		json.RawMessage(`{"type":1,"target":"update","arguments":[{"text":"plan ","contentOrigin":"ChainOfThoughtSummary"}]}`),
		json.RawMessage(`{"item":{"messages":[{"text":"check","addToChainOfThought":true}]}}`),
		json.RawMessage(`{"messages":[{"text":"visible answer"}]}`),
		json.RawMessage(`not-json`),
	}
	if got, want := reasoningFromFrames(frames), "plan check"; got != want {
		t.Fatalf("reasoningFromFrames() = %q, want %q", got, want)
	}
}

func TestCandidateMessagesAcceptsDirectArguments(t *testing.T) {
	frame := map[string]any{
		"arguments": []any{map[string]any{"text": "direct", "contentOrigin": "ChainOfThoughtSummary"}},
	}
	messages := candidateMessages(frame)
	if len(messages) != 1 || messages[0]["text"] != "direct" {
		t.Fatalf("candidateMessages() = %#v", messages)
	}
}
