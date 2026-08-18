package web

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"m365-copilot2api/internal/chathub"
)

// benchTask is the public task/run representation consumed by the APK-derived
// administration UI. The task artifacts and grading function inventory are
// retained in the APK's benchmark.go / benchmark_cases.go pclntab entries.
type benchTask struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Category string `json:"category"`
}

type benchTaskResult struct {
	benchTask
	Status     string   `json:"status"`
	NetScore   float64  `json:"netScore"`
	Passed     int      `json:"passed"`
	Floor      int      `json:"floor"`
	Total      int      `json:"total"`
	Steps      int      `json:"steps"`
	Redundant  int      `json:"redundant"`
	ElapsedMS  int64    `json:"elapsedMs"`
	WroteFiles bool     `json:"wroteFiles"`
	TestsPass  bool     `json:"testsPass"`
	TestRuns   int      `json:"testRuns"`
	Failures   []string `json:"failures,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type benchmarkRun struct {
	State          string             `json:"state"`
	Current        string             `json:"current,omitempty"`
	Model          string             `json:"model,omitempty"`
	Effort         string             `json:"effort,omitempty"`
	Average        float64            `json:"average"`
	CodingScore    float64            `json:"codingScore"`
	ReasoningScore float64            `json:"reasoningScore"`
	Tasks          []benchTaskResult  `json:"tasks"`
	Log            []string           `json:"log"`
	StartedAt      time.Time          `json:"startedAt,omitempty"`
	FinishedAt     time.Time          `json:"finishedAt,omitempty"`
	Cancellation   context.CancelFunc `json:"-"`
}

type benchmarkStore struct {
	mu  sync.Mutex
	run benchmarkRun
}

// benchTasks returns the eight APK benchmark lanes. Four are coding tasks and
// four are reasoning/data tasks; the UI starts the coding subset with the first
// four identifiers.
func benchTasks() []benchTask {
	return []benchTask{
		{ID: "bugfix", Title: "库存预约修复", Detail: "修复库存预留、释放和审计轨迹中的契约违例。", Category: "coding"},
		{ID: "debug", Title: "统计报告调试", Detail: "根据运行报错定位并修复统计报告模块。", Category: "coding"},
		{ID: "refactor", Title: "用户数据重构", Detail: "合并用户/员工加载逻辑并保持数据契约。", Category: "coding"},
		{ID: "algorithm", Title: "区间算法", Detail: "实现并验证区间处理的正确性与复杂度。", Category: "coding"},
		{ID: "shift", Title: "排班推理", Detail: "根据规则与人员约束给出可验证的排班结论。", Category: "reasoning"},
		{ID: "sales", Title: "销售分析", Detail: "从销售数据中计算指标并解释异常。", Category: "reasoning"},
		{ID: "ledger", Title: "账本推理", Detail: "处理账本记录、无效操作和余额约束。", Category: "reasoning"},
		{ID: "route", Title: "路径规划", Detail: "在给定图与约束下求解路径结果。", Category: "reasoning"},
	}
}

// benchWeightedAverage uses the APK/UI split: coding contributes 60%,
// reasoning contributes 40%. A missing category has zero contribution rather
// than silently renormalising the other category.
func benchWeightedAverage(tasks []benchTaskResult) (average, coding, reasoning float64) {
	var codingSum, reasoningSum float64
	var codingN, reasoningN int
	for _, task := range tasks {
		switch task.Category {
		case "coding":
			codingSum += task.NetScore
			codingN++
		case "reasoning":
			reasoningSum += task.NetScore
			reasoningN++
		}
	}
	if codingN > 0 {
		coding = codingSum / float64(codingN)
	}
	if reasoningN > 0 {
		reasoning = reasoningSum / float64(reasoningN)
	}
	average = coding*0.60 + reasoning*0.40
	return average, coding, reasoning
}

func (s *benchmarkStore) snapshot() benchmarkRun {
	if s == nil {
		return benchmarkRun{State: "idle"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.run
	out.Tasks = append([]benchTaskResult(nil), s.run.Tasks...)
	for i := range out.Tasks {
		out.Tasks[i].Failures = append([]string(nil), s.run.Tasks[i].Failures...)
	}
	out.Log = append([]string(nil), s.run.Log...)
	out.Cancellation = nil
	return out
}

func (s *benchmarkStore) logf(format string, args ...any) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line := fmt.Sprintf(format, args...)
	s.run.Log = append(s.run.Log, line)
	// Keep memory bounded while preserving enough context for the UI log pane.
	if len(s.run.Log) > 500 {
		s.run.Log = append([]string(nil), s.run.Log[len(s.run.Log)-500:]...)
	}
}

func (s *benchmarkStore) update(fn func(*benchmarkRun)) {
	if s == nil || fn == nil {
		return
	}
	s.mu.Lock()
	fn(&s.run)
	s.run.Average, s.run.CodingScore, s.run.ReasoningScore = benchWeightedAverage(s.run.Tasks)
	s.mu.Unlock()
}

func (s *benchmarkStore) stop() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run.State != "running" || s.run.Cancellation == nil {
		return false
	}
	s.run.Cancellation()
	s.run.State = "cancelled"
	s.run.FinishedAt = time.Now().UTC()
	return true
}

func benchTaskCatalog() []benchTask {
	return append([]benchTask(nil), benchTasks()...)
}

// benchWorkspace is the in-memory task filesystem used by the APK benchmark
// agent. It deliberately does not expose host commands: benchmark tool calls
// may only inspect or modify the supplied task artifacts.
type benchWorkspace struct {
	mu        sync.Mutex
	files     map[string]string
	testRuns  int
	testsPass bool
	wrote     bool
	steps     int
	redundant int
	test      func(map[string]string) bool
}

func newBenchWorkspace(files map[string]string) *benchWorkspace {
	workspace := &benchWorkspace{files: make(map[string]string)}
	for name, content := range files {
		if clean, err := cleanBenchPath(name); err == nil {
			workspace.files[clean] = content
		}
	}
	return workspace
}

// cleanBenchPath follows the APK's defensive path normalization: convert
// Windows separators, reject empty/absolute/traversing paths, and retain a
// clean slash-separated relative artifact name.
func cleanBenchPath(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("invalid benchmark path")
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("benchmark path escapes workspace")
	}
	return clean, nil
}

func (w *benchWorkspace) snapshot() map[string]string {
	if w == nil {
		return map[string]string{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]string, len(w.files))
	for name, content := range w.files {
		out[name] = content
	}
	return out
}

func (w *benchWorkspace) testStatus() (passed bool, runs int) {
	if w == nil {
		return false, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.testsPass, w.testRuns
}

func (w *benchWorkspace) setTest(fn func(map[string]string) bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.test = fn
	w.mu.Unlock()
}

// benchToolSchema returns the APK benchmark's constrained task tools. The
// model cannot invoke arbitrary command execution through this schema.
func benchToolSchema() []chathub.Tool {
	definition := func(name, description string, parameters map[string]any) chathub.Tool {
		body, _ := json.Marshal(map[string]any{
			"name":        name,
			"description": description,
			"parameters":  parameters,
		})
		return chathub.Tool{Type: "function", Function: body}
	}
	pathParam := map[string]any{
		"type":       "object",
		"properties": map[string]any{"path": map[string]any{"type": "string"}},
		"required":   []string{"path"},
	}
	writeParam := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		},
		"required": []string{"path", "content"},
	}
	return []chathub.Tool{
		definition("list_files", "List benchmark workspace files.", map[string]any{"type": "object", "properties": map[string]any{}}),
		definition("read_file", "Read one benchmark workspace file.", pathParam),
		definition("write_file", "Write one benchmark workspace file.", writeParam),
		definition("run_tests", "Run the benchmark task checks against the current workspace.", map[string]any{"type": "object", "properties": map[string]any{}}),
	}
}

// execute applies one constrained benchmark tool call and returns a structured
// tool response. The implementation is intentionally deterministic so hidden
// graders can assess artifact changes rather than host-side side effects.
func (w *benchWorkspace) execute(name string, arguments map[string]any) (map[string]any, error) {
	if w == nil {
		return nil, fmt.Errorf("benchmark workspace unavailable")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.steps++

	asPath := func() (string, error) {
		raw, ok := arguments["path"].(string)
		if !ok {
			return "", fmt.Errorf("path is required")
		}
		return cleanBenchPath(raw)
	}

	switch name {
	case "list_files":
		files := make([]string, 0, len(w.files))
		for file := range w.files {
			files = append(files, file)
		}
		sort.Strings(files)
		return map[string]any{"files": files}, nil
	case "read_file":
		file, err := asPath()
		if err != nil {
			return nil, err
		}
		content, ok := w.files[file]
		if !ok {
			return nil, fmt.Errorf("file not found: %s", file)
		}
		return map[string]any{"path": file, "content": content}, nil
	case "write_file":
		file, err := asPath()
		if err != nil {
			return nil, err
		}
		content, ok := arguments["content"].(string)
		if !ok {
			return nil, fmt.Errorf("content is required")
		}
		if previous, exists := w.files[file]; exists && previous == content {
			w.redundant++
		} else {
			w.wrote = true
			w.files[file] = content
		}
		return map[string]any{"path": file, "bytes": len(content)}, nil
	case "run_tests":
		w.testRuns++
		if w.test != nil {
			files := make(map[string]string, len(w.files))
			for file, content := range w.files {
				files[file] = content
			}
			w.testsPass = w.test(files)
		} else {
			w.testsPass = false
		}
		return map[string]any{"passed": w.testsPass, "runs": w.testRuns}, nil
	default:
		return nil, fmt.Errorf("unsupported benchmark tool: %s", name)
	}
}
