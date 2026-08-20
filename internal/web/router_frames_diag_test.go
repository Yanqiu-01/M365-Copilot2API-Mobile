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

// 保留组数：默认 50、上限 200。
//
// 默认 3 组时，一轮能力评测（8 任务 × 最多 14 步，每次调用一个 requestID）
// 会把最早的帧全部挤掉，只剩最后 3 组 —— 恰好是已通过的任务，想看的失败帧
// 早已丢失。
func TestRouterFrameKeepBounds(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 50}, {"0", 50}, {"201", 50}, {"bad", 50}, {"1", 1}, {"200", 200},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("M365_CAPTURE_ROUTER_FRAMES_KEEP", test.value)
			if got := routerFrameKeep(); got != test.want {
				t.Fatalf("routerFrameKeep()=%d want %d", got, test.want)
			}
		})
	}
}

// 采集结构必须符合 APK 的 DiagActivity 契约：它按
// requestId / promptLen / textLen / reasoningLen / cause / frames 渲染，
// 每帧再读 text / contentOrigin / addToChainOfThought / messageType。
// 恢复版此前返回 prompt / response / responseLen / error，字段名全部对不上，
// 原生诊断页只能渲染空白，表现为「开了捕获也什么都抓不到」。
func TestRouterFramesMatchDiagActivityContract(t *testing.T) {
	cleanup := resetRouterFramesForTest(t)
	defer cleanup()

	events := []json.RawMessage{
		json.RawMessage(`{"type":2,"arguments":[{"messages":[
			{"text":"先读文件","contentOrigin":"ChainOfThoughtSummary","addToChainOfThought":true,"messageType":"Chat"},
			{"text":"CALL_TOOL: read_file({\"path\":\"a.py\"})","messageType":"Chat"}
		]}]}`),
	}
	recordRouterFrames(routerFrameInput{
		RequestID: "req-1",
		Stage:     "router",
		Prompt:    "路由提示",
		Text:      "CALL_TOOL: read_file({\"path\":\"a.py\"})",
		Reasoning: "先读文件",
		Events:    events,
	})

	encoded, err := json.Marshal(routerFrameSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, key := range []string{
		`"requestId"`, `"promptLen"`, `"textLen"`, `"reasoningLen"`,
		`"frames"`, `"text"`, `"contentOrigin"`, `"addToChainOfThought"`, `"messageType"`,
	} {
		if !strings.Contains(payload, key) {
			t.Errorf("payload missing DiagActivity field %s: %s", key, payload)
		}
	}
	// 旧字段名不得再出现，否则原生诊断页会继续读不到。
	for _, gone := range []string{`"response"`, `"responseLen"`, `"error"`} {
		if strings.Contains(payload, gone) {
			t.Errorf("payload still exposes legacy field %s", gone)
		}
	}

	groups := routerFrameSnapshot()
	if len(groups) != 1 {
		t.Fatalf("groups=%d", len(groups))
	}
	g := groups[0]
	if g.RequestID != "req-1" || g.Stage != "router" {
		t.Errorf("group identity wrong: %+v", g)
	}
	if g.PromptLen == 0 || g.TextLen == 0 || g.ReasoningLen == 0 {
		t.Errorf("lengths must be populated: %+v", g)
	}
	if len(g.Frames) != 2 {
		t.Fatalf("frames=%d want 2 (projected from ChatHub messages)", len(g.Frames))
	}
	if !g.Frames[0].AddToChainOfThought || g.Frames[0].ContentOrigin != "ChainOfThoughtSummary" {
		t.Errorf("reasoning frame must keep its markers: %+v", g.Frames[0])
	}
	if g.Frames[1].AddToChainOfThought {
		t.Errorf("plain frame must not be marked as chain-of-thought: %+v", g.Frames[1])
	}
}

// 失败帧必须带 cause，且凭据要脱敏。
func TestRecordRouterFramesKeepsCauseAndRedacts(t *testing.T) {
	cleanup := resetRouterFramesForTest(t)
	defer cleanup()
	t.Setenv("M365_CAPTURE_ROUTER_FRAMES_KEEP", "2")

	recordRouterFrames(routerFrameInput{
		RequestID: "r1",
		Stage:     "router",
		Prompt:    "Bearer secret-token access_token=another",
		Text:      "Authorization: hidden",
		Err:       errNoAccounts,
	})
	groups := routerFrameSnapshot()
	if len(groups) != 1 {
		t.Fatalf("groups=%d", len(groups))
	}
	if groups[0].Cause == "" {
		t.Error("failed router turn must record cause")
	}
	if strings.Contains(groups[0].Prompt, "secret-token") ||
		strings.Contains(groups[0].Prompt, "another") ||
		strings.Contains(groups[0].Text, "hidden") {
		t.Errorf("credentials leaked: %+v", groups[0])
	}

	// 超出 keep 时保留最近的组。
	recordRouterFrames(routerFrameInput{RequestID: "r2", Stage: "router", Prompt: "p2", Text: "t2"})
	recordRouterFrames(routerFrameInput{RequestID: "r3", Stage: "router", Prompt: "p3", Text: "t3"})
	groups = routerFrameSnapshot()
	if len(groups) != 2 || groups[0].RequestID != "r2" || groups[1].RequestID != "r3" {
		t.Fatalf("groups=%+v", groups)
	}
}

func TestRouterFramesHandlers(t *testing.T) {
	cleanup := resetRouterFramesForTest(t)
	defer cleanup()
	recordRouterFrames(routerFrameInput{RequestID: "request", Stage: "router", Prompt: "prompt", Text: "text"})

	settings := &settingsStore{path: filepath.Join(t.TempDir(), "settings.json"), v: defaultRuntimeSettings()}
	settings.v.CaptureRouterFrames = true
	s := &Server{settings: settings}
	get := httptest.NewRecorder()
	s.handleRouterFrames(get, httptest.NewRequest(http.MethodGet, "/api/admin/debug/router-frames", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	// 原 APK 实测键名为 captures（并带固定 note）。
	var payload struct {
		Enabled  bool               `json:"enabled"`
		Note     string             `json:"note"`
		Captures []routerFrameGroup `json:"captures"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Enabled || len(payload.Captures) != 1 || payload.Note == "" {
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
