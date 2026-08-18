package web

import "testing"

func TestRequestSizeLimitsAPKEnvironmentParsing(t *testing.T) {
	tests := []struct {
		name         string
		messagesEnv  string
		bytesEnv     string
		wantMessages int
		wantBytes    int
	}{
		{name: "unset", wantMessages: 0, wantBytes: 0},
		{name: "positive", messagesEnv: "12", bytesEnv: "4096", wantMessages: 12, wantBytes: 4096},
		{name: "invalid", messagesEnv: "12x", bytesEnv: "bad", wantMessages: 0, wantBytes: 0},
		{name: "nonpositive", messagesEnv: "0", bytesEnv: "-1", wantMessages: 0, wantBytes: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("M365_MAX_MESSAGES", test.messagesEnv)
			t.Setenv("M365_MAX_REQUEST_BYTES", test.bytesEnv)
			messages, bytes := requestSizeLimits()
			if messages != test.wantMessages || bytes != test.wantBytes {
				t.Fatalf("requestSizeLimits()=(%d,%d), want (%d,%d)", messages, bytes, test.wantMessages, test.wantBytes)
			}
		})
	}
}

func TestOversizeReasonAPKPriorityAndText(t *testing.T) {
	t.Setenv("M365_MAX_MESSAGES", "2")
	t.Setenv("M365_MAX_REQUEST_BYTES", "10")
	if got, want := oversizeReason(3, 100), "request carries 3 messages, limit is 2 (set M365_MAX_MESSAGES to change)"; got != want {
		t.Fatalf("message reason=%q want %q", got, want)
	}
	if got, want := oversizeReason(2, 11), "request body is 11 bytes, limit is 10 (set M365_MAX_REQUEST_BYTES to change)"; got != want {
		t.Fatalf("bytes reason=%q want %q", got, want)
	}
	if got := oversizeReason(2, 10); got != "" {
		t.Fatalf("within limit reason=%q", got)
	}
}
