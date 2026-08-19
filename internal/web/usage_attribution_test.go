package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 评测经 callOwnChatCompletions 走 openaiChat，而 openaiChat 末尾的
// bindConversation 本身也会记账。若不加区分，同一次评测调用会被计入
// 两条用量记录（/v1/chat/completions 与 internal-benchmark 各一条），
// 导致「用量」页把评测消耗重复统计。
//
// 修法是给内部自调用打 X-M365-Internal-Call 头，bindConversation 据此跳过。
func TestInternalCallSkipsUsageAccounting(t *testing.T) {
	plain := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	if isInternalCall(plain) {
		t.Fatal("request without the header must not be treated as internal")
	}

	marked := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	marked.Header.Set(internalCallHeader, "benchmark")
	if !isInternalCall(marked) {
		t.Fatal("request carrying the internal header must be recognised")
	}

	if isInternalCall(nil) {
		t.Error("nil request must not panic or be treated as internal")
	}

	// 空白值不算内部调用，避免误设空头导致统计静默丢失。
	blank := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	blank.Header.Set(internalCallHeader, "   ")
	if isInternalCall(blank) {
		t.Error("blank header value must not suppress accounting")
	}
}

// callOwnChatCompletions 必须给内部请求打上标记，否则评测会双记账。
func TestBenchmarkSelfCallIsMarkedInternal(t *testing.T) {
	server := selfCallServer(t)
	payload := []byte(`{"model":"gpt-5.6-reasoning","messages":[{"role":"user","content":"ping"}]}`)

	before := len(server.usage.snapshotRecords())
	if _, _, err := server.callOwnChatCompletions(context.Background(), payload, 5*time.Second); err != nil {
		t.Fatalf("self-call error: %v", err)
	}
	after := server.usage.snapshotRecords()

	// callOwnChatCompletions 自身不记账；bindConversation 因内部标记跳过。
	// 因此这一层不应新增任何 /v1/chat/completions 记录。
	for _, rec := range after[before:] {
		if rec.Endpoint == "/v1/chat/completions" {
			t.Errorf("internal self-call must not record a /v1/chat/completions entry: %+v", rec)
		}
	}
}

// 评测一次调用只应留下一条 internal-benchmark 记录。
func TestBenchChatRecordsExactlyOneEntry(t *testing.T) {
	server := selfCallServer(t)
	before := len(server.usage.snapshotRecords())

	if _, _, err := server.benchChat(context.Background(), "gpt-5.6-reasoning", "xhigh",
		[]map[string]any{{"role": "user", "content": "ping"}}); err == nil {
		t.Log("upstream unexpectedly reachable; accounting assertions still apply")
	}

	added := server.usage.snapshotRecords()[before:]
	if len(added) != 1 {
		t.Fatalf("one bench call produced %d usage records, want exactly 1: %+v", len(added), added)
	}
	if added[0].Endpoint != "internal-benchmark" {
		t.Errorf("endpoint=%q want internal-benchmark", added[0].Endpoint)
	}
	// 评测消耗不应挂在任何调用方 API Key 上。
	if added[0].APIKeyPrefix != "" {
		t.Errorf("benchmark usage must not be attributed to an API key, got %q", added[0].APIKeyPrefix)
	}
}

// 模型测试（管理页的连通性探测）不计入用量统计 —— 它走 chatWithAccount，
// 不经任何记账路径。这里固化该行为，防止日后误加记账把探测算进账单。
func TestAdminModelTestDoesNotRecordUsage(t *testing.T) {
	server := selfCallServer(t)
	before := len(server.usage.snapshotRecords())

	recorder := httptest.NewRecorder()
	server.adminModelTest(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/models/test",
		strings.NewReader(`{"model":"gpt-5.6-reasoning"}`)))

	if added := server.usage.snapshotRecords()[before:]; len(added) != 0 {
		t.Errorf("model connectivity probe must not be billed, got %d records: %+v", len(added), added)
	}
}
