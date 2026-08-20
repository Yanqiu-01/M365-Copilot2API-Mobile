package web

import "testing"

// 真实推理模型的路由输出样式集合。
func TestParseRealisticReasoningOutputs(t *testing.T) {
	tools := routerTestTools()
	cases := []struct {
		name string
		text string
		want string // 期望选中的工具名；"" 表示 NO_TOOL
	}{
		{"markdown 包裹指令", "我需要先看文件。\n\n```\nCALL_TOOL: read_file({\"path\":\"inventory.py\"})\n```", "read_file"},
		{"指令后有解释", "CALL_TOOL: list_files({})\n\n这样可以先确认目录结构。", "list_files"},
		{"粗体标记", "分析完成。\n\n**CALL_TOOL: run_tests({})**", "run_tests"},
		{"行首有序号", "步骤：\n1. 读文件\n\n2. CALL_TOOL: read_file({\"path\":\"stats.py\"})", "read_file"},
		{"JSON 格式（修复轮要求的形状）", "{\"calls\":[{\"name\":\"read_file\",\"arguments\":{\"path\":\"a.py\"}}]}", "read_file"},
		{"JSON 带围栏", "```json\n{\"calls\":[{\"name\":\"run_tests\",\"arguments\":{}}]}\n```", "run_tests"},
		{"JSON 空 calls", "{\"calls\":[]}", ""},
		{"中文冒号", "先读文件。\n\nCALL_TOOL：read_file({\"path\":\"a.py\"})", "read_file"},
		{"参数含中文与换行", "CALL_TOOL: write_file({\"path\":\"notes.json\",\"content\":\"{\\\"a\\\":1}\"})", "write_file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls, parsed := parseModelToolDecision(c.text, tools, "auto")
			if !parsed {
				t.Fatalf("NOT PARSED: %q", c.text)
			}
			if c.want == "" {
				if len(calls) != 0 {
					t.Fatalf("expected no calls, got %+v", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0].Name != c.want {
				t.Fatalf("got %+v want %s", calls, c.want)
			}
		})
	}
}

// 推理模型在思考里提到 JSON 片段，随后才给出真正的决策。
// 取第一个 { 会解析到思考里的片段，导致 parsed=false → 修复轮 → 仍失败
// → HTTP 502 model returned an invalid tool routing decision。
func TestParseJSONDecisionWithReasoningBraces(t *testing.T) {
	tools := routerTestTools()
	cases := []string{
		`我需要返回形如 {"calls":[...]} 的结构。先确认要读哪个文件。
最终决定：
{"calls":[{"name":"read_file","arguments":{"path":"inventory.py"}}]}`,
		`思考：参数应该是 {"path": "..."} 这种形状。
{"calls":[{"name":"read_file","arguments":{"path":"inventory.py"}}]}`,
		`分析后确认无需工具，格式应为 {"calls":[]}。
{"calls":[]}`,
	}
	for i, text := range cases {
		calls, parsed := parseModelToolDecision(text, tools, "auto")
		if !parsed {
			t.Errorf("case %d NOT PARSED:\n%s", i, text)
			continue
		}
		t.Logf("case %d -> %d calls", i, len(calls))
	}
}
