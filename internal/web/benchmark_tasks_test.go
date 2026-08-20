package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// benchTask 的后三个字段是执行期数据，必须不出现在 API 响应里。
// 依据：APK assets/web/index.html 只引用 id/title/detail/category。
func TestBenchTaskCatalogJSONHidesExecutionFields(t *testing.T) {
	encoded, err := json.Marshal(benchTaskCatalog())
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	// 只断言结构体的执行期字段与产物内容不外泄。文件名本身会出现在
	// detail 里（原 APK 的提示词就写明「工作区有 inventory.py」、
	// 「工作区里有 sales.csv」），因此不能把文件名当作泄露标志。
	for _, leaked := range []string{`"Files"`, `"Protected"`, `"Grader"`, "import json", "def summarize", "def load_users", "DEPOSIT A "} {
		if strings.Contains(payload, leaked) {
			t.Errorf("JSON must not expose %q", leaked)
		}
	}
	// detail 必须完整下发：推理任务需要产物名与格式才能作答。
	for _, want := range []string{"schedule.json", "route.json", "state.json", "report.json"} {
		if !strings.Contains(payload, want) {
			t.Errorf("catalog detail must keep the artifact name %q", want)
		}
	}
	for _, want := range []string{`"id":"bugfix"`, `"category":"coding"`, `"id":"route"`, `"category":"reasoning"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("JSON missing %s", want)
		}
	}
}

// 八条任务的产物与评分器挂载，顺序由 APK 的 ADRP 0x136c000 引用点确定。
func TestBenchTasksWireArtifactsAndGraders(t *testing.T) {
	tasks := benchTasks()
	if len(tasks) != 8 {
		t.Fatalf("task count=%d want 8", len(tasks))
	}

	wantFiles := map[string][]string{
		"bugfix":    {"inventory.py"},
		"debug":     {"stats.py", "run_report.txt"},
		"refactor":  {"users.py", "staff.py", "people.json"},
		"algorithm": {},
		"shift":     {},
		"sales":     {"sales.csv"},
		"ledger":    {"ledger.txt"},
		"route":     {},
	}
	wantProtected := map[string][]string{
		"bugfix":    {},
		"debug":     {"run_report.txt"},
		"refactor":  {"people.json"},
		"algorithm": {},
		"shift":     {},
		"sales":     {"sales.csv"},
		"ledger":    {"ledger.txt"},
		"route":     {},
	}

	for _, task := range tasks {
		if task.Grader == nil {
			t.Errorf("%s: grader must be wired", task.ID)
			continue
		}
		if task.Files == nil || task.Protected == nil {
			t.Errorf("%s: Files/Protected must be non-nil", task.ID)
			continue
		}
		files, ok := wantFiles[task.ID]
		if !ok {
			t.Errorf("unexpected task id %q", task.ID)
			continue
		}
		if len(task.Files) != len(files) {
			t.Errorf("%s: %d files want %d", task.ID, len(task.Files), len(files))
		}
		for _, name := range files {
			if task.Files[name] == "" {
				t.Errorf("%s: missing artifact %s", task.ID, name)
			}
		}
		for _, name := range wantProtected[task.ID] {
			if task.Protected[name] == "" {
				t.Errorf("%s: missing protected %s", task.ID, name)
			}
			// 受保护项必须与工作区初始内容一致，否则 gradeBenchTask 会误报。
			if task.Files[name] != task.Protected[name] {
				t.Errorf("%s: protected %s differs from initial content", task.ID, name)
			}
		}
	}
}

// 评分器确实按任务分派：把某任务的原始产物喂给它自己的 grader，
// 应得到该任务特有的诊断，而不是「文件不存在」。
func TestBenchTaskGradersMatchTheirOwnArtifacts(t *testing.T) {
	byID := map[string]benchTask{}
	for _, task := range benchTasks() {
		byID[task.ID] = task
	}

	for _, tc := range []struct {
		id     string
		expect string
	}{
		{"bugfix", "缺陷"},
		{"debug", "与原始文件完全一致"},
		{"refactor", "共享模块"},
	} {
		task := byID[tc.id]
		_, total, failures := task.Grader(task.Files)
		if total == 0 {
			t.Errorf("%s: grader produced no checks", tc.id)
		}
		joined := strings.Join(failures, " | ")
		if !strings.Contains(joined, tc.expect) {
			t.Errorf("%s: grader diagnostics %q missing %q", tc.id, joined, tc.expect)
		}
	}

	// 无产物的任务，其 grader 面对空工作区应报缺文件而非 panic。
	for _, id := range []string{"algorithm", "shift", "route"} {
		task := byID[id]
		if _, total, _ := task.Grader(task.Files); total == 0 {
			t.Errorf("%s: grader produced no checks on empty workspace", id)
		}
	}
}

// 受保护产物未被改动时 gradeBenchTask 应放行；被改动则报告。
func TestBenchTasksProtectedArtifactsRoundTrip(t *testing.T) {
	for _, task := range benchTasks() {
		if len(task.Protected) == 0 {
			continue
		}
		if got := gradeBenchTask(task.Protected, task.Files); got != "" {
			t.Errorf("%s: pristine workspace flagged: %s", task.ID, got)
		}
		tampered := map[string]string{}
		for name, content := range task.Files {
			tampered[name] = content
		}
		for name := range task.Protected {
			tampered[name] = "tampered"
		}
		if got := gradeBenchTask(task.Protected, tampered); got == "" {
			t.Errorf("%s: tampering not detected", task.ID)
		}
	}
}
