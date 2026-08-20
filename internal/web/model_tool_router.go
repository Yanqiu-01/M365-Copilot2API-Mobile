package web

import (
	"encoding/json"
	"fmt"
	"strings"
)

func modelToolRouterPrompt(prompt string, tools []map[string]any, choice any) string {
	defs, _ := json.Marshal(tools)
	mode := normalizedToolChoiceMode(choice)
	// 规则文案逐字取自原 APK rodata（0x51e… 段，以 "- Decide the next
	// concrete action" 起、"Do not invent tools." 止）。关键是
	// "end with EXACTLY one line" —— 推理档位的模型会先输出思考过程，
	// 指令只出现在末尾。此前恢复成 "respond with:"，配合解析侧的
	// HasPrefix 检查，等于要求整段回复必须以 CALL_TOOL: 开头，推理
	// tone 必然解析失败（纯文本 tone 直接给指令所以能过）。
	rules := `- Decide the next concrete action. Prefer the most specific tool (a document/skill tool beats a raw shell).
- If a tool is needed, end with EXACTLY one line: CALL_TOOL: tool_name({"arg1":"value1"})
- If no tool is needed, end with: NO_TOOL_NEEDED
- When a tool writes a file, its text argument must contain the finished deliverable itself. Never pass the instructions, the brief, or a description of what to write; the user reads the file, not the request.
- Never build a document by shelling out. Do not use cat/tee heredocs or echo redirects to create content; call the file-writing tool or the dedicated skill instead.
- workspace_shell is for short operational commands (ls, tests, git, package installs), not for authoring.
- Only use tools from the available list. Validate arguments against the schema. Do not invent tools.`
	// Multi-turn: completed tool evidence (tool[...], tool_calls:) was already
	// acted upon, so re-invoking those tools would duplicate work.
	if strings.Contains(prompt, "tool_calls:") || strings.Contains(prompt, "tool[call_") {
		rules += `
- Completed evidence must not be repeated: tool_calls/tool[call_x] rows are prior results already delivered to the user, never re-invoke them
- Only start a new tool call when fresh unfinished work remains on the current request`
	}
	return fmt.Sprintf(`You are a tool selection assistant. Based on the user request, decide which tool to call next.

Available tools: %s

MODE: %s

Rules:
%s

User request and evidence:
%s`, defs, mode, rules, prompt)
}

// normalizeToolDirective 把全角冒号统一成半角。模型用中文作答时常输出
// "CALL_TOOL：name(...)"，此前一律解析失败并落到修复轮。全角冒号占 3 字节、
// 半角占 1 字节，长度会变，因此规范化后的文本要整体用于后续下标运算。
func normalizeToolDirective(text string) string {
	return strings.ReplaceAll(text, "：", ":")
}

// lastToolDirectiveIndex 返回最后一个 CALL_TOOL: 指令的起始下标（大小写
// 不敏感），没有则返回 -1。推理模型的指令位于思考之后，必须取最后一处：
// 思考过程里可能复述过 "CALL_TOOL:" 字样，取第一处会解析到错误的参数。
func lastToolDirectiveIndex(text string) int {
	lower := strings.ToLower(text)
	return strings.LastIndex(lower, "call_tool:")
}

// hasTrailingNoToolMarker 判断 NO_TOOL_NEEDED 是否作为收尾指令出现。
// 取末尾若干字符做窗口：指令按规则独占最后一行，而思考正文里的提及
// 通常位于更靠前的位置。
func hasTrailingNoToolMarker(text string) bool {
	trimmed := strings.TrimRight(strings.TrimSpace(text), "。.!！`\"'\u201d\u3002")
	const window = 64
	tail := trimmed
	if len(tail) > window {
		tail = tail[len(tail)-window:]
	}
	return strings.Contains(strings.ToUpper(tail), "NO_TOOL_NEEDED")
}

// parseModelToolDecision 抽取模型的路由决策。
//
// 实现委托给 tool_decision_extract.go 的统一抽取器：先枚举全部候选（不问
// 它被什么包裹），再统一校验并取最后一个有效的。这样新增一种输出包装方式
// 时无需在此新增分支 —— 此前按模型逐个打补丁（gpt-5.6 的 CALL_TOOL 前缀、
// gpt-5.5 的思考期花括号、中文全角冒号……）每换一个模型就要返工一次。
//
// 返回值 parsed 表示「模型给出了可理解的决策」，与是否选中工具无关：
// NO_TOOL_NEEDED 和空 calls 都是 parsed=true、calls 为空。
func parseModelToolDecision(text string, tools []map[string]any, choice any) ([]detectedToolCall, bool) {
	text = normalizeDecisionText(strings.TrimSpace(text))
	if text == "" {
		return nil, false
	}

	// 1) CALL_TOOL: name(<json>) —— 路由轮的主格式。
	if calls, ok := selectDecision(extractDirectiveCandidates(text), tools, choice); ok {
		return calls, true
	}

	// 2) {"calls":[...]} —— 修复轮与 required 重试轮要求的格式。
	//    信封本身即为一次完整决策，因此保留其中全部合法调用。
	if candidates, found := extractEnvelopeCandidates(text); found {
		return selectAllValid(candidates, tools, choice), true
	}

	// 3) 明确表示无需工具。放在最后：只有在没抽到任何可用调用时才采纳，
	//    避免「先说不需要、又给出指令」的回复被判成不调用。
	if hasTrailingNoTool(text) {
		return nil, true
	}
	return nil, false
}
