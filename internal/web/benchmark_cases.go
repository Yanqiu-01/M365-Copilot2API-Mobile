package web

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// gradeReport is the APK benchmark's compact assertion collector. Each check
// contributes one raw point and retains a short diagnostic for the UI.
type gradeReport struct {
	passed   int
	total    int
	failures []string
}

func (r *gradeReport) eq(label string, got, want any) {
	r.check(fmt.Sprint(got) == fmt.Sprint(want), "%s: got %s, want %s", label, trimForLog(fmt.Sprint(got)), trimForLog(fmt.Sprint(want)))
}

func (r *gradeReport) check(ok bool, format string, args ...any) {
	r.total++
	if ok {
		r.passed++
		return
	}
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func (r *gradeReport) numEq(label string, got any, want float64) {
	r.total++
	actual, ok := asFloat(got)
	if ok && math.Abs(actual-want) < 1e-9 {
		r.passed++
		return
	}
	r.failures = append(r.failures, fmt.Sprintf("%s: got %s, want %g", label, trimForLog(fmt.Sprint(got)), want))
}

func (r *gradeReport) tally() (passed, total int, failures []string) {
	return r.passed, r.total, append([]string(nil), r.failures...)
}

func trimForLog(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 240 {
		return value
	}
	return value[:240] + "…"
}

func asFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		result, err := v.Float64()
		return result, err == nil
	}
	return 0, false
}

func readJSONArtifact(files map[string]string, name string) (map[string]any, error) {
	content, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("missing %s", name)
	}
	content = strings.TrimSpace(stripJSONFence(content))
	var value map[string]any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", name, err)
	}
	return value, nil
}

func stripJSONFence(content string) string {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "```") {
		return content
	}
	if end := strings.IndexByte(content, '\n'); end >= 0 {
		content = content[end+1:]
	}
	if end := strings.LastIndex(content, "```"); end >= 0 {
		content = content[:end]
	}
	return strings.TrimSpace(content)
}

func mapOfNumbers(value any) map[string]float64 {
	input, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]float64, len(input))
	for key, raw := range input {
		if number, ok := asFloat(raw); ok {
			out[key] = number
		}
	}
	return out
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// gradeShift verifies the APK scheduling task. The constraints embedded in
// the task/scorer yield the unique schedule Mon=Dan, Tue=Ben, Wed=Cara,
// Thu=Ann. The direct assignments and individual constraints are both kept in
// the report so a nearly-correct artifact still gets useful diagnostics.
func gradeShift(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	artifact, err := readJSONArtifact(files, "schedule.json")
	if err != nil {
		report.total = 9
		report.failures = append(report.failures, err.Error())
		return report.tally()
	}
	day := func(name string) string { return strings.TrimSpace(fmt.Sprint(artifact[name])) }
	mon, tue, wed, thu := day("Mon"), day("Tue"), day("Wed"), day("Thu")
	report.eq("Mon person", mon, "Dan")
	report.eq("Tue person", tue, "Ben")
	report.eq("Wed person", wed, "Cara")
	report.eq("Thu person", thu, "Ann")

	people := []string{mon, tue, wed, thu}
	seen := map[string]bool{}
	for _, person := range people {
		seen[strings.ToLower(person)] = true
	}
	report.total++
	if len(seen) == 4 && !seen[""] {
		report.passed++
	} else {
		report.failures = append(report.failures, "four assigned names must be distinct")
	}

	index := map[string]int{}
	for i, person := range people {
		index[strings.ToLower(person)] = i
	}
	report.total++
	if !strings.EqualFold(mon, "Ann") {
		report.passed++
	} else {
		report.failures = append(report.failures, "constraint 1: Ann must not be Mon")
	}
	report.total++
	if strings.EqualFold(mon, "Ben") || strings.EqualFold(tue, "Ben") {
		report.passed++
	} else {
		report.failures = append(report.failures, "constraint 2: Ben must be Mon or Tue")
	}
	report.total++
	if index["cara"] > index["dan"] {
		report.passed++
	} else {
		report.failures = append(report.failures, "constraint 3: Cara must be later than Dan")
	}
	report.total++
	if !strings.EqualFold(tue, "Dan") {
		report.passed++
	} else {
		report.failures = append(report.failures, "constraint 4: Dan must not be Tue")
	}
	report.total++
	if len([]rune(wed)) == 4 {
		report.passed++
	} else {
		report.failures = append(report.failures, "constraint 5: Wed must be a four-letter name")
	}
	return report.tally()
}

// gradeSales verifies the APK sales task. The source data embedded in the APK
// yields north=80, south=80, east=70, total=230, top month 2026-02; north and
// south are tied for top region and either value is accepted.
func gradeSales(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	artifact, err := readJSONArtifact(files, "report.json")
	if err != nil {
		report.total = 6
		report.failures = append(report.failures, err.Error())
		return report.tally()
	}
	revenue := mapOfNumbers(firstPresent(artifact, "revenueByRegion", "revenue_by_region"))
	report.numEq("north revenue", revenue["north"], 80)
	report.numEq("south revenue", revenue["south"], 80)
	report.numEq("east revenue", revenue["east"], 70)
	report.eq("top month", firstPresent(artifact, "topMonth", "top_month"), "2026-02")
	report.numEq("total revenue", firstPresent(artifact, "totalRevenue", "total_revenue"), 230)
	top := fmt.Sprint(firstPresent(artifact, "topRegion", "top_region"))
	report.total++
	if top == "north" || top == "south" {
		report.passed++
	} else {
		report.failures = append(report.failures, fmt.Sprintf("top region: got %s, want north or south", trimForLog(top)))
	}
	return report.tally()
}

func firstPresent(value map[string]any, keys ...string) any {
	for _, key := range keys {
		if found, ok := value[key]; ok {
			return found
		}
	}
	return nil
}

// gradeLedger verifies the ledger task's post-state. Valid operations produce
// A=0, B=115, C=30; four invalid operations are rejected and six applied.
func gradeLedger(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	artifact, err := readJSONArtifact(files, "state.json")
	if err != nil {
		report.total = 6
		report.failures = append(report.failures, err.Error())
		return report.tally()
	}
	balances := mapOfNumbers(firstPresent(artifact, "balances"))
	report.numEq("account A balance", balances["A"], 0)
	report.numEq("account B balance", balances["B"], 115)
	report.numEq("account C balance", balances["C"], 30)
	rejected := firstPresent(artifact, "rejected")
	switch value := rejected.(type) {
	case []any:
		report.numEq("rejected count", len(value), 4)
	default:
		report.numEq("rejected count", value, 4)
	}
	applied := firstPresent(artifact, "applied")
	switch value := applied.(type) {
	case []any:
		report.numEq("applied count", len(value), 6)
	default:
		report.numEq("applied count", value, 6)
	}
	report.total++
	if balances["A"] >= 0 && balances["B"] >= 0 && balances["C"] >= 0 {
		report.passed++
	} else {
		report.failures = append(report.failures, "negative balance")
	}
	return report.tally()
}

// gradeRoute verifies the unique shortest path in the APK graph:
// A-C-B-D-E-F with total cost 13.
func gradeRoute(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	artifact, err := readJSONArtifact(files, "route.json")
	if err != nil {
		report.total = 3
		report.failures = append(report.failures, err.Error())
		return report.tally()
	}
	path := stringSlice(firstPresent(artifact, "path"))
	report.eq("shortest path", strings.Join(path, "-"), "A-C-B-D-E-F")
	report.numEq("shortest cost", firstPresent(artifact, "cost"), 13)
	report.total++
	if len(path) >= 2 && path[0] == "A" && path[len(path)-1] == "F" {
		report.passed++
	} else {
		report.failures = append(report.failures, "path endpoints must be A to F")
	}
	return report.tally()
}

// gradeInventory verifies the APK inventory.py source task. The APK grader
// awards one point for the file/contract identity and one for each of the five
// explicitly described defects. It uses source order for the trail check so a
// superficial presence check cannot pass the original bug.
func gradeInventory(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	source, ok := files["inventory.py"]
	if !ok {
		report.total = 7
		report.failures = append(report.failures, "inventory.py 不存在")
		return report.tally()
	}

	// The task requires preserving both the class and exception names. The APK
	// keeps the original docstring as a separate check and explicitly looks for
	// the original CONTRACT marker.
	report.total++
	if strings.Contains(source, "class Inventory") && strings.Contains(source, "class StockError") {
		report.passed++
	} else {
		report.failures = append(report.failures, "类名与异常名保留")
	}
	report.total++
	if strings.Contains(source, "CONTRACT") {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺少 CONTRACT")
	}

	addBody := functionBody(source, "def add")
	reserveBody := functionBody(source, "def reserve")
	releaseBody := functionBody(source, "def release")
	availableBody := functionBody(source, "def available")

	// Defect 1: the lower-bound guard must reject zero. The original source had
	// `qty < 0`; accept the APK-observed repaired forms.
	report.total++
	if rejectsNonPositiveQty(addBody) {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷1 add 拒绝 qty=0")
	}

	// Defect 2: failed reserve must not append to trail. If an append remains,
	// all validation must precede it.
	report.total++
	if trailOnlyOnSuccess(reserveBody) {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷2 失败操作不写入 trail")
	}

	// Defect 3: unknown SKUs must raise KeyError.
	report.total++
	if raisesKeyErrorForUnknownSKU(reserveBody) {
		report.passed++
	} else {
		report.failures = append(report.failures, "未见 KeyError")
	}

	// Defect 4: reservation must compare against available stock, not merely
	// on_hand. The exact available expression is present in the APK strings.
	report.total++
	if reservesAgainstAvailable(reserveBody) {
		report.passed++
	} else {
		report.failures = append(report.failures, "预留未依据可用量")
	}

	// Defect 5: release must clamp or otherwise guard the lower bound.
	report.total++
	// 常见的正确写法有多种：max(0, ...)、min(qty, reserved)、先比较再减、
	// 或在 available 侧夹紧。此前列举过窄，正确实现也会被判缺陷。
	if releaseGuardsLowerBound(releaseBody) || releaseGuardsLowerBound(availableBody) {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷5 release 后 reserved 不为负")
	}

	return report.tally()
}

// raisesKeyErrorForUnknownSKU 判断未知 sku 是否会抛 KeyError。
//
// 除了显式 raise KeyError，直接下标访问 self.on_hand[sku] / self.reserved[sku]
// 在 Python 中本身就抛 KeyError，这是惯用且正确的写法。此前只认字面
// "KeyError"，直接索引的正确实现被判为未修复。
func raisesKeyErrorForUnknownSKU(body string) bool {
	if body == "" {
		return false
	}
	if strings.Contains(body, "KeyError") {
		return true
	}
	compact := strings.Join(strings.Fields(body), "")
	// 下标访问（非 .get）即隐式 KeyError。
	return containsAny(compact,
		"self.on_hand[sku]", "self.on_hand[key]", "self.on_hand[item]",
		"self.reserved[sku]"+"-", // 读取后参与运算，说明依赖其存在
	) && !strings.Contains(compact, "self.on_hand.get(sku,0)+qty")
}

// reservesAgainstAvailable 判断预留是否依据可用量（on_hand - reserved）
// 而非仅 on_hand。
//
// 允许把可用量先赋给中间变量再比较 —— free / remaining / avail 等命名都常见。
// 此前要求可用量表达式与比较紧邻，先算变量再比较的正确写法被判为未修复。
func reservesAgainstAvailable(body string) bool {
	if body == "" {
		return false
	}
	if !strings.Contains(body, "reserved") {
		return false
	}
	compact := strings.Join(strings.Fields(body), "")
	// 直接调用 available()。
	if containsAny(compact, "self.available(", "available(sku)") {
		return true
	}
	// 差值可能直接写出，也可能先把 on_hand 取到局部变量再与 reserved 相减。
	// 逐行扫描：任一行同时出现「减号」「reserved」与某个库存来源即认定。
	for _, line := range strings.Split(body, "\n") {
		flat := strings.Join(strings.Fields(line), "")
		if !strings.Contains(flat, "-") || !strings.Contains(flat, "reserved") {
			continue
		}
		if containsAny(flat, "on_hand", "stock", "onhand") {
			return true
		}
		// 形如 free = stock - self.reserved.get(...)：库存已在别处取出，
		// 此处只要求减去 reserved，且被减数不是字面量。
		if idx := strings.Index(flat, "-self.reserved"); idx > 0 {
			left := flat[:idx]
			if i := strings.LastIndex(left, "="); i >= 0 {
				left = left[i+1:]
			}
			if left != "" && !isNumericLiteral(left) {
				return true
			}
		}
	}
	return false
}

// trailOnlyOnSuccess 判断 trail 是否只在变更成功后写入。
//
// 契约要求「trail 只记录成功的变更」。判定标准是 append 之后不再有任何抛异常
// 的路径 —— 只要 append 后面还有 raise，失败时就已经污染了 trail。
//
// 原实现只检查「是否存在任一校验早于 append」，而原始缺陷代码恰好满足
// （if qty < 1 在 append 之前，库存校验在之后），因此这条缺陷从未被检出：
// 原始文件实测 3/7，与地板分相同，模型即使修好它也拿不到这一分。
func trailOnlyOnSuccess(body string) bool {
	appendAt := strings.Index(body, "self.trail.append")
	if appendAt < 0 {
		// 没有写 trail：不构成「失败也记录」的缺陷。
		return true
	}
	tail := body[appendAt:]
	// append 之后仍存在抛异常路径即为缺陷。
	return !containsAny(tail, "raise ", "raise\t")
}

// isNumericLiteral 判断字符串是否为纯数字字面量。
func isNumericLiteral(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// rejectsNonPositiveQty 判断 add 的下界守卫是否把 0 也挡住。
//
// 原始缺陷是 `if qty < 0`（放过了 0）。正确修法在语义上只有一种 —— 要求
// qty >= 1 —— 但写法很多：qty < 1、qty <= 0、not qty >= 1、1 > qty、
// 先判类型再判范围等。此前按字面枚举三种，其余正确写法一律被判为未修复
// （实测「not qty >= 1」「1 > qty」都会误判），bugfix 因此长期停在 6/7。
//
// 这里改为归一化后做语义匹配：去掉空格、消解 not、再看是否等价于
// 「qty 小于 1」或「qty 不大于 0」。
func rejectsNonPositiveQty(body string) bool {
	if body == "" {
		return false
	}
	// 归一化：去空格并统一大小写，便于比较。
	compact := strings.ToLower(strings.Join(strings.Fields(body), ""))

	// not qty>=1 / not(qty>=1) 等价于 qty<1；not qty>0 等价于 qty<=0。
	compact = strings.ReplaceAll(compact, "not(", "not")
	for _, pair := range [][2]string{
		{"notqty>=1", "qty<1"},
		{"notqty>0", "qty<=0"},
		{"notqty>=1)", "qty<1"},
	} {
		compact = strings.ReplaceAll(compact, pair[0], pair[1])
	}

	// 直接形式与反向书写（1>qty、0>=qty）都接受。
	return containsAny(compact,
		"qty<1", "qty<=0",
		"1>qty", "0>=qty",
		"qty<int(1)",
	)
}

// gradeDebug 校验 debug 任务：修复 stats.py 的 f-string 语法错误与
// 空列表除零。APK 证据（benchmark_cases.go，结束行 775，3360 字节）：
// 内嵌 531 字节的 stats.py 原文用于「与原始文件完全一致」判定，
// 其余检查项均为对提交源码的静态匹配。

// emptyListSkipsMax 判定空输入路径上不会执行 max(rows)。
//
// 认定通过的形态：
//   - 源码里根本没有 max(rows)（改用了别的求最大值方式）；
//   - 空列表在 max 之前就已提前返回（return 出现在 max 之前）；
//   - max 调用本身带条件（三元表达式或 max(rows) if rows else None）。
func emptyListSkipsMax(source string) bool {
	maxAt := strings.Index(source, "max(rows)")
	if maxAt < 0 {
		return true
	}
	guardAt := firstIndex(source, "if not rows", "if len(rows) == 0", "if count == 0", "len(rows) > 0")
	if guardAt >= 0 {
		// 守卫之后出现 return/raise，且位置在 max 之前，即空列表不会走到 max。
		exitAt := firstIndexFrom(source, guardAt, "return", "raise")
		if exitAt >= 0 && exitAt < maxAt {
			return true
		}
	}
	// 条件化的 max：max(rows) if rows else None / rows and max(rows)。
	tail := source[maxAt:]
	if head := strings.IndexAny(tail, "\r\n"); head >= 0 {
		tail = tail[:head]
	}
	if strings.Contains(tail, "if rows") || strings.Contains(tail, "if count") {
		return true
	}
	line := currentLine(source, maxAt)
	return strings.Contains(line, "rows and") || strings.Contains(line, "if rows else")
}

// releaseGuardsLowerBound 判定 release/available 是否防止 reserved 变负。
func releaseGuardsLowerBound(body string) bool {
	if body == "" {
		return false
	}
	// 只认真正夹住下界的写法。不能把「出现了 reserved 减法」当作守卫：
	// 原始缺陷代码正是 reserved.get(sku, 0) - qty，一旦纳入就会把缺陷判成
	// 已修复（实测会让有缺陷的样本从 2/7 虚高到 3/7）。
	return containsAny(body,
		"max(0", "max( 0", "min(qty", "min(amount", "min(count",
		"if result < 0", "if new < 0", "if value < 0", "if current < ",
		"if self.reserved", "reserved = 0", "reserved = max",
		"> reserved", ">= reserved", "clamp",
	)
}

// firstIndexFrom 从 offset 起查找任一标记的最早位置。
func firstIndexFrom(source string, offset int, markers ...string) int {
	if offset < 0 || offset > len(source) {
		return -1
	}
	best := -1
	for _, marker := range markers {
		if i := strings.Index(source[offset:], marker); i >= 0 {
			abs := offset + i
			if best < 0 || abs < best {
				best = abs
			}
		}
	}
	return best
}

// currentLine 返回 index 所在的整行。
func currentLine(source string, index int) string {
	if index < 0 || index >= len(source) {
		return ""
	}
	start := strings.LastIndexAny(source[:index], "\r\n") + 1
	end := strings.IndexAny(source[index:], "\r\n")
	if end < 0 {
		return source[start:]
	}
	return source[start : index+end]
}

func gradeDebug(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	source, ok := files["stats.py"]
	if !ok {
		report.total = 6
		report.failures = append(report.failures, "stats.py 不存在")
		return report.tally()
	}
	report.check(strings.TrimSpace(source) != strings.TrimSpace(debugOriginalStats), "与原始文件完全一致，未改写")
	report.check(!strings.Contains(source, "].2f}"), "仍含 .2f 非法写法")
	report.check(containsAny(source, ":.2f", "round(", "format("), "未见两位小数格式化")
	report.check(containsAny(source, "if not rows", "if len(rows) == 0", "if count == 0", "if rows else", "len(rows) > 0"), "未见空输入判断")
	// APK 的两条文案是「仍无条件 max(rows)」+「空列表时跳过 max」：要判定的
	// 是「空列表路径上不会执行 max」，而非源码里不得出现 max(rows)。
	// 先 return/短路再调用 max 是完全正确的写法，此前按字符串存在与否判定，
	// 把带守卫的正确解判成缺陷（实测正确解只得 5/6，扣的就是这一条）。
	report.check(emptyListSkipsMax(source), "仍无条件 max(rows)，空列表时跳过 max")
	report.check(strings.Contains(source, "def summarize") && strings.Contains(source, "def format_report"), "summarize 与 format_report 两个函数都还在")
	return report.tally()
}

// debugOriginalStats 是 debug 任务的初始 stats.py，逐字节取自 APK
// （0x51f000+722，531 字节）。gradeDebug 用它判定提交是否真的改写过。
const debugOriginalStats = `def summarize(rows):
    """Return {"count", "total", "mean", "max"} for a list of numbers.

    An empty list must return count 0, total 0, mean None, max None.
    """
    total = 0
    for value in rows:
        total += value
    count = len(rows)
    mean = total / count
    return {
        "count": count,
        "total": total,
        "mean": mean,
        "max": max(rows),
    }


def format_report(rows):
    stats = summarize(rows)
    return f"count={stats['count']} total={stats['total']} mean={stats['mean'].2f}"
`

// gradeRefactor 校验 refactor 任务：把 users.py 与 staff.py 的重复校验
// 逻辑抽到共享模块。APK 证据（结束行 900，4352 字节，含两个闭包）：
// 检查两个入口函数保留、新建了 .py 共享模块、两侧都改为引用它、
// 各自不再重复校验、年龄上限与排序规范化移入共享模块。
func gradeRefactor(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	users, hasUsers := files["users.py"]
	staff, hasStaff := files["staff.py"]
	if !hasUsers || !hasStaff {
		report.total = 8
		report.failures = append(report.failures, "users.py 或 staff.py 不存在")
		return report.tally()
	}
	report.check(strings.Contains(users, "def load_users"), "保留 load_users")
	report.check(strings.Contains(staff, "def load_staff"), "保留 load_staff")

	shared := ""
	sharedName := ""
	for name, content := range files {
		if name == "users.py" || name == "staff.py" || !strings.HasSuffix(name, ".py") {
			continue
		}
		shared, sharedName = content, strings.TrimSuffix(name, ".py")
		break
	}
	report.check(sharedName != "", "创建了共享模块；未找到新的 .py 模块")

	imports := func(source string) bool {
		if sharedName == "" {
			return false
		}
		return strings.Contains(source, "from "+sharedName) || strings.Contains(source, "import "+sharedName)
	}
	report.check(imports(users), "users.py 改为引用共享模块；未引用")
	report.check(imports(staff), "staff.py 改为引用共享模块；未引用")
	report.check(!strings.Contains(users, "> 150"), "users.py 不再重复校验")
	report.check(!strings.Contains(staff, "> 150"), "staff.py 不再重复校验")
	report.check(strings.Contains(shared, "150") && containsAny(shared, ".sort(", "sorted(") && containsAny(shared, ".title()", ".strip()"), "共享模块保留排序与规范化，年龄上限出现在共享模块")
	return report.tally()
}

// gradeIntervals 校验 algorithm 任务：从零实现 intervals.py 的 merge 与
// subtract，并在 notes.json 中说明复杂度。
//
// APK 证据（结束行 987，5200 字节）：该任务无初始产物 —— 全项目 rodata
// 中 "intervals.py" 只出现在字符串池内，不存在对应的内容字面量，
// 与首个检查项「intervals.py 存在」相符。
// 复杂度要求 merge 为 O(n log n)、subtract 为 O(n log n) 或 O(n+m)。
func gradeIntervals(files map[string]string) (int, int, []string) {
	report := &gradeReport{}
	source, ok := files["intervals.py"]
	if !ok {
		report.total = 8
		report.failures = append(report.failures, "intervals.py 不存在")
		return report.tally()
	}
	report.check(strings.Contains(source, "def merge"), "缺失 def merge")
	report.check(strings.Contains(source, "def subtract"), "缺失 def subtract")
	report.check(containsAny(source, ".sort(", "sorted("), "未见排序；应先排序再扫描")
	report.check(!containsAny(source, "for i in range(start, end)", "for x in range(s, e)", "set(range(", "|= set(range("), "逐点展开导致复杂度退化，应先排序再扫描")
	report.check(containsAny(source, "<=", ">=", "<", ">"), "未见闭合比较")
	report.check(containsAny(source, "if len(intervals) == 0", "if not lst", "return []"), "未见空输入分支；空输入返回 []")
	report.check(strings.Contains(source, "(start, end)") || strings.Contains(source, "tuple"), "未见 tuple 构造")

	notes, err := readJSONArtifact(files, "notes.json")
	if err != nil {
		report.check(false, "notes.json 可解析；%s", err.Error())
		return report.tally()
	}
	mergeComplexity := strings.TrimSpace(fmt.Sprint(firstPresent(notes, "mergeComplexity", "merge_complexity")))
	subtractComplexity := strings.TrimSpace(fmt.Sprint(firstPresent(notes, "subtractComplexity", "subtract_complexity")))
	approach := strings.TrimSpace(fmt.Sprint(firstPresent(notes, "approach")))
	report.check(strings.Contains(mergeComplexity, "O(n log n)"), "说明 merge 复杂度为 O(n log n)")
	report.check(containsAny(subtractComplexity, "O(n log n)", "O(n+m)", "O(n + m)"), "说明 subtract 复杂度为 O(n log n) 或 O(n+m)")
	report.check(approach != "" && approach != "<nil>", "给出做法说明")
	return report.tally()
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func firstIndex(value string, needles ...string) int {
	result := -1
	for _, needle := range needles {
		if index := strings.Index(value, needle); index >= 0 && (result < 0 || index < result) {
			result = index
		}
	}
	return result
}

// functionBody extracts a Python def body by indentation. It is deliberately
// lexical: the APK implementation also uses string searches rather than
// executing the submitted Python in the gateway process.
func functionBody(source, signature string) string {
	start := strings.Index(source, signature)
	if start < 0 {
		return ""
	}
	lineStart := strings.LastIndexByte(source[:start], '\n') + 1
	lineEndRel := strings.IndexByte(source[start:], '\n')
	if lineEndRel < 0 {
		return ""
	}
	defLine := source[lineStart : start+lineEndRel]
	defIndent := len(defLine) - len(strings.TrimLeft(defLine, " \t"))
	lineEnd := start + lineEndRel
	var body strings.Builder
	for _, line := range strings.Split(source[lineEnd+1:], "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			body.WriteByte('\n')
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent <= defIndent && !strings.HasPrefix(trimmed, "#") {
			break
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	return body.String()
}
