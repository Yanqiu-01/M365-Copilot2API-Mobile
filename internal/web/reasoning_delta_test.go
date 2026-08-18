package web

import "testing"

func TestReasoningDeltaAPKSemantics(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     string
	}{
		{name: "empty current", previous: "old", current: "", want: ""},
		{name: "empty previous", previous: "", current: "thought", want: "thought"},
		{name: "identical snapshot", previous: "thought", current: "thought", want: ""},
		{name: "growing snapshot", previous: "think", current: "thinking", want: "ing"},
		{name: "shorter current is previous prefix", previous: "thinking", current: "think", want: ""},
		{name: "same length replacement", previous: "alpha", current: "bravo", want: "bravo"},
		{name: "longer replacement", previous: "alpha", current: "bravo-charlie", want: "bravo-charlie"},
		{name: "utf8 extension", previous: "思", current: "思考", want: "考"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reasoningDelta(tt.previous, tt.current); got != tt.want {
				t.Fatalf("reasoningDelta(%q, %q) = %q; want %q", tt.previous, tt.current, got, tt.want)
			}
		})
	}
}
