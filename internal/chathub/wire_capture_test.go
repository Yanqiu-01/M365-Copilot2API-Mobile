package chathub

import (
	"strings"
	"testing"
)

func resetWireCaptureForTest() {
	wireCapture.Lock()
	wireCapture.enabled = false
	wireCapture.frames = nil
	wireCapture.Unlock()
}

func TestWireCaptureSanitizesPayloadAndHandshake(t *testing.T) {
	resetWireCaptureForTest()
	t.Cleanup(resetWireCaptureForTest)
	t.Setenv("M365_CAPTURE_WIRE", "")
	EnableWireCapture(true)

	payload := `{"access_token":"secret","arguments":[{"accessToken":"also-secret","message":{"text":"hello","token":"nested-secret"}}]}` + rs + `{"type":1}`
	recordWire("chat_send", "wss://example.invalid/chat?access_token=secret&accessToken=also-secret&safe=ok", payload)

	frames := WireFrames()
	if len(frames) != 1 {
		t.Fatalf("frame count = %d", len(frames))
	}
	frame := frames[0]
	if frame.Kind != "chat_send" || frame.PayloadBytes != len(payload) {
		t.Fatalf("frame = %#v", frame)
	}
	for _, secret := range []string{"secret", "also-secret", "nested-secret", "access_token", "accessToken"} {
		if strings.Contains(frame.Sent, secret) || strings.Contains(frame.HandshakeURL, secret) {
			t.Fatalf("wire capture leaked %q: %#v", secret, frame)
		}
	}
	if !strings.Contains(frame.Sent, `"text":"hello"`) || !strings.Contains(frame.HandshakeURL, "safe=ok") {
		t.Fatalf("sanitized frame lost non-sensitive values: %#v", frame)
	}
}

func TestWireCaptureRetainsLastFourAndClearsOnDisable(t *testing.T) {
	resetWireCaptureForTest()
	t.Cleanup(resetWireCaptureForTest)
	t.Setenv("M365_CAPTURE_WIRE", "")
	EnableWireCapture(true)
	for i := 0; i < 6; i++ {
		recordWire("chat_send", "", `{"type":4}`)
	}
	frames := WireFrames()
	if len(frames) != maxWireFrames {
		t.Fatalf("frame count = %d, want %d", len(frames), maxWireFrames)
	}
	EnableWireCapture(false)
	if WireCaptureEnabled() {
		t.Fatal("capture should be disabled")
	}
	if got := WireFrames(); len(got) != 0 {
		t.Fatalf("frames after disable = %#v", got)
	}
}

func TestWireCaptureEnvironmentSwitch(t *testing.T) {
	resetWireCaptureForTest()
	t.Cleanup(resetWireCaptureForTest)
	for _, value := range []string{"1", "true", "yes", "on"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("M365_CAPTURE_WIRE", value)
			if !WireCaptureEnabled() {
				t.Fatalf("%q did not enable capture", value)
			}
		})
	}
}
