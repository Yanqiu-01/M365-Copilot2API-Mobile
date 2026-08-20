package web

import "testing"

// 复现评测无限空转：run_tests({}) 一旦完成过，就被永久剔除。
func TestVerificationToolMustBeRepeatable(t *testing.T) {
	msgs := []oaiMsg{
		{Role: "user", Content: "修复 inventory.py"},
		{Role: "assistant", ToolCalls: []map[string]any{{
			"id": "call_1", "type": "function",
			"function": map[string]any{"name": "run_tests", "arguments": "{}"},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: "TESTS FAILED: 3/7"},
		{Role: "assistant", ToolCalls: []map[string]any{{
			"id": "call_2", "type": "function",
			"function": map[string]any{"name": "write_file", "arguments": `{"path":"inventory.py","content":"fixed"}`},
		}}},
		{Role: "tool", ToolCallID: "call_2", Content: `{"path":"inventory.py","bytes":5}`},
	}
	ledger := buildAgentLedger(msgs)

	// 改完代码后，模型必须能再跑一次 run_tests 验证。
	again := []detectedToolCall{{Name: "run_tests", Arguments: []byte("{}")}}
	kept := filterCompletedCalls(again, ledger)
	if len(kept) == 0 {
		t.Error("BUG: run_tests 被当作已完成而剔除，模型无法验证修复 —— 网关随后判定「未调用工具」，" +
			"追加闭环提示再推一轮，模型再次请求 run_tests 又被剔除，直到 14 步耗尽")
	}

	// 写文件类工具重复同样内容才应剔除（避免真正的无效重复）。
	sameWrite := []detectedToolCall{{Name: "write_file", Arguments: []byte(`{"path":"inventory.py","content":"fixed"}`)}}
	if len(filterCompletedCalls(sameWrite, ledger)) != 0 {
		t.Error("完全相同的写入应被剔除")
	}

	// 内容不同的写入必须放行。
	newWrite := []detectedToolCall{{Name: "write_file", Arguments: []byte(`{"path":"inventory.py","content":"fixed again"}`)}}
	if len(filterCompletedCalls(newWrite, ledger)) == 0 {
		t.Error("内容不同的写入不应被剔除")
	}
}

// 强制推进必须有次数上限，否则 14 步会全部空转在同一句提示上。
func TestForcedClosureHasCap(t *testing.T) {
	if maxForcedClosures <= 0 || maxForcedClosures >= benchMaxSteps {
		t.Fatalf("maxForcedClosures=%d 必须为正且小于步数上限 %d", maxForcedClosures, benchMaxSteps)
	}
}

// 只读工具在状态未改变时仍应剔除，避免无意义的重复观察。
func TestObservationalToolStillFilteredWithoutMutation(t *testing.T) {
	msgs := []oaiMsg{
		{Role: "assistant", ToolCalls: []map[string]any{{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "read_file", "arguments": `{"path":"a.py"}`},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "contents"},
	}
	ledger := buildAgentLedger(msgs)
	same := []detectedToolCall{{Name: "read_file", Arguments: []byte(`{"path":"a.py"}`)}}
	if len(filterCompletedCalls(same, ledger)) != 0 {
		t.Error("状态未改变时重复读取同一文件应被剔除")
	}
}

// EVIDENCE_LEDGER 的提示语必须逐字符合 APK 原文。
//
// 原版明确允许 read/inspect/check/test 在状态变化后重复，只禁止参数完全相同
// 的变更类调用。恢复期误写成「任何已完成的调用都不得重复」，模型于是在需要
// 再写一次文件或再跑一次 run_tests 时回 NO_TOOL_NEEDED —— 而网关同时又在推
// 它「必须调用 run_tests」，两条指令自相矛盾，评测因此空转到步数耗尽。
func TestRouterContextHintMatchesAPK(t *testing.T) {
	ledger := buildAgentLedger([]oaiMsg{
		{Role: "assistant", ToolCalls: []map[string]any{{
			"id": "c1", "type": "function",
			"function": map[string]any{"name": "run_tests", "arguments": "{}"},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: "TESTS FAILED"},
	})
	hint := ledger.RouterContext()

	const want = "Use only this compact evidence. Do not repeat a completed mutating call with identical arguments. Read, inspect, check, and test calls may be repeated after workspace state changes."
	if !contains(hint, want) {
		t.Errorf("router context hint 与 APK 原文不符:\n%s", hint)
	}
	// 自撰的绝对禁令不得再出现。
	for _, forbidden := range []string{
		"A completed call is final evidence",
		"do not issue the same name and arguments again",
	} {
		if contains(hint, forbidden) {
			t.Errorf("hint 仍含自撰禁令 %q，会阻止模型继续调用工具", forbidden)
		}
	}
}
