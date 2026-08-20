package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func extractTestTools() []map[string]any {
	// required 用 []any：真实请求的工具定义经 json.Unmarshal 后就是这个类型，
	// schemaValid 也只识别 []any。用 []string 构造会让必填校验被静默跳过，
	// 测试就失去了意义。
	mk := func(name string, props map[string]any, required ...string) map[string]any {
		req := make([]any, 0, len(required))
		for _, r := range required {
			req = append(req, r)
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       name,
				"parameters": map[string]any{"type": "object", "properties": props, "required": req},
			},
		}
	}
	return []map[string]any{
		mk("list_files", map[string]any{}),
		mk("run_tests", map[string]any{}),
		mk("read_file", map[string]any{"path": map[string]any{"type": "string"}}, "path"),
		mk("write_file", map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}, "path", "content"),
	}
}

// 格式矩阵：同一个决策（read_file{"path":"a.py"}）用各种包装方式表达。
//
// 这些包装是模型自由行为，无法穷举也无法约束。抽取器必须对包装免疫，
// 而不是逐个加分支 —— 此前按模型打补丁（gpt-5.6 的前缀检查、gpt-5.5 的
// 思考期花括号、中文全角冒号），每换一个模型就要返工一次。
func TestExtractDecisionAcrossWrappings(t *testing.T) {
	tools := extractTestTools()
	want := `{"path":"a.py"}`

	wrappings := map[string]string{
		"裸指令":         `CALL_TOOL: read_file({"path":"a.py"})`,
		"思考在前":        "我得先看文件内容。\n\nCALL_TOOL: read_file({\"path\":\"a.py\"})",
		"解释在后":        "CALL_TOOL: read_file({\"path\":\"a.py\"})\n\n这样能确认契约。",
		"前后都有":        "先读文件。\nCALL_TOOL: read_file({\"path\":\"a.py\"})\n读完再判断。",
		"markdown 围栏": "分析完成：\n```\nCALL_TOOL: read_file({\"path\":\"a.py\"})\n```",
		"json 围栏":     "```json\nCALL_TOOL: read_file({\"path\":\"a.py\"})\n```",
		"粗体":          "**CALL_TOOL: read_file({\"path\":\"a.py\"})**",
		"下划粗体":        "__CALL_TOOL: read_file({\"path\":\"a.py\"})__",
		"行首短横":        "- CALL_TOOL: read_file({\"path\":\"a.py\"})",
		"行首序号点":       "1. CALL_TOOL: read_file({\"path\":\"a.py\"})",
		"行首序号括号":      "2) CALL_TOOL: read_file({\"path\":\"a.py\"})",
		"全角冒号":        `CALL_TOOL：read_file({"path":"a.py"})`,
		"全角括号":        `CALL_TOOL: read_file（{"path":"a.py"}）`,
		"全角引号":        "CALL_TOOL: read_file({“path”:“a.py”})",
		"小写标记":        `call_tool: read_file({"path":"a.py"})`,
		"混合大小写":       `Call_Tool: read_file({"path":"a.py"})`,
		"参数跨行":        "CALL_TOOL: read_file({\n  \"path\": \"a.py\"\n})",
		"名字与括号间空格":    `CALL_TOOL: read_file ({"path":"a.py"})`,
		"冒号后多空格":      `CALL_TOOL:    read_file({"path":"a.py"})`,
		"CRLF 换行":     "先看文件。\r\nCALL_TOOL: read_file({\"path\":\"a.py\"})\r\n",
		"信封形式":        `{"calls":[{"name":"read_file","arguments":{"path":"a.py"}}]}`,
		"信封带围栏":       "```json\n{\"calls\":[{\"name\":\"read_file\",\"arguments\":{\"path\":\"a.py\"}}]}\n```",
		"信封前有思考":      "需要返回 {\"calls\":[...]} 结构。\n{\"calls\":[{\"name\":\"read_file\",\"arguments\":{\"path\":\"a.py\"}}]}",
		"信封前提到参数形状":   "参数应该是 {\"path\": \"...\"} 这种。\n{\"calls\":[{\"name\":\"read_file\",\"arguments\":{\"path\":\"a.py\"}}]}",
		"思考里复述过指令":    "一种想法是 CALL_TOOL: write_file({\"path\":\"x\",\"content\":\"y\"})，但不对。\n\nCALL_TOOL: read_file({\"path\":\"a.py\"})",
		"先说不需要再给指令":   "起初以为 NO_TOOL_NEEDED，但还得读文件。\n\nCALL_TOOL: read_file({\"path\":\"a.py\"})",
		"指令重复两次取最后":   "CALL_TOOL: list_files({})\n重新考虑后：\nCALL_TOOL: read_file({\"path\":\"a.py\"})",
	}

	for name, text := range wrappings {
		t.Run(name, func(t *testing.T) {
			calls, parsed := parseModelToolDecision(text, tools, "auto")
			if !parsed {
				t.Fatalf("not parsed:\n%s", text)
			}
			if len(calls) != 1 {
				t.Fatalf("got %d calls, want 1: %+v", len(calls), calls)
			}
			if calls[0].Name != "read_file" {
				t.Fatalf("name=%s want read_file", calls[0].Name)
			}
			var got map[string]any
			if err := json.Unmarshal(calls[0].Arguments, &got); err != nil {
				t.Fatal(err)
			}
			if got["path"] != "a.py" {
				t.Fatalf("arguments=%s want %s", calls[0].Arguments, want)
			}
		})
	}
}

// 无需工具的各种表达。
func TestExtractNoToolDecisions(t *testing.T) {
	tools := extractTestTools()
	for name, text := range map[string]string{
		"裸标记":    "NO_TOOL_NEEDED",
		"思考在前":   "测试已通过，文件都写好了。\n\nNO_TOOL_NEEDED",
		"带句号":    "工作已完成。\nNO_TOOL_NEEDED.",
		"粗体":     "**NO_TOOL_NEEDED**",
		"小写":     "no_tool_needed",
		"空信封":    `{"calls":[]}`,
		"空信封带思考": "确认无需调用工具，应返回 {\"calls\":[]}。\n{\"calls\":[]}",
	} {
		t.Run(name, func(t *testing.T) {
			calls, parsed := parseModelToolDecision(text, tools, "auto")
			if !parsed {
				t.Fatalf("not parsed: %q", text)
			}
			if len(calls) != 0 {
				t.Fatalf("expected no calls, got %+v", calls)
			}
		})
	}
}

// 真正无法理解的输出必须报告 parsed=false，交给修复轮。
func TestExtractRejectsUnusableOutput(t *testing.T) {
	tools := extractTestTools()
	for name, text := range map[string]string{
		"纯文字":         "我觉得应该先看看文件里有什么。",
		"空":           "",
		"未知工具":        `CALL_TOOL: delete_everything({})`,
		"参数不合 schema": `CALL_TOOL: read_file({"wrong":"a.py"})`,
		"参数非 JSON":    `CALL_TOOL: read_file(path=a.py)`,
		"括号未闭合":       `CALL_TOOL: read_file({"path":"a.py"`,
		"只提到标记":       "我不会输出 CALL_TOOL 这种东西的。",
		"信封无 calls":   `{"result":"done"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, parsed := parseModelToolDecision(text, tools, "auto"); parsed {
				t.Fatalf("must not parse: %q", text)
			}
		})
	}
}

// 字符串字面量里的括号与转义不得干扰边界识别。
func TestExtractHandlesBracesInsideStrings(t *testing.T) {
	tools := extractTestTools()
	content := "def f():\n    return {\"a\": 1}  # }) 干扰\n"
	encoded, err := json.Marshal(map[string]any{"path": "m.py", "content": content})
	if err != nil {
		t.Fatal(err)
	}
	text := "写入文件。\n\nCALL_TOOL: write_file(" + string(encoded) + ")"

	calls, parsed := parseModelToolDecision(text, tools, "auto")
	if !parsed || len(calls) != 1 {
		t.Fatalf("parsed=%v calls=%+v", parsed, calls)
	}
	var got map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &got); err != nil {
		t.Fatal(err)
	}
	if got["content"] != content {
		t.Errorf("content mangled:\n got=%q\nwant=%q", got["content"], content)
	}
}

// 信封可以携带多个调用，顺序保持不变。
func TestExtractEnvelopeKeepsMultipleCalls(t *testing.T) {
	tools := extractTestTools()
	text := `{"calls":[
		{"name":"read_file","arguments":{"path":"a.py"}},
		{"name":"list_files","arguments":{}}
	]}`
	calls, parsed := parseModelToolDecision(text, tools, "auto")
	if !parsed {
		t.Fatal("not parsed")
	}
	if len(calls) != 2 || calls[0].Name != "read_file" || calls[1].Name != "list_files" {
		t.Fatalf("calls=%+v", calls)
	}
}

// tool_choice 约束必须生效。
func TestExtractHonoursToolChoice(t *testing.T) {
	tools := extractTestTools()
	text := `CALL_TOOL: read_file({"path":"a.py"})`

	if _, parsed := parseModelToolDecision(text, tools, map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "run_tests"},
	}); parsed {
		t.Error("a decision naming a disallowed tool must not be accepted")
	}

	calls, parsed := parseModelToolDecision(text, tools, map[string]any{
		"type":     "function",
		"function": map[string]any{"name": "read_file"},
	})
	if !parsed || len(calls) != 1 {
		t.Fatalf("allowed tool must parse: parsed=%v calls=%+v", parsed, calls)
	}
}

// 不带参数的工具应被接受（参数为空对象）。
func TestExtractAcceptsEmptyArguments(t *testing.T) {
	tools := extractTestTools()
	for _, text := range []string{
		`CALL_TOOL: run_tests({})`,
		`CALL_TOOL: run_tests()`,
		"CALL_TOOL: run_tests(  )",
	} {
		calls, parsed := parseModelToolDecision(text, tools, "auto")
		if !parsed || len(calls) != 1 || calls[0].Name != "run_tests" {
			t.Fatalf("text=%q parsed=%v calls=%+v", text, parsed, calls)
		}
	}
}

// 抽取器对包装组合免疫：随机叠加多层装饰仍应识别。
func TestExtractSurvivesLayeredDecoration(t *testing.T) {
	tools := extractTestTools()
	core := `CALL_TOOL: read_file({"path":"a.py"})`
	layers := []func(string) string{
		func(s string) string { return "**" + s + "**" },
		func(s string) string { return "- " + s },
		func(s string) string { return "```\n" + s + "\n```" },
		func(s string) string { return "思考中……\n\n" + s },
		func(s string) string { return s + "\n\n（以上为决策）" },
	}
	for i, outer := range layers {
		for j, inner := range layers {
			text := outer(inner(core))
			t.Run(fmt.Sprintf("%d-%d", i, j), func(t *testing.T) {
				calls, parsed := parseModelToolDecision(text, tools, "auto")
				if !parsed || len(calls) != 1 || calls[0].Name != "read_file" {
					t.Fatalf("layered wrapping broke extraction:\n%s\nparsed=%v calls=%+v", text, parsed, calls)
				}
			})
		}
	}
}

// 抽取与校验必须分离：候选枚举本身不应过滤未知工具，
// 否则新增工具时又要回到抽取器里改判断。
func TestExtractionIsSeparateFromValidation(t *testing.T) {
	candidates := extractDirectiveCandidates(normalizeDecisionText(
		"CALL_TOOL: unknown_tool({\"x\":1})\nCALL_TOOL: read_file({\"path\":\"a.py\"})"))
	if len(candidates) != 2 {
		t.Fatalf("extractor must return all candidates regardless of validity, got %d", len(candidates))
	}
	if candidates[0].Name != "unknown_tool" || candidates[1].Name != "read_file" {
		t.Fatalf("candidates=%+v", candidates)
	}
	// 校验阶段才淘汰未知工具，并取最后一个有效的。
	calls, ok := selectDecision(candidates, extractTestTools(), "auto")
	if !ok || len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("validation stage should pick the last valid call: ok=%v calls=%+v", ok, calls)
	}
}

func TestStripDecorationsIdempotent(t *testing.T) {
	for _, in := range []string{
		"**read_file**", "- read_file", "1. read_file", "`read_file`", "  read_file  ",
	} {
		once := stripDecorations(in)
		twice := stripDecorations(once)
		if once != twice {
			t.Errorf("stripDecorations not idempotent for %q: %q vs %q", in, once, twice)
		}
		if !strings.Contains(once, "read_file") {
			t.Errorf("stripDecorations lost content for %q: %q", in, once)
		}
	}
}
