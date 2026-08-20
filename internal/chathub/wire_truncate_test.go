package chathub

import (
	"encoding/json"
	"strings"
	"testing"
)

// 超过 wireFrameMaxBytes 的帧必须截断保留，不得整帧丢弃。
//
// 此前 sanitizeWirePayload 在编码结果超过 4096 字节时直接返回空串，而评测
// 以及任何携带工具 schema 的请求 payload 都远超该值（用户实测单帧 12856
// 字节）。结果是「捕获开启、帧数不为 0、payloadBytes 有值，但内容为空」，
// 恰好在最需要证据的场景下拿不到任何内容。
func TestSanitizeWirePayloadTruncatesInsteadOfDiscarding(t *testing.T) {
	big := strings.Repeat("x", 20000)
	payload, err := json.Marshal(map[string]any{
		"arguments": []any{map[string]any{"text": big, "access_token": "secret-value"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := sanitizeWirePayload(string(payload) + rs)
	if got == "" {
		t.Fatal("oversized frame must be truncated, not discarded")
	}
	if strings.Contains(got, "secret-value") {
		t.Error("credentials must still be redacted after truncation")
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("truncated frame must say so, got tail %q", got[max(0, len(got)-60):])
	}
	// 截断后长度受控，但保留了可读内容。
	if len(got) > wireFrameMaxBytes+64 {
		t.Errorf("truncated frame length=%d exceeds budget", len(got))
	}
	if !strings.Contains(got, "xxxx") {
		t.Error("truncated frame must keep the leading payload content")
	}
}

// 未超限的帧保持原样，且凭据仍被删除。
func TestSanitizeWirePayloadKeepsSmallFramesIntact(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"arguments": []any{map[string]any{"tone": "Gpt_5_6_Reasoning", "access_token": "secret-value"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := sanitizeWirePayload(string(payload) + rs)
	if got == "" {
		t.Fatal("small frame must be retained")
	}
	if strings.Contains(got, "secret-value") {
		t.Error("access_token must be redacted")
	}
	if strings.Contains(got, "truncated") {
		t.Error("small frame must not be marked truncated")
	}
	if !strings.Contains(got, "Gpt_5_6_Reasoning") {
		t.Error("small frame must keep its diagnostic content")
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
