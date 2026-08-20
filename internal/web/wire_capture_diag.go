package web

import (
	"encoding/json"
	"net/http"

	"m365-copilot2api/internal/chathub"
)

// handleWireFrames returns only the already-sanitized diagnostic records.
func (s *Server) handleWireFrames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	// 原版附带脱敏说明，且 frames 始终是数组。
	frames := chathub.WireFrames()
	if frames == nil {
		frames = []chathub.WireFrame{}
	}
	jsonOut(w, map[string]any{
		"enabled":  chathub.WireCaptureEnabled(),
		"identity": chathub.ActiveIdentitySummary(),
		"frames":   frames,
		"note":     "access token redacted; prompt text not captured",
	})
}

// handleWireFramesToggle updates only in-process capture state. Persisted
// settings are changed through /api/admin/settings.
func (s *Server) handleWireFramesToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "bad json")
		return
	}
	chathub.EnableWireCapture(body.Enabled)
	jsonOut(w, map[string]any{"ok": true, "enabled": chathub.WireCaptureEnabled()})
}
