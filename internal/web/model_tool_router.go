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

func parseModelToolDecision(text string, tools []map[string]any, choice any) ([]detectedToolCall, bool) {
	text = normalizeToolDirective(strings.TrimSpace(text))
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
	// Fallback: JSON 形状 {"calls":[...]}（修复轮与 required 重试轮要求的格式）。
	//
	// 不能只取第一个 { 到最后一个 }：推理档位的模型会在思考里提到 JSON
	// 片段（例如「需要返回形如 {"calls":[...]} 的结构」、「参数应该是
	// {"path": "..."} 这种形状」），首个 { 落在思考中，整段截取因此既不是
	// 合法 JSON、也不含 calls 键，一律 parsed=false —— 进修复轮，修复轮
	// 同样带思考、同样失败，最终报 502 model returned an invalid tool
	// routing decision。gpt-5.5 在评测中每次必报正是这条路径。
	//
	// 改为扫描全部候选对象，从最后一个 { 往前找第一个能解析且含 calls
	// 的片段：真正的决策位于回复末尾，思考中的示例在前面。
	for _, candidate := range jsonObjectCandidates(text) {
		var probe map[string]json.RawMessage
		if json.Unmarshal([]byte(candidate), &probe) != nil {
			continue
		}
		if _, ok := probe["calls"]; !ok {
			continue
		}
		var envelope struct {
			Calls []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"calls"`
		}
		if json.Unmarshal([]byte(candidate), &envelope) != nil {
			continue
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
	return nil, false
}

// jsonObjectCandidates 返回文本中所有花括号平衡的顶层对象片段，按结束位置
// 从后往前排列。字符串字面量内的花括号与转义不参与配对。
func jsonObjectCandidates(text string) []string {
	// 去掉代码围栏标记，保留内容本身。
	text = strings.ReplaceAll(text, "```json", "```")
	cleaned := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		if strings.HasPrefix(text[i:], "```") {
			i += 2
			continue
		}
		cleaned = append(cleaned, text[i])
	}
	body := string(cleaned)

	var out []string
	var starts []int
	inString := false
	escaped := false
	for i := 0; i < len(body); i++ {
		ch := body[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			starts = append(starts, i)
		case '}':
			if n := len(starts); n > 0 {
				start := starts[n-1]
				starts = starts[:n-1]
				// 只收集顶层对象，嵌套片段不单独作为候选。
				if len(starts) == 0 {
					out = append(out, body[start:i+1])
				}
			}
		}
	}
	// 真正的决策在末尾，优先尝试靠后的候选。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
