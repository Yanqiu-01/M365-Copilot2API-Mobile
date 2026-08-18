package web

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCleanBenchPathRejectsEscapeAndNormalizes(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
		bad   bool
	}{
		{"inventory.py", "inventory.py", false},
		{"nested\\report.json", "nested/report.json", false},
		{"./stats.py", "stats.py", false},
		{"", "", true},
		{"/etc/passwd", "", true},
		{"../secret", "", true},
		{"nested/../../secret", "", true},
	} {
		got, err := cleanBenchPath(test.input)
		if test.bad {
			if err == nil {
				t.Fatalf("cleanBenchPath(%q)=%q, expected error", test.input, got)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Fatalf("cleanBenchPath(%q)=(%q,%v), want %q", test.input, got, err, test.want)
		}
	}
}

func TestBenchWorkspaceSnapshotAndConstrainedExecution(t *testing.T) {
	workspace := newBenchWorkspace(map[string]string{"a.txt": "one", "nested/b.txt": "two"})
	workspace.setTest(func(files map[string]string) bool { return files["a.txt"] == "updated" })

	listed, err := workspace.execute("list_files", map[string]any{})
	if err != nil || !reflect.DeepEqual(listed["files"], []string{"a.txt", "nested/b.txt"}) {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	read, err := workspace.execute("read_file", map[string]any{"path": "a.txt"})
	if err != nil || read["content"] != "one" {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	if _, err := workspace.execute("write_file", map[string]any{"path": "../escape", "content": "bad"}); err == nil {
		t.Fatal("workspace allowed escaping write")
	}
	if _, err := workspace.execute("write_file", map[string]any{"path": "a.txt", "content": "updated"}); err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.execute("write_file", map[string]any{"path": "a.txt", "content": "updated"}); err != nil {
		t.Fatal(err)
	}
	status, err := workspace.execute("run_tests", map[string]any{})
	if err != nil || status["passed"] != true || status["runs"] != 1 {
		t.Fatalf("test status=%#v err=%v", status, err)
	}
	passed, runs := workspace.testStatus()
	if !passed || runs != 1 || !workspace.wrote || workspace.redundant != 1 {
		t.Fatalf("workspace state: passed=%t runs=%d wrote=%t redundant=%d", passed, runs, workspace.wrote, workspace.redundant)
	}

	snapshot := workspace.snapshot()
	snapshot["a.txt"] = "mutated"
	if after := workspace.snapshot()["a.txt"]; after != "updated" {
		t.Fatalf("snapshot mutated workspace: %q", after)
	}
}

func TestBenchToolSchemaAPKNames(t *testing.T) {
	tools := benchToolSchema()
	if len(tools) != 4 {
		t.Fatalf("tool count=%d", len(tools))
	}
	want := []string{"list_files", "read_file", "write_file", "run_tests"}
	for i, tool := range tools {
		var function struct {
			Name       string         `json:"name"`
			Parameters map[string]any `json:"parameters"`
		}
		if err := json.Unmarshal(tool.Function, &function); err != nil {
			t.Fatal(err)
		}
		if tool.Type != "function" || function.Name != want[i] || function.Parameters["type"] != "object" {
			t.Fatalf("tool %d = %#v / %#v", i, tool, function)
		}
	}
}
