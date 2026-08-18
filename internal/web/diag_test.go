package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func resetDiagForTest(t *testing.T) {
	t.Helper()
	diagMu.Lock()
	if diagFile != nil {
		_ = diagFile.Close()
	}
	diagFile = nil
	diagOnce = sync.Once{}
	diagMu.Unlock()
	inflight = sync.Map{}
	chatSlotsMu.Lock()
	chatSlots = nil
	chatSlotN = 0
	chatSlotsMu.Unlock()
	t.Cleanup(func() {
		diagMu.Lock()
		if diagFile != nil {
			_ = diagFile.Close()
		}
		diagFile = nil
		diagOnce = sync.Once{}
		diagMu.Unlock()
		inflight = sync.Map{}
		chatSlotsMu.Lock()
		chatSlots = nil
		chatSlotN = 0
		chatSlotsMu.Unlock()
	})
}

func TestDiagAPKConfiguration(t *testing.T) {
	resetDiagForTest(t)
	t.Setenv("M365_DATA_DIR", "/tmp/m365-data")
	t.Setenv("M365_STAGE_LOG", "")
	if diagEnabled() {
		t.Fatal("unset stage log should be disabled")
	}
	if got, want := diagPath(), filepath.Join("/tmp/m365-data", "server-stages.log"); got != want {
		t.Fatalf("diagPath()=%q want %q", got, want)
	}
	for _, value := range []string{"0", "no", "off", "false"} {
		t.Setenv("M365_STAGE_LOG", value)
		if diagEnabled() {
			t.Fatalf("%q should disable stage logs", value)
		}
	}
	t.Setenv("M365_STAGE_LOG", "yes")
	if !diagEnabled() {
		t.Fatal("yes should enable stage logs")
	}
	t.Setenv("M365_STAGE_LOG_MAX_BYTES", "1234")
	if got := diagMaxBytes(); got != 1234 {
		t.Fatalf("diagMaxBytes=%d", got)
	}
	t.Setenv("M365_STAGE_LOG_MAX_BYTES", "bad")
	if got := diagMaxBytes(); got != defaultDiagMaxBytes {
		t.Fatalf("default diagMaxBytes=%d", got)
	}
}

func TestStageTracksInflightAndWritesBoundedLog(t *testing.T) {
	resetDiagForTest(t)
	dir := t.TempDir()
	t.Setenv("M365_DATA_DIR", dir)
	t.Setenv("M365_STAGE_LOG", "1")
	t.Setenv("M365_STAGE_LOG_MAX_BYTES", "512")

	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	beginRequest("request-1", r)
	stage("request-1", "body_parsed", map[string]any{"raw_bytes": 42})
	items := inflightSnapshot()
	if len(items) != 1 || items[0].ID != "request-1" || items[0].Stage != "body_parsed" {
		t.Fatalf("inflight=%#v", items)
	}
	endRequest("request-1", nil)
	if got := inflightSnapshot(); len(got) != 0 {
		t.Fatalf("inflight after end=%#v", got)
	}
	data, err := os.ReadFile(diagPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"stage":"body_parsed"`) || len(data) > 512 {
		t.Fatalf("stage log=%q (%d bytes)", data, len(data))
	}
}

func TestGlobalChatSlotsAPKLimitAndCancellation(t *testing.T) {
	resetDiagForTest(t)
	t.Setenv("M365_MAX_CONCURRENT_CHATS", "1")
	if got := maxConcurrentChats(); got != 1 {
		t.Fatalf("maxConcurrentChats=%d", got)
	}
	first, err := acquireChatSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := acquireChatSlot(ctx); err == nil {
		t.Fatal("second slot acquisition should wait for release")
	}
	first()
	second, err := acquireChatSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestLivenessAndStageHandlers(t *testing.T) {
	resetDiagForTest(t)
	t.Setenv("M365_STAGE_LOG", "0")
	s := &Server{}
	live := httptest.NewRecorder()
	s.handleLiveness(live, httptest.NewRequest(http.MethodGet, "/api/live", nil))
	if live.Code != http.StatusOK || !strings.Contains(live.Body.String(), `"status":"ok"`) || !strings.Contains(live.Body.String(), `"chatSlots"`) {
		t.Fatalf("liveness=%d %s", live.Code, live.Body.String())
	}
	stages := httptest.NewRecorder()
	s.handleStageLog(stages, httptest.NewRequest(http.MethodGet, "/api/stages", nil))
	if stages.Code != http.StatusOK || !strings.Contains(stages.Body.String(), `"enabled":false`) {
		t.Fatalf("stages=%d %s", stages.Code, stages.Body.String())
	}
}
