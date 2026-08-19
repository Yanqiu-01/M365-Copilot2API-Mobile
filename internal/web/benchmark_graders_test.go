package web

import (
	"strings"
	"testing"
)

// 原始 stats.py（audit/artifacts/stats.py，逐字节取自 APK）应被判为未改写。
func TestGradeDebugRejectsOriginalAndAcceptsFix(t *testing.T) {
	passed, total, failures := gradeDebug(map[string]string{"stats.py": debugOriginalStats})
	if total != 6 {
		t.Fatalf("total=%d want 6", total)
	}
	if passed != 1 {
		t.Fatalf("original stats.py should score 1/6, got %d/%d (%v)", passed, total, failures)
	}
	joined := strings.Join(failures, " | ")
	for _, want := range []string{"与原始文件完全一致", ".2f", "空输入", "max(rows)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing diagnostic %q in %s", want, joined)
		}
	}

	fixed := `def summarize(rows):
    if not rows:
        return {"count": 0, "total": 0, "mean": None, "max": None}
    total = 0
    for value in rows:
        total += value
    count = len(rows)
    return {"count": count, "total": total, "mean": total / count, "max": max(rows)}


def format_report(rows):
    stats = summarize(rows)
    if stats["mean"] is None:
        return f"count=0 total=0 mean=n/a"
    return f"count={stats['count']} total={stats['total']} mean={stats['mean']:.2f}"
`
	passed, total, failures = gradeDebug(map[string]string{"stats.py": fixed})
	if passed != total {
		t.Fatalf("fixed stats.py should be full marks, got %d/%d (%v)", passed, total, failures)
	}

	if passed, total, _ = gradeDebug(map[string]string{}); passed != 0 || total != 6 {
		t.Fatalf("missing file: got %d/%d want 0/6", passed, total)
	}
}

// 原始 users.py / staff.py 是两份重复实现，未重构时应大量扣分。
func TestGradeRefactorRequiresSharedModule(t *testing.T) {
	original := map[string]string{
		"users.py": `import json


def load_users(path):
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    out = []
    for item in raw:
        age = item.get("age")
        if not isinstance(age, int) or age < 0 or age > 150:
            continue
        out.append({"name": item["name"].strip().title(), "age": age})
    out.sort(key=lambda d: (d["age"], d["name"]))
    return out
`,
		"staff.py": `import json


def load_staff(path):
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    result = []
    for entry in raw:
        yrs = entry.get("age")
        if not isinstance(yrs, int) or yrs < 0 or yrs > 150:
            continue
        result.append({"name": entry["name"].strip().title(), "age": yrs})
    result.sort(key=lambda d: (d["age"], d["name"]))
    return result
`,
	}
	passed, total, failures := gradeRefactor(original)
	if total != 8 {
		t.Fatalf("total=%d want 8", total)
	}
	if passed != 2 {
		t.Fatalf("unrefactored should score 2/8, got %d/%d (%v)", passed, total, failures)
	}

	refactored := map[string]string{
		"common.py": `def normalize(raw):
    out = []
    for item in raw:
        age = item.get("age")
        if not isinstance(age, int) or age < 0 or age > 150:
            continue
        out.append({"name": item["name"].strip().title(), "age": age})
    out.sort(key=lambda d: (d["age"], d["name"]))
    return out
`,
		"users.py": `import json
from common import normalize


def load_users(path):
    with open(path, encoding="utf-8") as f:
        return normalize(json.load(f))
`,
		"staff.py": `import json
from common import normalize


def load_staff(path):
    with open(path, encoding="utf-8") as f:
        return normalize(json.load(f))
`,
	}
	passed, total, failures = gradeRefactor(refactored)
	if passed != total {
		t.Fatalf("refactored should be full marks, got %d/%d (%v)", passed, total, failures)
	}

	if passed, total, _ = gradeRefactor(map[string]string{"users.py": "x"}); passed != 0 || total != 8 {
		t.Fatalf("missing staff.py: got %d/%d want 0/8", passed, total)
	}
}

// algorithm 任务无初始产物，须从零创建 intervals.py 与 notes.json。
func TestGradeIntervalsFromScratch(t *testing.T) {
	if passed, total, _ := gradeIntervals(map[string]string{}); passed != 0 || total != 8 {
		t.Fatalf("empty submission: got %d/%d want 0/8", passed, total)
	}

	// 逐点展开（set(range(...))）必须被判复杂度退化。
	naive := `def merge(intervals):
    seen = set()
    for start, end in intervals:
        seen |= set(range(start, end))
    return sorted(seen)


def subtract(a, b):
    return sorted(set(a) - set(b))
`
	_, _, failures := gradeIntervals(map[string]string{
		"intervals.py": naive,
		"notes.json":   `{"mergeComplexity":"O(n)","subtractComplexity":"O(n)","approach":"brute force"}`,
	})
	joined := strings.Join(failures, " | ")
	if !strings.Contains(joined, "复杂度") {
		t.Errorf("naive point expansion must be flagged: %s", joined)
	}

	good := `def merge(intervals):
    if not intervals:
        return []
    ordered = sorted(intervals)
    out = [ordered[0]]
    for start, end in ordered[1:]:
        last_start, last_end = out[-1]
        if start <= last_end:
            out[-1] = (last_start, max(last_end, end))
        else:
            out.append((start, end))
    return out


def subtract(base, holes):
    if not base:
        return []
    result = []
    for start, end in merge(base):
        cursor = start
        for hs, he in merge(holes):
            if he <= cursor or hs >= end:
                continue
            if hs > cursor:
                result.append((cursor, hs))
            cursor = max(cursor, he)
        if cursor < end:
            result.append((cursor, end))
    return result
`
	passed, total, failures := gradeIntervals(map[string]string{
		"intervals.py": good,
		"notes.json":   `{"mergeComplexity":"O(n log n)","subtractComplexity":"O(n log n)","approach":"sort then sweep"}`,
	})
	if passed != total {
		t.Fatalf("correct solution should be full marks, got %d/%d (%v)", passed, total, failures)
	}

	// notes.json 缺失或不可解析时给出对应诊断。
	_, _, failures = gradeIntervals(map[string]string{"intervals.py": good})
	if !strings.Contains(strings.Join(failures, " "), "notes.json") {
		t.Errorf("missing notes.json must be reported: %v", failures)
	}
}
