package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m365-copilot2api/internal/auth"
)

// selfCallServer 构造一个足以让 openaiChat 完整执行的最小 Server。
// 会话缓存重定向到临时目录，避免测试间互相污染。
func selfCallServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("M365_SESSION_CACHE", filepath.Join(t.TempDir(), "sessions.json"))
	dir := t.TempDir()
	store, err := auth.OpenStore(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("M365_USAGE_LOG", filepath.Join(dir, "usage.json"))
	s := benchmarkHTTPServer()
	s.tokens = store
	s.sessionResolver = openSessionResolver()
	s.usage = openUsageLog()
	return s
}

// callOwnChatCompletions 的形状由 APK 机器码约束：
// POST /v1/chat/completions、Content-Type: application/json、
// goroutine 内调用 openaiChat、selectgo 两路（完成 / ctx.Done()），
// 返回 (body, status, error)。
func TestCallOwnChatCompletionsRoutesThroughOpenAIHandler(t *testing.T) {
	server := selfCallServer(t)

	payload, err := json.Marshal(map[string]any{
		"model":    "gpt-5.6-reasoning",
		"messages": []map[string]string{{"role": "user", "content": "ping"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	body, status, err := server.callOwnChatCompletions(context.Background(), payload, 5*time.Second)
	if err != nil {
		t.Fatalf("self-call returned error: %v", err)
	}
	// 没有配置账号时处理器应给出 4xx/5xx，但必须是真实走过 handler 的响应，
	// 而不是零值：status 必须被 recorder 填充。
	if status == 0 {
		t.Fatal("status must be populated by the recorder")
	}
	if len(body) == 0 {
		t.Fatal("body must be captured from the recorder")
	}
	// 原 APK 在 /v1/chat/completions 上把账号解析失败作为纯文本返回
	// （实测 400 "no accounts; login first"），因此这里只能断言响应体
	// 被真实捕获，不能要求它是 JSON。
	if !strings.Contains(string(body), "no accounts; login first") {
		t.Fatalf("unexpected self-call body=%q", trimForLog(string(body)))
	}
	if status != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", status)
	}
}

// 超时分支：selectgo 的第二个 case 返回 ctx.Err()，body 为 nil、status 为 0。
func TestCallOwnChatCompletionsHonoursTimeout(t *testing.T) {
	server := selfCallServer(t)
	payload := []byte(`{"model":"gpt-5.6-reasoning","messages":[{"role":"user","content":"x"}]}`)

	// 传入已过期的超时，迫使 ctx.Done() 先就绪。
	body, status, err := server.callOwnChatCompletions(context.Background(), payload, time.Nanosecond)
	if err == nil {
		// 处理器极快返回时也可能先命中完成分支，此时不应报错。
		if status == 0 {
			t.Fatal("completed branch must carry a status")
		}
		return
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error=%v want DeadlineExceeded", err)
	}
	if body != nil || status != 0 {
		t.Fatalf("timeout branch must return nil body and zero status, got body=%d status=%d", len(body), status)
	}
}

// 父 context 取消同样经由 ctx.Done() 分支传播。
func TestCallOwnChatCompletionsPropagatesParentCancel(t *testing.T) {
	server := selfCallServer(t)
	parent, cancel := context.WithCancel(context.Background())
	cancel()

	payload := []byte(`{"model":"gpt-5.6-reasoning","messages":[{"role":"user","content":"x"}]}`)
	_, _, err := server.callOwnChatCompletions(parent, payload, 5*time.Second)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v want Canceled", err)
	}
}

// 非法 JSON 应由 openaiChat 自身拒绝，自调用链路仍须正常返回状态码，
// 证明请求确实穿过了 handler 而非在构造阶段短路。
func TestCallOwnChatCompletionsSurfacesHandlerRejection(t *testing.T) {
	server := selfCallServer(t)
	body, status, err := server.callOwnChatCompletions(context.Background(), []byte("not json"), 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if status < 400 {
		t.Fatalf("malformed payload should be rejected, got status=%d body=%q", status, trimForLog(string(body)))
	}
	if !strings.Contains(strings.ToLower(string(body)), "json") {
		t.Logf("body=%q", trimForLog(string(body)))
	}
}
