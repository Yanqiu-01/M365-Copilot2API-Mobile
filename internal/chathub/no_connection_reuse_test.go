package chathub

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 原 APK 的 pclntab 文件清单里没有 preheater.go / connpool.go，
// chathub 包内也不存在 Preheater、ConnPool、Take、Warm 等符号。
//
// 这条约束不是风格问题。buildWSURL 把 chatsessionid、clientrequestid、
// ConversationId 写进握手 URL，连接因此与具体会话绑定；跨请求复用会让上游把
// 新 payload 归到旧连接的会话上，表现为随机的拒绝、空补全或上下文丢失。
// 恢复过程中曾引入一个按 oid|tid 取连接、用随机 UUID 预热的 Preheater，
// 正是随机响应失败的根因。此测试防止它以任何形式回来。
func TestNoConnectionReuseLayer(t *testing.T) {
	forbidden := []string{"Preheater", "preheatedConn", "ConnPool", "NewPreheater", "NewConnPool"}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, sym := range forbidden {
			if strings.Contains(src, sym) {
				t.Errorf("%s references %q: the original APK dials a fresh WebSocket per request and has no connection-reuse layer", name, sym)
			}
		}
	}

	for _, f := range []string{"preheater.go", "connpool.go"} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s must not exist: absent from the original APK pclntab file list", f)
		}
	}
}

// 会话标识必须留在握手 URL 里；否则连接无法与会话对应，
// 也就无法证明"每请求新建连接"是必要的。
func TestWSURLCarriesPerRequestSessionIdentity(t *testing.T) {
	acc := Account{AccessToken: "token", OID: "oid-1", TID: "tid-1"}
	raw, err := buildWSURL(acc, "session-abc", "conversation-xyz", "request-123")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"chatsessionid":   "request-123",
		"clientrequestid": "request-123",
		"ConversationId":  "conversation-xyz",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query %s=%q want %q", key, got, want)
		}
	}
}
