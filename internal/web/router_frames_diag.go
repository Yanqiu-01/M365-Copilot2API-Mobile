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

// routerFrameKeep is recovered from the APK: M365_CAPTURE_ROUTER_FRAMES_KEEP
// accepts 1..20 and otherwise defaults to three request groups.
func routerFrameKeep() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("M365_CAPTURE_ROUTER_FRAMES_KEEP"))); err == nil && value >= 1 && value <= 20 {
		return value
	}
	return 3
}

type routerFrame struct {
	Stage       string `json:"stage"`
	Prompt      string `json:"prompt,omitempty"`
	Response    string `json:"response,omitempty"`
	Error       string `json:"error,omitempty"`
	PromptLen   int    `json:"promptLen"`
	ResponseLen int    `json:"responseLen"`
}

type routerFrameGroup struct {
	RequestID string        `json:"requestId"`
	At        time.Time     `json:"at"`
	Frames    []routerFrame `json:"frames"`
}

var routerFrames struct {
	sync.Mutex
	groups []routerFrameGroup
}

// recordRouterFrames records a bounded, sanitized snapshot of a router turn.
// APK evidence places the call in (*Server).openaiChat after tool-router work.
func recordRouterFrames(requestID, stage, prompt, response string, routeErr error) {
	if !currentSettings().CaptureRouterFrames {
		return
	}
	frame := routerFrame{
		Stage:       strings.TrimSpace(stage),
		Prompt:      truncateRouterFrame(prompt),
		Response:    truncateRouterFrame(response),
		PromptLen:   len(prompt),
		ResponseLen: len(response),
	}
	if routeErr != nil {
		frame.Error = truncateRouterFrame(routeErr.Error())
	}

	routerFrames.Lock()
	defer routerFrames.Unlock()
	if requestID == "" {
		requestID = "unknown"
	}
	for i := range routerFrames.groups {
		if routerFrames.groups[i].RequestID == requestID {
			routerFrames.groups[i].Frames = append(routerFrames.groups[i].Frames, frame)
			return
		}
	}
	routerFrames.groups = append(routerFrames.groups, routerFrameGroup{
		RequestID: requestID,
		At:        time.Now().UTC(),
		Frames:    []routerFrame{frame},
	})
	keep := routerFrameKeep()
	if len(routerFrames.groups) > keep {
		routerFrames.groups = append([]routerFrameGroup(nil), routerFrames.groups[len(routerFrames.groups)-keep:]...)
	}
}

func routerFrameSnapshot() []routerFrameGroup {
	routerFrames.Lock()
	defer routerFrames.Unlock()
	out := make([]routerFrameGroup, len(routerFrames.groups))
	for i, group := range routerFrames.groups {
		out[i] = routerFrameGroup{RequestID: group.RequestID, At: group.At, Frames: append([]routerFrame(nil), group.Frames...)}
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
			// Do not repeatedly match the same marker in the replacement string.
			break
		}
	}
	const maxRouterFrameBytes = 8000
	if len(value) > maxRouterFrameBytes {
		return value[:maxRouterFrameBytes] + "…[truncated]"
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
