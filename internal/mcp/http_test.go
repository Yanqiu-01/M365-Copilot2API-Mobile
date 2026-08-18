package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func resetStreamableState(t *testing.T) {
	t.Helper()
	globalStreamSessions.mu.Lock()
	ids := make([]string, 0, len(globalStreamSessions.sessions))
	for id := range globalStreamSessions.sessions {
		ids = append(ids, id)
	}
	globalStreamSessions.sessions = map[string]time.Time{}
	globalStreamSessions.mu.Unlock()
	for _, id := range ids {
		GlobalRegistry.UnregisterSession(id)
	}
	GlobalToolRegistry.ClearTools()
	t.Cleanup(func() {
		globalStreamSessions.mu.Lock()
		ids := make([]string, 0, len(globalStreamSessions.sessions))
		for id := range globalStreamSessions.sessions {
			ids = append(ids, id)
		}
		globalStreamSessions.sessions = map[string]time.Time{}
		globalStreamSessions.mu.Unlock()
		for _, id := range ids {
			GlobalRegistry.UnregisterSession(id)
		}
		GlobalToolRegistry.ClearTools()
	})
}

func streamableRequest(method, body, sessionID string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/v1/mcp", strings.NewReader(body))
	if sessionID != "" {
		r.Header.Set(streamSessionHeader, sessionID)
	}
	w := httptest.NewRecorder()
	HandleStreamable(w, r)
	return w
}

func decodeResponse(t *testing.T, body *bytes.Buffer) jsonRPCResponse {
	t.Helper()
	var response jsonRPCResponse
	if err := json.Unmarshal(body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", body.String(), err)
	}
	return response
}

func TestStreamableInitializeCreatesAndExposesSession(t *testing.T) {
	resetStreamableState(t)
	w := streamableRequest(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin=%q", got)
	}
	sessionID := w.Header().Get(streamSessionHeader)
	if sessionID == "" {
		t.Fatal("initialize response omitted Mcp-Session-Id")
	}
	response := decodeResponse(t, w.Body)
	if response.Error != nil || response.ID == nil || *response.ID != 1 {
		t.Fatalf("response=%#v", response)
	}
}

func TestStreamableToolsListAndBatch(t *testing.T) {
	resetStreamableState(t)
	GlobalToolRegistry.RegisterTools([]Tool{{Name: "echo", Description: "Echo"}})
	init := streamableRequest(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "")
	sessionID := init.Header().Get(streamSessionHeader)
	if sessionID == "" {
		t.Fatal("missing session")
	}

	w := streamableRequest(http.MethodPost, `[{"jsonrpc":"2.0","id":2,"method":"tools/list"},{"jsonrpc":"2.0","id":3,"method":"tools/list"}]`, sessionID)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var responses []jsonRPCResponse
	if err := json.Unmarshal(w.Body.Bytes(), &responses); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 2 || responses[0].Error != nil || responses[1].Error != nil {
		t.Fatalf("responses=%#v", responses)
	}
	for _, response := range responses {
		var result struct {
			Tools []Tool `json:"tools"`
		}
		if err := json.Unmarshal(response.Result, &result); err != nil || len(result.Tools) != 1 || result.Tools[0].Name != "echo" {
			t.Fatalf("result=%s err=%v", response.Result, err)
		}
	}
}

func TestStreamableNotificationDeleteAndOptions(t *testing.T) {
	resetStreamableState(t)
	init := streamableRequest(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, "")
	sessionID := init.Header().Get(streamSessionHeader)

	notification := streamableRequest(http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, sessionID)
	if notification.Code != http.StatusAccepted {
		t.Fatalf("notification status=%d", notification.Code)
	}

	options := streamableRequest(http.MethodOptions, "", "")
	if options.Code != http.StatusNoContent || !strings.Contains(options.Header().Get("Access-Control-Allow-Headers"), streamSessionHeader) {
		t.Fatalf("options status=%d headers=%v", options.Code, options.Header())
	}

	deleted := streamableRequest(http.MethodDelete, "", sessionID)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d", deleted.Code)
	}
	// APK POST always calls streamSessionStore.ensure. A request after DELETE
	// therefore creates a fresh streamable session rather than returning 404.
	recreated := streamableRequest(http.MethodPost, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, sessionID)
	if recreated.Code != http.StatusOK {
		t.Fatalf("after delete status=%d body=%s", recreated.Code, recreated.Body.String())
	}
	if replacement := recreated.Header().Get(streamSessionHeader); replacement == "" || replacement == sessionID {
		t.Fatalf("expected replacement session header after delete, got %q", replacement)
	}
}

func TestStreamableRejectsOversizeBodyAndStreamsGet(t *testing.T) {
	resetStreamableState(t)
	tooLarge := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":"` + strings.Repeat("x", int(maxStreamableBody)) + `"}`
	w := streamableRequest(http.MethodPost, tooLarge, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversize status=%d", w.Code)
	}

	get := streamableRequest(http.MethodGet, "", "")
	if get.Code != http.StatusOK || !strings.Contains(get.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(get.Body.String(), "streamable-mcp") {
		t.Fatalf("get status=%d headers=%v body=%q", get.Code, get.Header(), get.Body.String())
	}
}
