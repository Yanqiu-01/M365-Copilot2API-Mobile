package web

import (
	"strings"
	"testing"
)

func TestToolPlanningModeDefaultsToRouter(t *testing.T) {
	for _, raw := range []string{"", "router", "ROUTER", "unexpected"} {
		if got := toolPlanningMode(raw); got != "router" {
			t.Fatalf("toolPlanningMode(%q)=%q, want router", raw, got)
		}
	}
}

func TestToolPlanningModeAcceptsNative(t *testing.T) {
	if got := toolPlanningMode(" native "); got != "native" {
		t.Fatalf("toolPlanningMode(native)=%q, want native", got)
	}
}

func TestUnavailableModelReply(t *testing.T) {
	for _, text := range []string{
		"抱歉，我无法响应。",
		"抱歉，我无法回答这个请求。",
		"I'm sorry, I can't respond to this request.",
	} {
		if !isUnavailableModelReply(text) {
			t.Errorf("refusal not recognized: %q", text)
		}
	}
	for _, text := range []string{
		"这是正常的详细回答，不是路由拒绝。",
		"我无法响应这么多请求，请稍后重试。",                                           // rate limiting has its own path
		"I'm sorry, I can't respond to this many requests right now.", // English rate limiting
		strings.Repeat("抱歉，我无法响应。", 30),                               // long real answer must not be classified by prefix alone
	} {
		if isUnavailableModelReply(text) {
			t.Errorf("normal/rate-limit text misclassified: %q", text)
		}
	}
}
