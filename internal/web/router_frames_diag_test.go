package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func resetRouterFramesForTest(t *testing.T) func() {
	t.Helper()
	settings := openSettingsStore()
	settings.mu.Lock()
	previous := settings.v.CaptureRouterFrames
	settings.v.CaptureRouterFrames = true
	settings.mu.Unlock()

	routerFrames.Lock()
	previousGroups := routerFrames.groups
	routerFrames.groups = nil
	routerFrames.Unlock()

	return func() {
		settings.mu.Lock()
		settings.v.CaptureRouterFrames = previous
		settings.mu.Unlock()
		routerFrames.Lock()
		routerFrames.groups = previousGroups
		routerFrames.Unlock()
	}
}

func TestRouterFrameKeepAPKBounds(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 3}, {"0", 3}, {"21", 3}, {"bad", 3}, {"1", 1}, {"20", 20},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("M365_CAPTURE_ROUTER_FRAMES_KEEP", test.value)
			if got := routerFrameKeep(); got != test.want {
				t.Fatalf("routerFrameKeep()=%d want %d", got, test.want)
			}
		})
	}
}

func TestRecordRouterFramesGroupsBoundsAndRedacts(t *testing.T) {
	cleanup := resetRouterFramesForTest(t)
	defer cleanup()
	t.Setenv("M365_CAPTURE_ROUTER_FRAMES_KEEP", "2")

	recordRouterFrames("r1", "router", "Bearer secret-token access_token=another", "Authorization: hidden", nil)
	recordRouterFrames("r1", "repair", "prompt", "response", nil)
	first := routerFrameSnapshot()
	if len(first) != 1 || len(first[0].Frames) != 2 {
		t.Fatalf("first snapshot=%#v", first)
	}
	if strings.Contains(first[0].Frames[0].Prompt, "secret-token") || strings.Contains(first[0].Frames[0].Prompt, "another") || strings.Contains(first[0].Frames[0].Response, "hidden") {
		t.Fatalf("router frame leaked sensitive content: %#v", first[0].Frames[0])
	}

	recordRouterFrames("r2", "router", "prompt2", "response2", nil)
	recordRouterFrames("r3", "router", "prompt3", "response3", nil)
	groups := routerFrameSnapshot()
	if len(groups) != 2 || groups[0].RequestID != "r2" || groups[1].RequestID != "r3" {
		t.Fatalf("groups=%#v", groups)
	}
	if len(groups[0].Frames) != 1 || groups[0].Frames[0].Stage != "router" {
		t.Fatalf("group=%#v", groups[0])
	}
	for _, group := range groups {
		for _, frame := range group.Frames {
			if frame.PromptLen == 0 || frame.ResponseLen == 0 {
				t.Fatalf("lengths missing: %#v", frame)
			}
		}
	}
}

func TestRouterFramesHandlers(t *testing.T) {
	cleanup := resetRouterFramesForTest(t)
	defer cleanup()
	t.Setenv("M365_CAPTURE_ROUTER_FRAMES_KEEP", "3")
	recordRouterFrames("request", "router", "prompt", "response", nil)

	settings := &settingsStore{path: filepath.Join(t.TempDir(), "settings.json"), v: defaultRuntimeSettings()}
	settings.v.CaptureRouterFrames = true
	s := &Server{settings: settings}
	get := httptest.NewRecorder()
	s.handleRouterFrames(get, httptest.NewRequest(http.MethodGet, "/api/admin/debug/router-frames", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	var payload struct {
		Enabled bool               `json:"enabled"`
		Keep    int                `json:"keep"`
		Frames  []routerFrameGroup `json:"frames"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Enabled || payload.Keep != 3 || len(payload.Frames) != 1 {
		t.Fatalf("payload=%#v", payload)
	}

	post := httptest.NewRecorder()
	s.handleRouterFramesToggle(post, httptest.NewRequest(http.MethodPost, "/api/admin/debug/router-frames/toggle", strings.NewReader(`{"enabled":false}`)))
	if post.Code != http.StatusOK {
		t.Fatalf("toggle status=%d body=%s", post.Code, post.Body.String())
	}
	if got := routerFrameSnapshot(); len(got) != 0 {
		t.Fatalf("frames were not cleared: %#v", got)
	}

	bad := httptest.NewRecorder()
	s.handleRouterFramesToggle(bad, httptest.NewRequest(http.MethodPost, "/api/admin/debug/router-frames/toggle", strings.NewReader(`{`)))
	// The malformed body must be rejected rather than changing state.
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad toggle status=%d", bad.Code)
	}
}
