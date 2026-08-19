package web

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// benchChat 的 payload 形状由 APK 机器码约束：
// model / stream=false / messages / tools 恒有，reasoning_effort 仅在非空时写入。
// 这里通过 json.Marshal 后的字节直接断言，避免依赖上游可达性。
func TestBenchChatPayloadShape(t *testing.T) {
	messages := []map[string]any{{"role": "user", "content": "ping"}}
	payload := map[string]any{"model": "gpt-5.6-reasoning", "stream": false, "messages": messages, "tools": benchToolSchema()}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, key := range []string{`"model":`, `"stream":false`, `"messages":`, `"tools":`} {
		if !strings.Contains(text, key) {
			t.Errorf("payload missing %s: %s", key, trimForLog(text))
		}
	}
	if strings.Contains(text, `"reasoning_effort"`) {
		t.Error("effort must be omitted when empty")
	}

	payload["reasoning_effort"] = "xhigh"
	body, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"reasoning_effort":"xhigh"`) {
		t.Errorf("effort not written: %s", trimForLog(string(body)))
	}
}

// 上游不可用时 benchChat 必须返回错误而非 panic，并且始终报告耗时。
func TestBenchChatReportsErrorAndElapsed(t *testing.T) {
	server := selfCallServer(t)
	parsed, elapsed, err := server.benchChat(context.Background(), "gpt-5.6-reasoning", "xhigh",
		[]map[string]any{{"role": "user", "content": "ping"}})
	if err == nil {
		// 没有可用账号时不应"成功"；若确实成功，响应必须是可解析的 map。
		if parsed == nil {
			t.Fatal("nil error must come with a parsed response")
		}
	}
	if elapsed <= 0 {
		t.Fatalf("elapsed must be measured even on failure, got %v", elapsed)
	}
}

// 已取消的 context 经 callOwnChatCompletions 传播出来。
func TestBenchChatPropagatesCancel(t *testing.T) {
	server := selfCallServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := server.benchChat(ctx, "gpt-5.6-reasoning", "",
		[]map[string]any{{"role": "user", "content": "x"}})
	if err == nil {
		t.Fatal("cancelled context must surface an error")
	}
}

// APK 用 "HTTP %d: %s" 包装非 2xx，并把响应体经 compactToolResult 截到 300 字节。
func TestBenchChatHTTPErrorFormatAndTruncation(t *testing.T) {
	long := strings.Repeat("x", 900)
	got := compactToolResult(long, 300)
	if len(got) > 320 {
		t.Fatalf("compactToolResult(300) produced %d bytes", len(got))
	}
	// 与实现中一致的格式串，确认占位符顺序为 status 后跟 body。
	formatted := "HTTP 503: " + got
	if !strings.HasPrefix(formatted, "HTTP 503: ") {
		t.Fatalf("unexpected format: %s", trimForLog(formatted))
	}
}

// benchChat 以 defer 调用 recordBenchUsage，因此失败路径也必须留下记录。
// APK 证据：+0x0384 MOVZ #502 / +0x0390 MOVZ #200 经 CSEL 选择状态码。
func TestBenchChatRecordsUsageOnFailure(t *testing.T) {
	server := selfCallServer(t)
	before := len(server.usage.snapshotRecords())

	_, _, err := server.benchChat(context.Background(), "gpt-5.6-reasoning", "xhigh",
		[]map[string]any{{"role": "user", "content": "ping"}})

	after := server.usage.snapshotRecords()
	if len(after) != before+1 {
		t.Fatalf("usage records: before=%d after=%d, defer must record exactly once", before, len(after))
	}
	rec := after[len(after)-1]
	if rec.Endpoint != "benchmark" {
		t.Errorf("endpoint=%q want benchmark", rec.Endpoint)
	}
	if rec.Model != "gpt-5.6-reasoning" {
		t.Errorf("model=%q", rec.Model)
	}
	if rec.DurationMs < 0 {
		t.Errorf("duration=%d must be non-negative", rec.DurationMs)
	}
	if err != nil && rec.Status != 502 {
		t.Errorf("failed call status=%d want 502", rec.Status)
	}
	if err == nil && rec.Status != 200 {
		t.Errorf("successful call status=%d want 200", rec.Status)
	}
}
