package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"m365-copilot2api/internal/auth"
)

// 会话列表把本地 session 历史合并进结果（messageCount 等字段）。
// 详情页相关断言已移除：handleM365ConversationDetail 与
// web/conversation.html 在 APK 中均不存在（rodata 无对应字面量、
// APK rootPage 只判 / 与 /login、conversations.go 行段亦无容身空隙）。
func TestConversationListUsesCompleteLocalHistory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("M365_SESSION_CACHE", filepath.Join(dir, "sessions.json"))
	t.Setenv("M365_CONVERSATION_CACHE", filepath.Join(dir, "conversations.json"))
	store, err := auth.OpenStore(filepath.Join(dir, "accounts.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{tokens: store, sessionResolver: openSessionResolver()}

	oldCloudClient := m365CloudClient
	m365CloudClient = nil
	defer func() { m365CloudClient = oldCloudClient }()

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(sessionHeaderName, "session-detail")
	body := &oaiReq{Messages: []oaiMsg{
		{Role: "user", Content: "show the complete answer"},
		{Role: "assistant", Content: "complete body", ReasoningContent: "complete reasoning"},
	}}
	s.sessionResolver.Bind("", "conversation-detail", "account-a", body, "", req)

	listRecorder := httptest.NewRecorder()
	s.handleM365Conversations(listRecorder, httptest.NewRequest(http.MethodGet, "/api/m365/conversations", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	var list struct {
		Count int              `json:"count"`
		Data  []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || list.Data[0]["messageCount"] != float64(2) {
		t.Fatalf("list response=%s", listRecorder.Body.String())
	}

}

func TestConversationTimestampPrefersUpdateTime(t *testing.T) {
	created := time.Now().Add(-time.Hour).UnixMilli()
	updated := time.Now().UnixMilli()
	if got := conversationTimestamp(map[string]any{"createTimeUtc": created, "updateTimeUtc": updated}); got != updated {
		t.Fatalf("timestamp=%d want %d", got, updated)
	}
}
