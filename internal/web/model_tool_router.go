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

func parseModelToolDecision(text string, tools []map[string]any, choice any) ([]detectedToolCall, bool) {
	text = strings.TrimSpace(text)
	// CALL_TOOL: 指令按 APK 规则出现在回复末尾，前面可以有任意长度的
	// 思考过程。因此从后往前找最后一处 CALL_TOOL:，而不是要求它位于
	// 整段文本开头 —— 后者会让所有推理档位的路由输出解析失败。
	if idx := lastToolDirectiveIndex(text); idx >= 0 {
		rest := strings.TrimSpace(text[idx:])
		if colon := strings.Index(rest, ":"); colon > 0 {
			rest = strings.TrimSpace(rest[colon+1:])
			// 指令占一行；截到行尾避免把后续解释文字带进参数。
			if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
				rest = strings.TrimSpace(rest[:nl])
			}
			start := strings.Index(rest, "(")
			end := strings.LastIndex(rest, ")")
			if start > 0 && end > start {
				name := strings.TrimSpace(rest[:start])
				argsStr := rest[start+1 : end]
				var args map[string]any
				if json.Unmarshal([]byte(argsStr), &args) == nil && toolChoiceAllows(choice, name) {
					fn := toolFunction(name, tools)
					if fn != nil && schemaValid(args, fn) == nil {
						b, _ := json.Marshal(args)
						return []detectedToolCall{{ID: callID(name, string(b), 0), Type: toolType(name, tools), Name: name, Arguments: b}}, true
					}
				}
			}
		}
	}
	// NO_TOOL_NEEDED 同样按 APK 规则出现在末尾。只在尾部窗口内认定，
	// 避免思考过程中提到该标记（例如「这里不该回 NO_TOOL_NEEDED」）
	// 被误判成「无需调用工具」而丢掉真正的工具调用。
	if hasTrailingNoToolMarker(text) {
		return nil, true
	}
	// Fallback: try the old JSON format
	if i := strings.Index(text, "```"); i >= 0 {
		text = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(text[i+3:], "```"), "json"))
	}
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, false
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal([]byte(text[start:end+1]), &probe) != nil {
		return nil, false
	}
	if _, ok := probe["calls"]; !ok {
		return nil, false
	}
	var envelope struct {
		Calls []struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"calls"`
	}
	if json.Unmarshal([]byte(text[start:end+1]), &envelope) != nil {
		return nil, false
	}
	out := make([]detectedToolCall, 0, len(envelope.Calls))
	for i, c := range envelope.Calls {
		fn := toolFunction(c.Name, tools)
		if fn == nil || c.Arguments == nil || !toolChoiceAllows(choice, c.Name) || schemaValid(c.Arguments, fn) != nil {
			continue
		}
		b, _ := json.Marshal(c.Arguments)
		out = append(out, detectedToolCall{ID: callID(c.Name, string(b), i), Type: toolType(c.Name, tools), Name: c.Name, Arguments: b})
	}
	return out, true
}
