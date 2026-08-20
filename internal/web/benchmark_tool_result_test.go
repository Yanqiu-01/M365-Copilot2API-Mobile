package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// 工具结果回传给模型时必须完整，且成功读取不得被记为失败。
//
// 实测症状：bugfix 任务第 1 步 read_file 成功，此后模型连续 8 轮回复
// 「read_file 返回被截断的内容，无法完整读取 docstring 和实现」「结果被标记
// 为失败」，直到步数耗尽，最终只拿地板分。两个原因叠加：
//
//  1. 结果按 400 字节截断。inventory.py 有 1724 字节，模型只能看到开头与结尾，
//     无法逐条对照 CONTRACT 找出五处缺陷。
//  2. failureSignal 对结果全文做宽匹配，而源码里 class StockError(Exception)
//     的 "Exception"、以及 "error"/"failed"/"not found" 等词天然出现，
//     一次成功的读取因此被记为 failed。
func TestToolResultBudgetKeepsSourceIntact(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{
		"path": "inventory.py", "content": benchInventorySource,
	})
	if err != nil {
		t.Fatal(err)
	}

	budget := toolResultBudget("read_file", len(encoded))
	got := compactToolResult(string(encoded), budget)

	if strings.Contains(got, "truncated") {
		t.Errorf("read_file 结果被截断（%d 字节 → 预算 %d）", len(encoded), budget)
	}
	if !strings.Contains(got, "CONTRACT") {
		t.Error("回传内容缺少 CONTRACT，模型无法对照契约")
	}
	if !strings.Contains(got, "def release") {
		t.Error("回传内容缺少末尾的 release 实现")
	}
	if !strings.Contains(got, "def add") {
		t.Error("回传内容缺少 add 实现")
	}
}

// 变更类工具沿用紧凑摘要，避免上下文被写入内容淹没。
func TestToolResultBudgetStaysCompactForMutations(t *testing.T) {
	long := strings.Repeat("x", 5000)
	encoded, err := json.Marshal(map[string]any{"path": "a.py", "bytes": len(long)})
	if err != nil {
		t.Fatal(err)
	}
	if budget := toolResultBudget("write_file", len(encoded)); budget > 400 && len(encoded) > 400 {
		t.Errorf("write_file 预算=%d，应保持紧凑", budget)
	}
}

// 成功的读取结果不得被判为失败，即使源码里含 Exception / error 等词。
func TestSuccessfulReadNotMarkedFailed(t *testing.T) {
	encoded, err := json.Marshal(map[string]any{
		"path": "inventory.py", "content": benchInventorySource,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(benchInventorySource, "Exception") {
		t.Fatal("夹具前提失效：源码应含 Exception")
	}
	if toolResultLooksFailed("read_file", string(encoded)) {
		t.Error("含 Exception 的源码被判为失败结果")
	}

	// list_files 的正常输出同样不能判失败。
	listOut, _ := json.Marshal(map[string]any{"files": []string{"inventory.py"}})
	if toolResultLooksFailed("list_files", string(listOut)) {
		t.Error("list_files 正常输出被判为失败")
	}
}

// 真正的失败仍须识别。
func TestGenuineFailuresStillDetected(t *testing.T) {
	cases := map[string]struct {
		name   string
		result string
	}{
		"结构化 error 字段":  {"read_file", `{"error":"file not found"}`},
		"run_tests 未通过": {"run_tests", `{"passed":false,"runs":1,"output":"缺陷1 add 拒绝 qty=0"}`},
		"纯文本错误前缀":       {"read_file", "error: no such file"},
		"变更类工具报错":       {"write_file", "permission denied"},
	}
	for label, c := range cases {
		if !toolResultLooksFailed(c.name, c.result) {
			t.Errorf("[%s] 真实失败未被识别: %s", label, c.result)
		}
	}

	// run_tests 通过时不算失败。
	if toolResultLooksFailed("run_tests", `{"passed":true,"runs":2,"output":"TESTS PASSED: 7/7"}`) {
		t.Error("通过的测试被判为失败")
	}
}
