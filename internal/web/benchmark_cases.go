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
	r.total++
	if fmt.Sprint(got) == fmt.Sprint(want) {
		r.passed++
		return
	}
	r.failures = append(r.failures, fmt.Sprintf("%s: got %s, want %s", label, trimForLog(fmt.Sprint(got)), trimForLog(fmt.Sprint(want))))
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
	if containsAny(addBody, "qty < 1", "qty <= 0", "not isinstance(qty, int) or qty < 1") {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷1 add 拒绝 qty=0")
	}

	// Defect 2: failed reserve must not append to trail. If an append remains,
	// all validation must precede it.
	report.total++
	appendAt := strings.Index(reserveBody, "self.trail.append")
	validationAt := firstIndex(reserveBody, "if qty", "if available", "if self.available", "raise StockError", "raise KeyError")
	if appendAt < 0 || (validationAt >= 0 && validationAt < appendAt) {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷2 失败操作不写入 trail")
	}

	// Defect 3: unknown SKUs must raise KeyError.
	report.total++
	if strings.Contains(reserveBody, "KeyError") {
		report.passed++
	} else {
		report.failures = append(report.failures, "未见 KeyError")
	}

	// Defect 4: reservation must compare against available stock, not merely
	// on_hand. The exact available expression is present in the APK strings.
	report.total++
	if containsAny(reserveBody, "available(", "available =", "self.available", "on_hand.get(sku, 0) - self.reserved.get(sku, 0)", "self.on_hand.get(sku, 0) - self.reserved.get(sku, 0)") && strings.Contains(reserveBody, "reserved") {
		report.passed++
	} else {
		report.failures = append(report.failures, "预留未依据可用量")
	}

	// Defect 5: release must clamp or otherwise guard the lower bound.
	report.total++
	if containsAny(releaseBody, "max(0", "if result < 0", "if self.reserved", "reserved = 0", "reserved = max") || containsAny(availableBody, "max(0", "if result < 0") {
		report.passed++
	} else {
		report.failures = append(report.failures, "缺陷5 release 后 reserved 可能为负")
	}

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
