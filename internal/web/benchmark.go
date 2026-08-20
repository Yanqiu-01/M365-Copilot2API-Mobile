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
// benchTask 描述一条评测任务。前四个字段参与 JSON 序列化，供前端渲染；
// 后三个是执行期数据，不出现在 API 响应中。
//
// APK 字段布局（元素大小 112 字节，由 benchTasks 的 STR 偏移实测）：
//
//	+0/+8 ID   +16/+24 Title   +32/+40 Detail   +48/+56 Category
//	+64 Files  +72 Grader      +80 Protected
//
// grader 存储偏移序列 72/184/296/408/520/632/744/856，相邻差恒为 112，
// 故元素内偏移为 72。各任务的写入模式与语义逐项吻合：
//
//	i0 bugfix     +64 +72          （无受保护文件）
//	i1 debug      +64 +72 +80
//	i2 refactor   +64 +72 +80
//	i3 algorithm      +72          （无初始产物）
//	i4 shift          +72
//	i5 sales      +64 +72 +80
//	i6 ledger     +64 +72 +80
//	i7 route          +72
//
// +88..+111 这 24 字节在 benchTasks 中从未被写入，为零值字段，
// 其用途暂无证据可判，故不揣测补入。
//
// 前端 index.html 只引用 id/title/detail/category，与此处 json 标签一致。
type benchTask struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Category string `json:"category"`

	// Files 是交给模型的初始工作区内容（元素内偏移 +64）。
	Files map[string]string `json:"-"`
	// Grader 是该任务的隐藏评分器（+72）。APK 中八个 grade* 函数均无 BL
	// 直接调用，而是以 funcval 存入 .data.rel.ro（0x136c948-0x136c998），
	// 由 benchTasks 挂载到此字段后间接调用。
	Grader func(map[string]string) (int, int, []string) `json:"-"`
	// Protected 记录不得被篡改的输入（+80），由 gradeBenchTask 校验。
	Protected map[string]string `json:"-"`
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
//
// APK 证据：八个 grade* 函数在 APK 中均无 BL 直接调用，而是以 funcval 存入
// .data.rel.ro（0x136c948-0x136c998），由本函数挂载到各任务的 Grader 字段。
// 挂载顺序由 benchTasks 的 ADRP 0x136c000 引用点确定：
//
//	+0x00ec gradeInventory  +0x01bc gradeDebug     +0x0348 gradeRefactor
//	+0x03d8 gradeIntervals  +0x03e4 gradeShift     +0x046c gradeLedger
//	+0x0568 gradeSales      +0x05f0 gradeRoute
//

// 以下八条任务提示词逐字节取自原 APK 的 rodata（tools/apktool 提取
// libm365.so，以任务开头与收尾句为锚切段）。恢复初期误将它们压缩成一句话
// 概述，模型因此不知道该产出哪个文件、用什么格式 —— 推理类任务遂在第 1 步
// 就「未写任何文件」结束，评分器读不到 schedule.json / report.json /
// route.json / state.json，得分恒为 0。
const (
	benchDetailBugfix = `工作区有 inventory.py，类的 docstring 写明了正确的行为契约（CONTRACT）。
当前实现违反了其中五条。

请先用 read_file 读 inventory.py，逐条对照契约找出全部五处缺陷，然后用 write_file
写回修正后的完整文件。不要改类名、方法名、异常名，也不要改 docstring。

关键点提示（必须自己判断具体是哪几处）：
- 数量的边界值校验
- 失败操作是否应该写入 trail
- 未知 sku 应该抛什么异常
- 预留是否可能被重复占用
- release 之后 reserved 是否可能变成负数

改完后必须再调用 read_file 读回 inventory.py，逐条复核契约。
只声明“已修复”但没有写文件将被判为未完成。`

	benchDetailDebug = `工作区有两个文件：

- stats.py：待修复的源码
- run_report.txt：真实运行时产生的报错记录（只读，不要修改）

请先 read_file 读 run_report.txt，看清报错，再 read_file 读 stats.py，
然后修复所有问题并用 write_file 写回 stats.py。

必须同时满足：
1. 修掉 f-string 语法错误，mean 保留两位小数
2. summarize([]) 不再抛异常：count=0，total=0，mean=None，max=None
3. summarize 非空输入行为不变
4. format_report 在空列表时也不能崩

修好后再 read_file 读回 stats.py 自查一遍。反复读报错、改代码，直到自己确认通过。`

	benchDetailRefactor = `工作区有 users.py 和 staff.py，其中 load_users 与 load_staff 的逻辑几乎完全重复
（读 JSON、校验字段、规范化名字、排序）。people.json 是测试数据，只读，不要修改。

请把公共逻辑抽取到一个新的共享模块，然后让两个文件都改成调用它。要求：

- load_users 和 load_staff 的函数名与外部行为必须完全不变
- 两个入口文件重构后各自不超过 12 行
- 校验规则（年龄 0-150、名字必须是非空字符串等）只允许出现在共享模块里
- 不要留下重复的校验代码

改完后 read_file 读回三个文件确认。`

	benchDetailAlgorithm = `请实现区间运算，使用半开区间 [start, end)。

在工作区创建 intervals.py，提供两个函数：

1. merge(intervals) -> list[tuple[int, int]]
   合并所有重叠或相邻的区间，按 start 升序返回。
   输入可能无序、重复、嵌套、为空，也可能含负数。
   相邻视为可合并：[(1,4),(4,5)] -> [(1,5)]

2. subtract(a, b) -> list[tuple[int, int]]
   从区间集合 a 中减去区间集合 b，返回剩余部分，按 start 升序。
   例：subtract([(1,10)], [(4,6)]) == [(1,4),(6,10)]

复杂度要求：两个函数都必须能处理 100000 个区间的输入。
必须先排序再线性扫描，禁止对每个点或每个整数做逐一枚举，
也禁止使用 set 展开区间内的所有整数（会超时和爆内存）。

然后在同一目录创建 notes.json，写明你的复杂度，格式严格如下（不要代码围栏）：

{"mergeComplexity":"O(n log n)","subtractComplexity":"O(n log n)","approach":"一句话说明做法"}

写完后 read_file 读回两个文件自查。`

	benchDetailShift = `排班问题。四个人 Ann、Ben、Cara、Dan，四个班次 Mon、Tue、Wed、Thu。
每人恰好排一个班次，每个班次恰好一个人。约束：

1. Ann 不能排 Mon。
2. Ben 只能排 Mon 或 Tue。
3. Cara 的班次必须比 Dan 的班次晚（顺序 Mon < Tue < Wed < Thu）。
4. Dan 不排 Tue。
5. Wed 的人名字必须是四个字母（Ann 三个、Ben 三个、Cara 四个、Dan 三个）。

请推理出唯一解，然后调用 write_file 把结果写成 schedule.json，格式严格如下（不要加注释、不要用代码围栏）：

{"Mon":"名字","Tue":"名字","Wed":"名字","Thu":"名字"}

写完后调用 read_file 读回 schedule.json 自查一遍，确认每条约束都成立。`

	benchDetailSales = `工作区里有 sales.csv，列为 date,region,product,units,unit_price。

请先用 read_file 读取它，然后计算：
1. 每个 region 的总营收（营收 = units × unit_price）
2. 营收最高的月份，格式 "YYYY-MM"
3. 全部营收合计
4. 营收最高的 region

把结果调用 write_file 写成 report.json，格式严格如下（数字用数值，不要字符串，不要代码围栏）：

{"revenueByRegion":{"north":0,"south":0,"east":0},"topMonth":"YYYY-MM","totalRevenue":0,"topRegion":"名称"}

注意 unit_price 是小数，务必逐行累加，不要估算。写完后 read_file 读回自查。`

	benchDetailLedger = `账务重放问题。工作区有 ledger.txt，每行一条操作，按顺序执行。
账户初始余额都是 0。规则：

- DEPOSIT <账户> <金额>：存入。金额必须 > 0，否则整条跳过并计一次 rejected。
- WITHDRAW <账户> <金额>：取出。金额必须 > 0 且不超过当前余额，
  否则整条跳过并计一次 rejected（余额绝不能变负）。
- TRANSFER <源> <目标> <金额>：转账，必须原子。
  如果源余额不足或金额 <= 0，整条跳过并计一次 rejected，两个账户都不变。

请先 read_file 读取 ledger.txt，逐行重放，然后 write_file 写出 state.json，
格式严格如下（余额用数值，不要代码围栏）：

{"balances":{"A":0,"B":0,"C":0},"rejected":0,"applied":0}

applied 是成功执行的操作条数，rejected 是被跳过的条数。两者相加应等于总行数。
写完后 read_file 读回自查，确认没有负余额、且 applied+rejected 等于总行数。`

	benchDetailRoute = `最短路问题。有向加权图，边如下（起点 终点 权重）：

A B 4
A C 2
B D 5
C B 1
C D 8
C E 10
D E 2
D F 6
E F 3

请求出 A 到 F 的最短路径和总权重，然后 write_file 写出 route.json，
格式严格如下（不要代码围栏）：

{"path":["A","...","F"],"cost":0}

path 是从 A 到 F 依次经过的节点，cost 是路径总权重。
注意存在多条候选路径，必须比较后取最小值。写完后 read_file 读回自查，
把 path 上相邻边的权重逐段相加，确认等于 cost。`
)

// 与下列任务顺序逐一对应。
func benchTasks() []benchTask {
	none := func() map[string]string { return map[string]string{} }
	return []benchTask{
		{ID: "bugfix", Title: "库存预约修复", Detail: benchDetailBugfix, Category: "coding",
			Files: map[string]string{"inventory.py": benchInventorySource}, Protected: none(), Grader: gradeInventory},
		{ID: "debug", Title: "统计报告调试", Detail: benchDetailDebug, Category: "coding",
			Files:     map[string]string{"stats.py": benchStatsSource, "run_report.txt": benchRunReport},
			Protected: map[string]string{"run_report.txt": benchRunReport}, Grader: gradeDebug},
		{ID: "refactor", Title: "用户数据重构", Detail: benchDetailRefactor, Category: "coding",
			Files:     map[string]string{"users.py": benchUsersSource, "staff.py": benchStaffSource, "people.json": benchPeopleJSON},
			Protected: map[string]string{"people.json": benchPeopleJSON}, Grader: gradeRefactor},
		// algorithm 无初始产物：rodata 中 "intervals.py" 仅在字符串池内出现，
		// gradeIntervals 首项检查即「intervals.py 存在」，要求从零创建。
		{ID: "algorithm", Title: "区间算法", Detail: benchDetailAlgorithm, Category: "coding",
			Files: none(), Protected: none(), Grader: gradeIntervals},
		{ID: "shift", Title: "排班推理", Detail: benchDetailShift, Category: "reasoning",
			Files: none(), Protected: none(), Grader: gradeShift},
		{ID: "sales", Title: "销售分析", Detail: benchDetailSales, Category: "reasoning",
			Files:     map[string]string{"sales.csv": benchSalesCSV},
			Protected: map[string]string{"sales.csv": benchSalesCSV}, Grader: gradeSales},
		{ID: "ledger", Title: "账本推理", Detail: benchDetailLedger, Category: "reasoning",
			Files:     map[string]string{"ledger.txt": benchLedgerText},
			Protected: map[string]string{"ledger.txt": benchLedgerText}, Grader: gradeLedger},
		{ID: "route", Title: "路径规划", Detail: benchDetailRoute, Category: "reasoning",
			Files: none(), Protected: none(), Grader: gradeRoute},
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
// APK 证据：execute +0x0238 以 ADD x20,x1,#16 从 benchWorkspace 偏移 16
// 取出 14 个寄存器宽的数据传给 gradeBenchTask，即此处内嵌的 benchTask。
// APK 的 benchWorkspace 只有 snapshot/testStatus/execute 三个方法，
// 没有 setTest —— 评分器经内嵌任务的 Grader 字段直接调用。
// 字段偏移：+136 testsPass、+144 testRuns（execute +0x0358/+0x0368）。
type benchWorkspace struct {
	mu        sync.Mutex
	task      benchTask
	files     map[string]string
	testRuns  int
	testsPass bool
	wrote     bool
	steps     int
	redundant int
}

func newBenchWorkspace(task benchTask) *benchWorkspace {
	workspace := &benchWorkspace{task: task, files: make(map[string]string)}
	for name, content := range task.Files {
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
	// 标记为内部自调用：openaiChat 末尾的 bindConversation 会据此跳过用量
	// 记账，避免与 recordBenchUsage 对同一次调用重复计入统计。
	request.Header.Set(internalCallHeader, "benchmark")
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
		// APK 在此处先解锁、取快照并校验受保护输入，再调用任务自己的
		// 评分器（execute +0x0230 snapshot、+0x02c8 gradeBenchTask）。
		files := make(map[string]string, len(w.files))
		for file, content := range w.files {
			files[file] = content
		}
		if tampered := gradeBenchTask(w.task.Protected, files); tampered != "" {
			w.testRuns++
			w.testsPass = false
			return map[string]any{"passed": false, "runs": w.testRuns, "output": tampered}, nil
		}
		passed, total, failures := 0, 0, []string(nil)
		if w.task.Grader != nil {
			passed, total, failures = w.task.Grader(files)
		}
		w.testRuns++
		w.testsPass = total > 0 && passed == total
		// +0x0430 "TESTS PASSED: %d/%d"(19) 经 fmt.Sprintf 生成，
		// 失败时改用 strings.Join 拼接诊断清单。
		output := fmt.Sprintf("TESTS PASSED: %d/%d", passed, total)
		if !w.testsPass {
			output = strings.Join(failures, "\n")
		}
		return map[string]any{"passed": w.testsPass, "runs": w.testRuns, "output": output}, nil
	default:
		return nil, fmt.Errorf("unsupported benchmark tool: %s", name)
	}
}

// benchMaxSteps 是单任务的工具循环上限。
// APK 证据：runBenchTask +0x03fc CMP #14，与前端「每项最多 14 步」一致。
const benchMaxSteps = 14

// maxForcedClosures 是「强制进入测试闭环」提示的连续上限。超过即停止推进，
// 把剩余步数留给真正的工作，而不是耗在同一句提示上。
const maxForcedClosures = 3

// benchCodingLoopPrompt 是编程任务追加的闭环要求，逐字节取自 APK
// runBenchTask +0x0288（0x51e7f4，267 字节）。仅当 category=="coding"
// 时经 concatstring2 追加到任务描述之后。
const benchCodingLoopPrompt = "\n\n编程闭环要求：修改代码后必须调用 run_tests。若测试失败，读取失败信息、重新检查相关文件、继续修改并再次运行 run_tests；只有看到 TESTS PASSED 才能结束。不要用文字声称测试通过来代替实际调用。"

// runBenchTask 执行一条评测任务的完整工具循环：装配工作区、逐步与模型
// 交互、执行受限工具调用，最后交隐藏评分器打分。
//
// APK 证据（tools/apktool，benchmark.go:518-644，6000 字节，127 行单体无闭包）。
//
// 调用序列：
//
//	time.Now → newBenchWorkspace → benchWorkspace.snapshot → gradeBenchTask →
//	benchmarkStore.logf → benchChat → compactToolResult →
//	benchWorkspace.testStatus → time.Since → toolArgumentsJSON →
//	json.Unmarshal → canonicalToolArguments → shouldSuppressCompletedCall →
//	benchWorkspace.execute
//
// 关键立即数与浮点常量：
//   - +0x03fc CMP #14：步数上限，与前端「每项最多 14 步」一致；
//   - +0x0250 CMP #6 判 "coding"，命中才追加 267 字节闭环提示；
//   - +0x0844 MOVZ #104：benchTaskResult 元素大小（growslice）；
//   - +0x104c LDR d0 与 +0x1054 LDR d1,[0x5be4f0]=100.0 后 FMUL，
//     即百分比 = netScore*100，对应日志尾部的 %.0f%%；
//   - +0x138c MOVZ #400 / +0x13a4 MOVZ #300：工具结果截断上限。
//
// 日志格式串（地址已核实）：
//
//	0x4f947c "[%s] %s，地板分 %d"
//	0x514455 "[%s] 第 %d 步提前结束，强制进入测试闭环"
//	0x50cd83 "[%s] 第 %d 步结束回答（%d ms）"
//	0x5100f7 "最新代码未通过 run_tests 闭环验证"
//	0x510123 "[%s] 原始 %d/%d，净得分 %d/%d = %.0f%%"
//
// +0x08e4 的 testStatus 只在编程任务分支调用，实现前端所述
// 「编程任务必须在最新代码上通过 run_tests 才计闭环分」。
func (s *Server) runBenchTask(ctx context.Context, task benchTask, model, effort string) benchTaskResult {
	started := time.Now()
	result := benchTaskResult{benchTask: task, Status: "running"}
	workspace := newBenchWorkspace(task)

	initial := workspace.snapshot()
	floor, _, _ := task.Grader(initial)
	result.Floor = floor
	s.benchmark.logf("[%s] %s，地板分 %d", task.ID, task.Title, floor)

	prompt := task.Detail
	if task.Category == "coding" {
		prompt += benchCodingLoopPrompt
	}
	messages := []map[string]any{{"role": "user", "content": prompt}}

	// 连续强制推进的次数上限，防止空转耗尽全部步数。
	forcedClosures := 0

	for step := 1; step <= benchMaxSteps; step++ {
		select {
		case <-ctx.Done():
			result.Status = "cancelled"
			result.ElapsedMS = time.Since(started).Milliseconds()
			return result
		default:
		}
		response, elapsed, err := s.benchChat(ctx, model, effort, messages)
		if err != nil {
			result.Status = "error"
			result.Error = err.Error()
			result.ElapsedMS = time.Since(started).Milliseconds()
			return result
		}
		choices, _ := response["choices"].([]any)
		if len(choices) == 0 {
			result.Status = "error"
			result.Error = "response contained no choices"
			result.ElapsedMS = time.Since(started).Milliseconds()
			return result
		}
		choice, _ := choices[0].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		content := contentToString(message["content"])
		calls, _ := message["tool_calls"].([]any)

		if len(calls) == 0 {
			// 编程任务若还没跑通测试就想收尾，强制再推一轮进入闭环。
			//
			// 推进次数必须设上限：模型可能持续不给工具调用（或其调用被上游
			// 与网关之间的某一环丢掉），此时每一轮都是一次完整的上游请求，
			// 14 步会全部空转在同一句提示上，最终因闭环未通过罚到地板分。
			// 实测运行日志里连续出现「第 N 步提前结束」直到步数耗尽即是此情形。
			if passed, runs := workspace.testStatus(); task.Category == "coding" && (!passed || runs == 0) {
				if forcedClosures >= maxForcedClosures {
					s.benchmark.logf("[%s] 第 %d 步：已连续 %d 次提示仍未进入闭环，停止推进",
						task.ID, step, forcedClosures)
					break
				}
				forcedClosures++
				s.benchmark.logf("[%s] 第 %d 步提前结束，强制进入测试闭环（第 %d/%d 次）",
					task.ID, step, forcedClosures, maxForcedClosures)
				messages = append(messages,
					map[string]any{"role": "assistant", "content": content},
					map[string]any{"role": "user", "content": benchCodingLoopPrompt})
				continue
			}
			s.benchmark.logf("[%s] 第 %d 步结束回答（%d ms）", task.ID, step, elapsed.Milliseconds())
			break
		}

		// 模型给出了工具调用，说明推进有效，重置计数。
		forcedClosures = 0
		messages = append(messages, map[string]any{"role": "assistant", "content": content, "tool_calls": calls})
		for _, raw := range calls {
			call, _ := raw.(map[string]any)
			function, _ := call["function"].(map[string]any)
			name := fmt.Sprint(function["name"])
			arguments := toolArgumentsJSON(call)
			var parsed map[string]any
			_ = json.Unmarshal([]byte(arguments), &parsed)
			reply := map[string]any{"role": "tool", "tool_call_id": fmt.Sprint(call["id"])}
			if output, err := workspace.execute(name, parsed); err != nil {
				reply["content"] = compactToolResult(err.Error(), 300)
			} else {
				encoded, _ := json.Marshal(output)
				reply["content"] = compactToolResult(string(encoded), 400)
			}
			messages = append(messages, reply)
		}
	}

	final := workspace.snapshot()
	if tampered := gradeBenchTask(task.Protected, final); tampered != "" {
		result.Status = "error"
		result.Error = tampered
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result
	}

	passed, total, failures := task.Grader(final)
	testsPass, testRuns := workspace.testStatus()
	// 闭环判定必须落在最终快照上。workspace.testsPass 记录的是「最后一次
	// run_tests 调用时」的结论，而 write_file 不会重置它：模型常见的轨迹是
	// 写代码 → run_tests 失败 → 改对 → 直接收尾，此时 testsPass 仍停留在
	// false，尽管当前代码已经满分。此前据此把 net 清成 floor，出现过
	// 「原始 10/10、地板 0、最终 0%」这种把做对的工作判成零分的结果。
	//
	// 因此改为：若最终快照本身已通过全部检查，就认定闭环达成（等价于在
	// 收尾处替模型补跑一次 run_tests，评分标准并未放宽）；只有最终代码确实
	// 不满分时，才追加闭环未通过的惩罚。
	if task.Category == "coding" && total > 0 && passed == total {
		testsPass = true
	}
	net := passed
	if task.Category == "coding" && !testsPass {
		failures = append(failures, "最新代码未通过 run_tests 闭环验证")
		net = floor
	}
	if net < floor {
		net = floor
	}
	result.Status = "done"
	result.Passed = passed
	result.Total = total
	result.Failures = failures
	result.Steps = workspace.steps
	result.Redundant = workspace.redundant
	result.WroteFiles = workspace.wrote
	result.TestsPass = testsPass
	result.TestRuns = testRuns
	result.ElapsedMS = time.Since(started).Milliseconds()
	if total > 0 {
		result.NetScore = float64(net) / float64(total)
	}
	s.benchmark.logf("[%s] 原始 %d/%d，净得分 %d/%d = %.0f%%", task.ID, passed, total, net, total, result.NetScore*100)
	return result
}

// startBenchmark 校验参数、抢占 running 状态，并在后台跑完整轮评测。
//
// APK 证据（benchmark.go:649-719，1184 字节，含 6 个闭包）：
//   - 调用图 Mutex.lockSlow / time.Now / time.Time.Format /
//     Mutex.unlockSlow / fmt.Errorf / strings.TrimSpace；
//   - +0x030c "已有评测在运行"(21, @0x4f6cda) 经 fmt.Errorf 返回：
//     已在运行时拒绝重入；
//   - +0x011c 时间格式 "20060102T150405Z"(16, @0x4e1913) 用于运行 ID；
//   - func1(666-743) 是主循环：遍历 benchTasks、按 ids 过滤、
//     调 runBenchTask、以 benchWeightedAverage 汇总；
//   - func1 +0x00e0 len=89 "开始评测：模型 %s，思考强度 %s（编程占 %.0f%%，纯 Go 隐藏评分器）"
//   - func1 +0x07f8 len=66 "评测结束：编程 %.0f%%，推理 %.0f%%，加权总分 %.0f%%"
//   - func1 +0x0464 整数化比较 "cancelled"、+0x0534 CMP #6 判 "coding"、
//     +0x0454 CMP #9 判 "reasoning"、+0x0480 CMP #100 为百分比换算；
//   - func1.1(667-681) 每任务状态更新，仅在 State 仍为 "running" 时写入
//     （+0x0144 CMP #7、+0x0178 CMP #103 即 'g'，为 "running" 的长度与尾字节）。
func (s *Server) startBenchmark(model, effort string, ids []string) error {
	normalizedEffort, err := benchmarkEffort(effort)
	if err != nil {
		return err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model is required")
	}

	selected := map[string]bool{}
	for _, id := range ids {
		selected[strings.TrimSpace(id)] = true
	}
	tasks := make([]benchTask, 0, len(benchTasks()))
	for _, task := range benchTasks() {
		if len(selected) == 0 || selected[task.ID] {
			tasks = append(tasks, task)
		}
	}
	if len(tasks) == 0 {
		return fmt.Errorf("no benchmark task selected")
	}

	if s.benchmark == nil {
		s.benchmark = &benchmarkStore{}
	}
	s.benchmark.mu.Lock()
	if s.benchmark.run.State == "running" {
		s.benchmark.mu.Unlock()
		return fmt.Errorf("已有评测在运行")
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := time.Now().UTC()
	s.benchmark.run = benchmarkRun{
		State:        "running",
		Model:        model,
		Effort:       normalizedEffort,
		StartedAt:    started,
		Cancellation: cancel,
		// 运行 ID 采用 APK 的 "20060102T150405Z" 格式。
		Current: started.Format("20060102T150405Z"),
	}
	s.benchmark.mu.Unlock()

	codingCount := 0
	for _, task := range tasks {
		if task.Category == "coding" {
			codingCount++
		}
	}
	codingShare := 0.0
	if len(tasks) > 0 {
		codingShare = float64(codingCount) / float64(len(tasks)) * 100
	}
	s.benchmark.logf("开始评测：模型 %s，思考强度 %s（编程占 %.0f%%，纯 Go 隐藏评分器）",
		model, normalizedEffort, codingShare)

	go func() {
		defer cancel()
		for _, task := range tasks {
			s.benchmark.update(func(run *benchmarkRun) {
				if run.State == "running" {
					run.Current = task.ID
				}
			})
			result := s.runBenchTask(ctx, task, model, normalizedEffort)
			s.benchmark.update(func(run *benchmarkRun) {
				if run.State != "running" {
					return
				}
				run.Tasks = append(run.Tasks, result)
			})
			if result.Status == "cancelled" {
				break
			}
		}
		s.benchmark.update(func(run *benchmarkRun) {
			if run.State == "running" {
				run.State = "done"
			}
			run.Current = ""
			run.FinishedAt = time.Now().UTC()
			run.Cancellation = nil
		})
		final := s.benchmark.snapshot()
		s.benchmark.logf("评测结束：编程 %.0f%%，推理 %.0f%%，加权总分 %.0f%%",
			final.CodingScore*100, final.ReasoningScore*100, final.Average*100)
	}()
	return nil
}
