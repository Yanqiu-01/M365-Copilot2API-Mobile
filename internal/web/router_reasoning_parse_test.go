package web

import (
	"encoding/json"
	"testing"
)

func routerTestTools() []map[string]any {
	mk := func(name string, props map[string]any, required []string) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       name,
				"parameters": map[string]any{"type": "object", "properties": props, "required": required},
			},
		}
	}
	return []map[string]any{
		mk("list_files", map[string]any{}, nil),
		mk("read_file", map[string]any{"path": map[string]any{"type": "string"}}, []string{"path"}),
		mk("write_file", map[string]any{
			"path":    map[string]any{"type": "string"},
			"content": map[string]any{"type": "string"},
		}, []string{"path", "content"}),
		mk("run_tests", map[string]any{}, nil),
	}
}

// 推理档位的模型先输出思考、最后一行才给指令。APK 的规则原文是
// "end with EXACTLY one line: CALL_TOOL: ..."，因此解析必须容忍指令
// 前面存在任意长度的思考内容。
//
// 此前解析要求 strings.HasPrefix(text, "CALL_TOOL:")，等于强制整段回复
// 以指令开头：纯文本 tone 直接给指令所以能过，推理 tone 一律解析失败，
// 随后进入修复轮、仍失败，最终报 router_error。
func TestParseToolDecisionAcceptsReasoningPreamble(t *testing.T) {
	tools := routerTestTools()
	reply := `让我先理清任务。工作区里有 inventory.py，需要先读出来对照契约。
我应该调用 read_file 而不是直接猜测内容。

CALL_TOOL: read_file({"path":"inventory.py"})`

	calls, parsed := parseModelToolDecision(reply, tools, "auto")
	if !parsed {
		t.Fatal("router output with a reasoning preamble must parse")
	}
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("calls=%+v", calls)
	}
	var args map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["path"] != "inventory.py" {
		t.Errorf("path=%v want inventory.py", args["path"])
	}
}

// 思考过程中可能复述 CALL_TOOL: 字样，必须采用最后一处指令。
func TestParseToolDecisionUsesLastDirective(t *testing.T) {
	tools := routerTestTools()
	reply := `一种做法是 CALL_TOOL: read_file({"path":"wrong.py"})，但那不是当前该做的事。
先看目录更稳妥。

CALL_TOOL: list_files({})`

	calls, parsed := parseModelToolDecision(reply, tools, "auto")
	if !parsed {
		t.Fatal("must parse")
	}
	if len(calls) != 1 || calls[0].Name != "list_files" {
		t.Fatalf("must use the final directive, got %+v", calls)
	}
}

// 带思考的 NO_TOOL_NEEDED 同样要识别。
func TestParseToolDecisionAcceptsNoToolAfterReasoning(t *testing.T) {
	tools := routerTestTools()
	reply := `测试已经通过，工作区文件都已写回，没有剩余动作。

NO_TOOL_NEEDED`

	calls, parsed := parseModelToolDecision(reply, tools, "auto")
	if !parsed {
		t.Fatal("NO_TOOL_NEEDED after reasoning must parse")
	}
	if len(calls) != 0 {
		t.Fatalf("expected no calls, got %+v", calls)
	}
}

// write_file 带多行内容时，指令行内的 JSON 必须完整解析。
func TestParseToolDecisionHandlesWriteFilePayload(t *testing.T) {
	tools := routerTestTools()
	content := "import json\n\n\ndef load(path):\n    return json.load(open(path))\n"
	encoded, err := json.Marshal(map[string]any{"path": "shared.py", "content": content})
	if err != nil {
		t.Fatal(err)
	}
	reply := "先抽取共享模块，再让两个入口引用它。\n\nCALL_TOOL: write_file(" + string(encoded) + ")"

	calls, parsed := parseModelToolDecision(reply, tools, "auto")
	if !parsed {
		t.Fatal("write_file directive must parse")
	}
	if len(calls) != 1 || calls[0].Name != "write_file" {
		t.Fatalf("calls=%+v", calls)
	}
	var args map[string]any
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatal(err)
	}
	if args["content"] != content {
		t.Errorf("content mangled:\n got=%q\nwant=%q", args["content"], content)
	}
}

// 路由规则必须保留 APK 的 "end with EXACTLY one line" 措辞，
// 否则推理模型会把指令放在开头之外的位置而不自知。
func TestRouterPromptKeepsAPKWording(t *testing.T) {
	prompt := modelToolRouterPrompt("请读取 inventory.py", routerTestTools(), "auto")
	for _, want := range []string{
		"end with EXACTLY one line: CALL_TOOL:",
		"If no tool is needed, end with: NO_TOOL_NEEDED",
		"Do not invent tools.",
	} {
		if !contains(prompt, want) {
			t.Errorf("router prompt missing APK wording %q", want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && stringsIndex(haystack, needle) >= 0
}

func stringsIndex(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// 思考中提及 NO_TOOL_NEEDED 但末尾给出真实指令时，必须执行工具调用。
func TestParseToolDecisionIgnoresMidTextNoToolMention(t *testing.T) {
	tools := routerTestTools()
	reply := `这里绝不能直接回 NO_TOOL_NEEDED，因为文件还没有读过，必须先取内容。

CALL_TOOL: read_file({"path":"users.py"})`

	calls, parsed := parseModelToolDecision(reply, tools, "auto")
	if !parsed {
		t.Fatal("must parse")
	}
	if len(calls) != 1 || calls[0].Name != "read_file" {
		t.Fatalf("mid-text NO_TOOL_NEEDED must not suppress the real call, got %+v", calls)
	}
}
