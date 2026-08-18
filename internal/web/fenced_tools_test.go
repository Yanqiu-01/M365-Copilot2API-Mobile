package web

import "testing"

func TestFencedWorkspaceShellIsStructuredToolCall(t *testing.T) {
	tools := []map[string]any{{"type": "function", "function": map[string]any{"name": "workspace_shell"}}}
	calls := fencedToolCalls("```workspace_shell\n{\"command\":\"find /workspace -type f -o -type d | sort\"}\n```", tools, "auto")
	if len(calls) != 1 {
		t.Fatalf("expected one structured tool call, got %d", len(calls))
	}
	if calls[0].Name != "workspace_shell" {
		t.Fatalf("unexpected tool name %q", calls[0].Name)
	}
	if string(calls[0].Arguments) != `{"command":"find /workspace -type f -o -type d | sort"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Arguments)
	}
}

func TestFencedBashNotConvertedWhenUndeclared(t *testing.T) {
	// Issue #12: no bash tool declared -> code blocks must stay as text.
	calls := fencedToolCalls("```powershell\nGet-Process\n```", nil, "auto")
	if len(calls) != 0 {
		t.Fatalf("undeclared shell conversion must not happen, got %d calls", len(calls))
	}
}

func TestFencedBashConvertedWhenDeclared(t *testing.T) {
	tools := []map[string]any{{"type": "function", "function": map[string]any{"name": "bash"}}}
	calls := fencedToolCalls("```bash\nls -la\n```", tools, "auto")
	if len(calls) != 1 || calls[0].Name != "bash" {
		t.Fatalf("declared bash should convert, got %+v", calls)
	}
	if string(calls[0].Arguments) != `{"command":"ls -la"}` {
		t.Fatalf("unexpected arguments: %s", calls[0].Arguments)
	}
}
