package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// recordBenchUsage 把一次评测调用记入用量日志。它被 benchChat 以 defer
// 方式调用，因此即使请求失败也会留下记录。
//
// APK 证据（tools/apktool，benchmark.go:421-499，1968 字节）：
//   - 调用图：contentToString / json.Unmarshal / resolveAccount /
//     time.Now / (*usageLog).record / fmt.Sprint / toolArgumentsJSON；
//   - map 键（长度由 ORR 立即数解出）：
//     "choices"(7) / "content"(7) / "reasoning_content"(17) /
//     "tool_calls"(10) / "arguments"(9)；
//   - +0x0384 MOVZ #502 与 +0x0390 MOVZ #200 经 CSEL 选择：
//     出错记 502，否则记 200；
//   - 输出侧统计把 content、reasoning_content 与各 tool_call 的
//     arguments 长度累加（+0x0128 的 ADD 累加链）；
//   - endpoint 记为 "internal-benchmark"：该串以 18 字节存在于 rodata
//     0x4ec96a，但全项目机器码中零引用（已穷举 ADRP+ADD 配对确认），
//     实际来自 assets/web/index.html 的用量分类文案
//     「评测消耗会记入「用量」，归类为 internal-benchmark」。
//     前端 t.* 字段集与 benchTaskResult 完全一致，可作为 JSON 契约依据。
func (s *Server) recordBenchUsage(model, effort string, response map[string]any, elapsed time.Duration, callErr error) {
	status := http.StatusOK
	if callErr != nil {
		status = http.StatusBadGateway
	}
	outputChars := 0
	if choices, ok := response["choices"].([]any); ok {
		for _, entry := range choices {
			choice, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			message, ok := choice["message"].(map[string]any)
			if !ok {
				continue
			}
			outputChars += len(contentToString(message["content"]))
			if reasoning, ok := message["reasoning_content"].(string); ok {
				outputChars += len(reasoning)
			}
			calls, ok := message["tool_calls"].([]any)
			if !ok {
				continue
			}
			for _, raw := range calls {
				call, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				outputChars += len(toolArgumentsJSON(call))
			}
		}
	}
	email := ""
	if account, err := s.resolveAccount(""); err == nil {
		email = account.Email
	}
	if s.usage == nil {
		return
	}
	s.usage.record(UsageRecord{
		Time:         time.Now().UTC(),
		AccountEmail: email,
		Model:        model,
		Endpoint:     "internal-benchmark",
		InputTokens:  0,
		OutputTokens: int64(outputChars / 4),
		DurationMs:   elapsed.Milliseconds(),
		Status:       status,
	})
}

// benchChat 在 callOwnChatCompletions 之上包一层评测专用的请求构造与
// 响应解析：装配 OpenAI 兼容 payload、发起进程内自调用、把响应解析为
// 通用 map，并在 defer 中记账。
//
// APK 证据（tools/apktool，benchmark.go:378-410，1600 字节）：
//   - 调用图：benchToolSchema / json.Marshal / time.Now /
//     callOwnChatCompletions / time.Since / json.Unmarshal /
//     fmt.Sprint / compactToolResult / fmt.Errorf；
//     func1 闭包（396-398 行）内调用 recordBenchUsage；
//   - payload 键（长度由 ORR 立即数解出）：
//     "model"(5) / "stream"(6)=false / "messages"(8) / "tools"(5) /
//     "reasoning_effort"(16)，其中 effort 前有 CBZ 保护，空值不写入；
//   - +0x0274 MOVZ+MOVK 组出超时 240000000000 ns，即 4 分钟；
//   - +0x03b4 取响应中的 "error"(5) 键，非空时经 fmt.Sprint 转字符串、
//     compactToolResult 截断到 300 字节（+0x03e8 MOVZ #300），
//     再由 fmt.Errorf("%s") 包装；
//   - +0x0508 另有 "HTTP %d: %s" 用于非 2xx 状态；
//   - 返回 (map[string]any, 耗时, error)：runBenchTask +0x0444 之后
//     以 CBNZ x2 判错、对 x0 做 mapaccess1_faststr。
func (s *Server) benchChat(ctx context.Context, model, effort string, messages []map[string]any) (map[string]any, time.Duration, error) {
	payload := map[string]any{"model": model, "stream": false, "messages": messages, "tools": benchToolSchema()}
	if effort != "" {
		payload["reasoning_effort"] = effort
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	var parsed map[string]any
	var elapsed time.Duration
	var callErr error
	defer func() {
		s.recordBenchUsage(model, effort, parsed, elapsed, callErr)
	}()
	started := time.Now()
	raw, status, callErr := s.callOwnChatCompletions(ctx, body, 4*time.Minute)
	elapsed = time.Since(started)
	if callErr != nil {
		return nil, elapsed, callErr
	}
	if status < 200 || status >= 300 {
		callErr = fmt.Errorf("HTTP %d: %s", status, compactToolResult(string(raw), 300))
		return nil, elapsed, callErr
	}
	if callErr = json.Unmarshal(raw, &parsed); callErr != nil {
		return nil, elapsed, callErr
	}
	if failure, ok := parsed["error"]; ok && failure != nil {
		callErr = fmt.Errorf("%s", compactToolResult(fmt.Sprint(failure), 300))
		return nil, elapsed, callErr
	}
	return parsed, elapsed, nil
}

// callOwnChatCompletions 以进程内自调用方式走一遍 /v1/chat/completions，
// 使评测复用与外部客户端完全相同的请求路径。
//
// APK 证据（tools/apktool，benchmark.go:362-374，1200 字节）：
//   - 调用图：context.WithTimeout / httptest.NewRequestWithContext /
//     CanonicalMIMEHeaderKey / mapassign_faststr / makechan / newproc /
//     selectgo；func1 闭包（369 行）内调用 (*Server).openaiChat 后 closechan；
//   - +0x0058 MOV x2,x4 表明超时时长来自入参，非常量；
//   - +0x00d0 method 为 "POST"（ORR 立即数解出长度 4），
//     +0x00dc path 为 "/v1/chat/completions"（MOVZ x5,#20）；
//   - +0x019c/+0x01c8 设置 "Content-Type"(12) = "application/json"(16)；
//   - +0x0284 MOVZ #200 为 recorder 默认状态码；
//   - +0x034c selectgo 两个 case（ORR 解出 2）：完成与 ctx.Done()；
//   - 返回三组值：body 切片、状态码、error。
func (s *Server) callOwnChatCompletions(ctx context.Context, payload []byte, timeout time.Duration) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.openaiChat(recorder, request); close(done) }()
	select {
	case <-done:
		return recorder.Body.Bytes(), recorder.Code, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

// gradeBenchTask 校验受保护输入未被篡改。它不做打分，也不路由任何评分器；
// 返回空串表示通过，否则返回诊断信息。
//
// APK 证据（tools/apktool，benchmark.go:505-515，624 字节）：
//   - 调用图仅含 mapiterinit / mapiternext / newobject / cleanBenchPath /
//     mapaccess2_faststr / memequal / concatstring2，无任何 grade* 调用；
//   - 返回单值经 runBenchTask +0x01d4 的 convTstring 处理，故返回类型为
//     string，而非结构体；
//   - +0x0104 对被遍历 map 的每个键调用 cleanBenchPath 规范化后，
//     +0x011c 在第二个 map 中查找同名项；
//   - +0x0124 先比较长度（LDR x2,[x0,#8] 与栈上原长度），长度不等即判定
//     被修改，长度相等才在 +0x0144 调用 memequal 比较内容；
//   - +0x0168 引用长度 26 的前缀 "受保护输入被修改: "（MOVZ x2,#26），
//     经 concatstring2 与文件名拼接，前缀在前。
func gradeBenchTask(protected, submitted map[string]string) string {
	for name, original := range protected {
		clean, err := cleanBenchPath(name)
		if err != nil {
			continue
		}
		if current, ok := submitted[clean]; !ok || current != original {
			return "受保护输入被修改: " + name
		}
	}
	return ""
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
