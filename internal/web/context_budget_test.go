package web

import (
	"encoding/json"
	"strings"
	"testing"

	"m365-copilot2api/internal/chathub"
)

func TestConfiguredContextBudgetReservesOutput(t *testing.T) {
	settings := currentSettings()
	originalWindow, originalOutput := settings.ContextWindow, settings.MaxOutputTokens
	store := openSettingsStore()
	store.mu.Lock()
	store.v.ContextWindow = 1000
	store.v.MaxOutputTokens = 200
	store.mu.Unlock()
	t.Cleanup(func() {
		store.mu.Lock()
		store.v.ContextWindow = originalWindow
		store.v.MaxOutputTokens = originalOutput
		store.mu.Unlock()
	})
	if got := configuredContextBudget(); got != 127800 { // invalid window falls back to 128000; configured output remains reserved
		t.Fatalf("configuredContextBudget()=%d", got)
	}
}

func TestMessageGroupsPreserveInstructionsAndTurnBoundaries(t *testing.T) {
	messages := []oaiMsg{
		{Role: "system", Content: "always be precise"},
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "new question"},
		{Role: "assistant", Content: "new answer"},
	}
	instructions, groups := messageGroups(messages, "gpt-5.6")
	if len(instructions) != 1 || instructions[0].Role != "system" {
		t.Fatalf("instructions=%#v", instructions)
	}
	if len(groups) != 2 || len(groups[0].messages) != 2 || len(groups[1].messages) != 2 {
		t.Fatalf("groups=%#v", groups)
	}
}

func TestTrimMessagesWithBudgetKeepsNewestCompleteTurn(t *testing.T) {
	messages := []oaiMsg{
		{Role: "system", Content: "keep this instruction"},
		{Role: "user", Content: strings.Repeat("old ", 120)},
		{Role: "assistant", Content: strings.Repeat("old answer ", 120)},
		{Role: "user", Content: "latest question"},
		{Role: "assistant", Content: "latest answer"},
	}
	got, err := trimMessagesWithBudget(messages, nil, nil, "", 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Role != "system" || got[1].Content != "latest question" || got[2].Content != "latest answer" {
		t.Fatalf("trimmed=%#v", got)
	}
}

func TestTrimMessagesCountsToolSchemas(t *testing.T) {
	function, _ := json.Marshal(map[string]any{"name": "large", "description": strings.Repeat("schema ", 100), "parameters": map[string]any{"type": "object"}})
	tools := []chathub.Tool{{Type: "function", Function: function}}
	messages := []oaiMsg{{Role: "user", Content: "hello"}}
	if _, err := trimMessagesWithBudget(messages, tools, "auto", "gpt-5.6", 10); err == nil {
		t.Fatal("expected tool schema to consume the budget")
	}
}

func TestTrimMessagesRejectsInstructionOverflow(t *testing.T) {
	messages := []oaiMsg{{Role: "system", Content: strings.Repeat("instruction ", 100)}}
	if _, err := trimMessagesWithBudget(messages, nil, nil, "", 5); err == nil {
		t.Fatal("expected instruction overflow")
	}
}
