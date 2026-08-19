package web

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 无可用上游时，runBenchTask 必须以 error 收尾而不是 panic 或空转。
func TestRunBenchTaskFailsFastWithoutUpstream(t *testing.T) {
	server := selfCallServer(t)
	server.benchmark = &benchmarkStore{run: benchmarkRun{State: "running"}}

	task := benchTasks()[0]
	result := server.runBenchTask(context.Background(), task, "gpt-5.6-reasoning", "xhigh")

	if result.Status != "error" {
		t.Fatalf("status=%q want error (no upstream configured)", result.Status)
	}
	if result.Error == "" {
		t.Error("error detail must be populated")
	}
	if result.ID != "bugfix" {
		t.Errorf("embedded task lost: id=%q", result.ID)
	}
	// 地板分在首轮 chat 之前就已算出并记录。
	if result.Floor == 0 {
		t.Error("floor score should be computed from the initial workspace")
	}
	logs := strings.Join(server.benchmark.snapshot().Log, "\n")
	if !strings.Contains(logs, "地板分") {
		t.Errorf("floor log missing: %s", logs)
	}
}

// 已取消的 context 必须在首次循环判定时就退出。
func TestRunBenchTaskHonoursCancel(t *testing.T) {
	server := selfCallServer(t)
	server.benchmark = &benchmarkStore{run: benchmarkRun{State: "running"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := server.runBenchTask(ctx, benchTasks()[4], "gpt-5.6-reasoning", "")
	if result.Status != "cancelled" {
		t.Fatalf("status=%q want cancelled", result.Status)
	}
	if result.ElapsedMS < 0 {
		t.Errorf("elapsed=%d", result.ElapsedMS)
	}
}

// 工作区的测试钩子必须接的是该任务自己的 grader：
// 原始产物不应通过，正确答案应通过。
func TestRunBenchTaskWiresGraderIntoRunTests(t *testing.T) {
	task := benchTasks()[1] // debug: stats.py
	workspace := newBenchWorkspace(task)

	out, err := workspace.execute("run_tests", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out["passed"] != false {
		t.Errorf("original stats.py must fail run_tests, got %v", out["passed"])
	}

	fixed := `def summarize(rows):
    if not rows:
        return {"count": 0, "total": 0, "mean": None, "max": None}
    count = len(rows)
    total = sum(rows)
    return {"count": count, "total": total, "mean": total / count, "max": max(rows)}


def format_report(rows):
    stats = summarize(rows)
    return f"count={stats['count']} total={stats['total']} mean={stats['mean']:.2f}"
`
	if _, err := workspace.execute("write_file", map[string]any{"path": "stats.py", "content": fixed}); err != nil {
		t.Fatal(err)
	}
	out, err = workspace.execute("run_tests", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out["passed"] != true {
		t.Errorf("fixed stats.py should pass run_tests, got %v", out["passed"])
	}
	if runs, _ := out["runs"].(int); runs != 2 {
		t.Errorf("runs=%v want 2", out["runs"])
	}
	// APK 用 "TESTS PASSED: %d/%d"（execute +0x0430，19 字节）宣告通过，
	// 正是编程闭环提示要求模型看到的字样。
	if output, _ := out["output"].(string); !strings.HasPrefix(output, "TESTS PASSED: ") {
		t.Errorf("output=%q want TESTS PASSED prefix", output)
	}
}

// 受保护输入被工具改写后，收尾校验必须拦下。
func TestRunBenchTaskDetectsProtectedTampering(t *testing.T) {
	task := benchTasks()[6] // ledger: ledger.txt 受保护
	workspace := newBenchWorkspace(task)
	if _, err := workspace.execute("write_file", map[string]any{"path": "ledger.txt", "content": "DEPOSIT A 999999"}); err != nil {
		t.Fatal(err)
	}
	if got := gradeBenchTask(task.Protected, workspace.snapshot()); got == "" {
		t.Fatal("tampering with a protected artifact must be detected")
	}
}

// 工具应答需经 compactToolResult 截断，且是合法 JSON 载荷。
func TestRunBenchTaskToolReplyIsBounded(t *testing.T) {
	task := benchTasks()[0]
	workspace := newBenchWorkspace(task)
	out, err := workspace.execute("read_file", map[string]any{"path": "inventory.py"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	// inventory.py 有 1724 字节，截断到 400 后必须显著变短。
	trimmed := compactToolResult(string(encoded), 400)
	if len(trimmed) >= len(encoded) {
		t.Errorf("reply not truncated: %d vs %d", len(trimmed), len(encoded))
	}
	if len(trimmed) > 460 {
		t.Errorf("truncated reply still too long: %d", len(trimmed))
	}
}
