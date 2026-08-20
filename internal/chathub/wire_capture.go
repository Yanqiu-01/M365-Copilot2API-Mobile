package chathub

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// WireFrame is the sanitized diagnostic record exposed by /api/admin/debug/wire.
// Its field layout and the four-entry retention window are recovered from the
// APK's wire_capture.go pclntab/ARM64 implementation.
type WireFrame struct {
	At           time.Time `json:"at"`
	Kind         string    `json:"kind"`
	Sent         string    `json:"sent,omitempty"`
	HandshakeURL string    `json:"handshakeUrl,omitempty"`
	PayloadBytes int       `json:"payloadBytes"`
}

const maxWireFrames = 4

var wireCapture struct {
	sync.Mutex
	enabled bool
	frames  []WireFrame
}

func wireCaptureEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("M365_CAPTURE_WIRE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// EnableWireCapture changes runtime capture state. Disabling capture also
// clears previously captured request data, as the APK does.
func EnableWireCapture(enabled bool) {
	wireCapture.Lock()
	wireCapture.enabled = enabled
	if !enabled {
		wireCapture.frames = nil
	}
	wireCapture.Unlock()
}

func WireCaptureEnabled() bool {
	wireCapture.Lock()
	enabled := wireCapture.enabled
	wireCapture.Unlock()
	return enabled || wireCaptureEnvEnabled()
}

func WireFrames() []WireFrame {
	wireCapture.Lock()
	frames := append([]WireFrame(nil), wireCapture.frames...)
	wireCapture.Unlock()
	return frames
}

// recordWire captures a sent SignalR payload only after sanitizing it. The APK
// calls this immediately before the main chat payload is written.
func recordWire(kind, handshakeURL, payload string) {
	sent := sanitizeWirePayload(payload)
	if !WireCaptureEnabled() {
		return
	}

	frame := WireFrame{
		At:           time.Now(),
		Kind:         kind,
		Sent:         sent,
		HandshakeURL: redactHandshake(handshakeURL),
		PayloadBytes: len(payload),
	}
	wireCapture.Lock()
	if len(wireCapture.frames) >= maxWireFrames {
		copy(wireCapture.frames, wireCapture.frames[len(wireCapture.frames)-maxWireFrames+1:])
		wireCapture.frames = wireCapture.frames[:maxWireFrames-1]
	}
	wireCapture.frames = append(wireCapture.frames, frame)
	wireCapture.Unlock()
}

// wireFrameMaxBytes 是单帧保留上限。超限时截断而非丢弃 —— 评测与任何带
// 工具 schema 的请求 payload 都远超此值（实测 12856 字节），此前「超限即
// 返回空串」使捕获在最需要它的场景下永远为空，只能看到 payloadBytes 而
// 看不到任何内容。
const wireFrameMaxBytes = 4096

// sanitizeWirePayload retains the first SignalR record for diagnostics while
// recursively redacting credential fields. Invalid JSON is intentionally not
// retained: the APK returned an empty sanitized payload on parse failure.
func sanitizeWirePayload(payload string) string {
	first, _, _ := strings.Cut(payload, rs)
	if first == "" {
		return ""
	}
	var value any
	if json.Unmarshal([]byte(first), &value) != nil {
		return ""
	}
	redactWireValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	if len(encoded) > wireFrameMaxBytes {
		// 在 UTF-8 边界上截断，附标记说明被截断及原始长度。
		cut := wireFrameMaxBytes
		for cut > 0 && !utf8.RuneStart(encoded[cut]) {
			cut--
		}
		return string(encoded[:cut]) + fmt.Sprintf("…[truncated, %d bytes total]", len(encoded))
	}
	return string(encoded)
}

func redactWireValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch strings.ToLower(key) {
			case "access_token", "accesstoken", "refresh_token", "refreshtoken", "authorization", "token", "client_secret", "clientsecret", "password":
				delete(v, key)
			default:
				redactWireValue(child)
			}
		}
	case []any:
		for _, child := range v {
			redactWireValue(child)
		}
	}
}

// redactHandshake removes both token spellings explicitly handled by the APK.
func redactHandshake(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Del("access_token")
	query.Del("accessToken")
	u.RawQuery = query.Encode()
	return u.String()
}
