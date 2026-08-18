package web

import (
	"strings"
	"testing"
)

func TestRouterMaxGroupsAPKEnvironment(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 40}, {"bad", 40}, {"-1", 40}, {"0", 0}, {"1", 1}, {"41", 41},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("M365_ROUTER_MAX_MESSAGES", test.value)
			if got := routerMaxGroups(); got != test.want {
				t.Fatalf("routerMaxGroups()=%d want %d", got, test.want)
			}
		})
	}
}

func routerTrimFixture() []oaiMsg {
	return []oaiMsg{
		{Role: "system", Content: "system rule"},
		{Role: "developer", Content: "developer rule"},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
		{Role: "assistant", Content: "second answer"},
		{Role: "user", Content: "latest question"},
		{Role: "assistant", Content: "latest answer"},
	}
}

func TestTrimHistoryForRouterKeepsInstructionsAndNewestGroups(t *testing.T) {
	t.Setenv("M365_ROUTER_MAX_MESSAGES", "2")
	trimmed := trimHistoryForRouter(routerTrimFixture())
	if len(trimmed) != 6 {
		t.Fatalf("len=%d messages=%#v", len(trimmed), trimmed)
	}
	if trimmed[0].Role != "system" || trimmed[1].Role != "developer" {
		t.Fatalf("instructions missing: %#v", trimmed[:2])
	}
	for _, forbidden := range []string{"first question", "first answer"} {
		for _, message := range trimmed {
			if message.Content == forbidden {
				t.Fatalf("old group survived: %#v", trimmed)
			}
		}
	}
	if trimmed[2].Content != "second question" || trimmed[4].Content != "latest question" {
		t.Fatalf("newest groups wrong: %#v", trimmed)
	}
}

func TestTrimHistoryForRouterZeroDisablesLimit(t *testing.T) {
	t.Setenv("M365_ROUTER_MAX_MESSAGES", "0")
	messages := routerTrimFixture()
	trimmed := trimHistoryForRouter(messages)
	if len(trimmed) != len(messages) {
		t.Fatalf("zero must disable trimming: %d != %d", len(trimmed), len(messages))
	}
}

func TestRouterPromptMessagesUsesTrimmedHistory(t *testing.T) {
	t.Setenv("M365_ROUTER_MAX_MESSAGES", "1")
	prompt := routerPromptMessages(routerTrimFixture())
	if strings.Contains(prompt, "first question") || strings.Contains(prompt, "second question") {
		t.Fatalf("old router history leaked: %q", prompt)
	}
	for _, required := range []string{"system rule", "developer rule", "latest question", "latest answer"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("missing %q from router prompt: %q", required, prompt)
		}
	}
}
