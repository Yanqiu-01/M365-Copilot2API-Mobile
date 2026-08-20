package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// routerFrameKeep 是保留的请求组数上限。
//
// 默认 3 组对单次对话诊断够用，但一轮能力评测会产生几十到上百次路由调用
// （8 个任务 × 每任务最多 14 步，每次一个新 requestID 即一组），跑完后最早
// 的帧全被挤掉，只剩最后 3 组 —— 往往还是已通过的任务，真正想看的失败帧
// 早已丢失。上限放宽到 200，默认 50，足够覆盖整轮评测。
func routerFrameKeep() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("M365_CAPTURE_ROUTER_FRAMES_KEEP"))); err == nil && value >= 1 && value <= 200 {
		return value
	}
	return 50
}

// routerRawFrame 是单条 ChatHub 原始消息的诊断投影。
//
// 字段名由 APK 的 DiagActivity 决定（smali 常量池按渲染顺序给出
// text / contentOrigin / addToChainOfThought / ChainOfThoughtSummary /
// messageType）。原生诊断页正是用 contentOrigin 与 addToChainOfThought
// 判定「上游有没有标记思考帧」，再比较相邻帧的 text 判定「累积重写式还是
// 增量式」，因此这三个字段必须原样透出。
type routerRawFrame struct {
	Text                string `json:"text"`
	ContentOrigin       string `json:"contentOrigin,omitempty"`
	AddToChainOfThought bool   `json:"addToChainOfThought,omitempty"`
	MessageType         string `json:"messageType,omitempty"`
	TextLen             int    `json:"textLen"`
}

// routerFrameGroup 是一次路由调用的采集结果。
//
// DiagActivity 读取 requestId / promptLen / textLen / reasoningLen / frames，
// 并在失败时读 cause。此前恢复版返回的是 prompt / response / responseLen /
// error，字段名全部对不上，原生诊断页只能渲染出空白 —— 表现为「开了捕获也
// 什么都抓不到」。
type routerFrameGroup struct {
	RequestID    string           `json:"requestId"`
	At           time.Time        `json:"at"`
	Stage        string           `json:"stage"`
	PromptLen    int              `json:"promptLen"`
	TextLen      int              `json:"textLen"`
	ReasoningLen int              `json:"reasoningLen"`
	Cause        string           `json:"cause,omitempty"`
	Prompt       string           `json:"prompt,omitempty"`
	Text         string           `json:"text,omitempty"`
	Frames       []routerRawFrame `json:"frames"`
}

var routerFrames struct {
	sync.Mutex
	groups []routerFrameGroup
}

// routerFrameInput 汇总一次路由调用的可诊断材料。
type routerFrameInput struct {
	RequestID string
	Stage     string
	Prompt    string
	Text      string
	Reasoning string
	Events    []json.RawMessage
	Err       error
}

// projectRawFrames 把 ChatHub 的 SignalR 帧投影成诊断记录。只保留判定思考
// 帧所需的字段，其余原始内容不落盘。
func projectRawFrames(events []json.RawMessage) []routerRawFrame {
	out := make([]routerRawFrame, 0, len(events))
	for _, raw := range events {
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil {
			continue
		}
		args, _ := obj["arguments"].([]any)
		for _, entry := range args {
			arg, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			messages, _ := arg["messages"].([]any)
			for _, m := range messages {
				msg, ok := m.(map[string]any)
				if !ok {
					continue
				}
				text, _ := msg["text"].(string)
				origin, _ := msg["contentOrigin"].(string)
				cot, _ := msg["addToChainOfThought"].(bool)
				messageType, _ := msg["messageType"].(string)
				if text == "" && origin == "" && messageType == "" {
					continue
				}
				out = append(out, routerRawFrame{
					Text:                truncateRouterFrame(text),
					ContentOrigin:       origin,
					AddToChainOfThought: cot,
					MessageType:         messageType,
					TextLen:             len(text),
				})
			}
		}
	}
	return out
}

// recordRouterFrames records a bounded, sanitized snapshot of a router turn.
// APK evidence places the call in (*Server).openaiChat after tool-router work.
func recordRouterFrames(in routerFrameInput) {
	if !currentSettings().CaptureRouterFrames {
		return
	}
	group := routerFrameGroup{
		RequestID:    strings.TrimSpace(in.RequestID),
		At:           time.Now().UTC(),
		Stage:        strings.TrimSpace(in.Stage),
		PromptLen:    len(in.Prompt),
		TextLen:      len(in.Text),
		ReasoningLen: len(in.Reasoning),
		Prompt:       truncateRouterFrame(in.Prompt),
		Text:         truncateRouterFrame(in.Text),
		Frames:       projectRawFrames(in.Events),
	}
	if group.RequestID == "" {
		group.RequestID = "unknown"
	}
	if in.Err != nil {
		group.Cause = truncateRouterFrame(in.Err.Error())
	}
	if group.Frames == nil {
		group.Frames = []routerRawFrame{}
	}

	routerFrames.Lock()
	defer routerFrames.Unlock()
	routerFrames.groups = append(routerFrames.groups, group)
	if keep := routerFrameKeep(); len(routerFrames.groups) > keep {
		routerFrames.groups = append([]routerFrameGroup(nil), routerFrames.groups[len(routerFrames.groups)-keep:]...)
	}
}

func routerFrameSnapshot() []routerFrameGroup {
	routerFrames.Lock()
	defer routerFrames.Unlock()
	out := make([]routerFrameGroup, len(routerFrames.groups))
	for i, group := range routerFrames.groups {
		copied := group
		copied.Frames = append([]routerRawFrame(nil), group.Frames...)
		out[i] = copied
	}
	return out
}

func truncateRouterFrame(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\r", "")
	for _, marker := range []string{"Bearer ", "bearer ", "access_token=", "accessToken=", "refresh_token=", "Authorization:"} {
		for {
			index := strings.Index(value, marker)
			if index < 0 {
				break
			}
			end := index + len(marker)
			// Header-style values may have whitespace after the colon; include it
			// in the redacted range instead of leaking the actual credential.
			for end < len(value) && (value[end] == ' ' || value[end] == '\t') {
				end++
			}
			for end < len(value) && !strings.ContainsRune(" \t\n&\"',;", rune(value[end])) {
				end++
			}
			value = value[:index] + marker + "[redacted]" + value[end:]
			// Skip past the replacement so the loop terminates.
			if next := strings.Index(value[index+len(marker):], marker); next < 0 {
				break
			}
		}
	}
	const limit = 4096
	if len(value) > limit {
		return value[:limit] + "…"
	}
	return value
}

func (s *Server) handleRouterFrames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 原版返回 {captures, enabled, note}，键名与提示文案均固定。
	captures := routerFrameSnapshot()
	if captures == nil {
		captures = []routerFrameGroup{}
	}
	jsonOut(w, map[string]any{
		"enabled":  currentSettings().CaptureRouterFrames,
		"captures": captures,
		"note":     "在「设置」页开启「捕获路由原始帧」后重新发一次请求即可采集",
	})
}

func (s *Server) handleRouterFramesToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "bad json")
		return
	}
	settings := s.settings.get()
	settings.CaptureRouterFrames = input.Enabled
	if err := s.settings.save(settings); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if !input.Enabled {
		routerFrames.Lock()
		routerFrames.groups = nil
		routerFrames.Unlock()
	}
	jsonOut(w, map[string]any{"ok": true, "enabled": input.Enabled})
}
