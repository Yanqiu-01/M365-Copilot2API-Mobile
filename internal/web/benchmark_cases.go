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
