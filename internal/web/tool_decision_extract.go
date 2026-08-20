package web

import (
	"encoding/json"
	"strings"
	"unicode"
)

// 路由决策抽取：格式无关的统一实现。
//
// 背景（为什么不再按模型逐个打补丁）：
// 路由轮要求模型「思考完，最后一行给出 CALL_TOOL: name({...}) 或
// NO_TOOL_NEEDED」；修复轮与 required 重试轮则要求 {"calls":[...]} 的 JSON。
// 实际回复的包装方式随模型、档位、语言千变万化 —— markdown 围栏、粗体、
// 行首序号、中文全角冒号、指令后追加解释、思考里复述示例 JSON……
// 逐个 if 分支去猜，每换一个模型就要返工一次。
//
// 因此这里改成两段式：
//  1. 抽取：把整段文本里所有「看起来像一次调用」的候选全部枚举出来，
//     不判断它是否合法、也不关心它被什么包裹；
//  2. 校验：对候选按出现顺序统一做「工具存在 + choice 允许 + schema 合法」
//     检查，取最后一个通过的。
//
// 「取最后一个」来自 APK 的规则原文 end with EXACTLY one line：真正的决策在
// 末尾，思考中的示例、被否决的方案都在前面。新增一种包装方式时只需让抽取器
// 多认一种边界，校验与调用点都不必改动。

// toolCandidate 是一次尚未校验的候选调用。
type toolCandidate struct {
	Name string
	Args map[string]any
	At   int // 在原文中的位置，用于「取最后一个」
}

const (
	directiveMarker = "call_tool"
	noToolMarker    = "no_tool_needed"
)

// normalizeDecisionText 统一全角标点与代码围栏，使后续抽取只面对一种形态。
// 只替换等宽的 ASCII 对应物，不改变字节长度以外的语义。
func normalizeDecisionText(text string) string {
	replacer := strings.NewReplacer(
		"：", ":",
		"（", "(",
		"）", ")",
		"“", `"`,
		"”", `"`,
		"，", ",",
		"\r\n", "\n",
		"\r", "\n",
	)
	return replacer.Replace(text)
}

// stripDecorations 去掉不影响语义的包装字符，让指令行的边界可被识别。
// markdown 围栏、粗体星号、行首列表符号都属于此类。
func stripDecorations(line string) string {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "`")
	line = strings.TrimSpace(line)
	// 行首的列表/序号标记：- * 1. 2) 等。
	for {
		trimmed := strings.TrimLeft(line, "-*•\t ")
		if trimmed == line {
			break
		}
		line = trimmed
	}
	if i := strings.IndexFunc(line, func(r rune) bool { return !unicode.IsDigit(r) }); i > 0 {
		if i < len(line) && (line[i] == '.' || line[i] == ')') {
			line = strings.TrimSpace(line[i+1:])
		}
	}
	// 成对的强调标记。
	for _, mark := range []string{"**", "__", "*", "_"} {
		for strings.HasPrefix(line, mark) && strings.HasSuffix(line, mark) && len(line) > 2*len(mark) {
			line = strings.TrimSpace(line[len(mark) : len(line)-len(mark)])
		}
	}
	return strings.Trim(strings.TrimSpace(line), "`")
}

// balancedSpan 从 open 位置起返回配对闭合的区间 [open, close]。
// 字符串字面量内的括号与反斜杠转义不参与配对；未闭合时返回 -1。
func balancedSpan(text string, open int, openCh, closeCh byte) int {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(text); i++ {
		ch := text[i]
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
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractDirectiveCandidates 找出所有 CALL_TOOL: name(<json>) 形式的候选。
// 支持指令前有思考、后有解释，参数跨多行，以及各种装饰包装。
func extractDirectiveCandidates(text string) []toolCandidate {
	var out []toolCandidate
	lower := strings.ToLower(text)
	for offset := 0; ; {
		idx := strings.Index(lower[offset:], directiveMarker)
		if idx < 0 {
			break
		}
		at := offset + idx
		offset = at + len(directiveMarker)

		rest := text[offset:]
		// 跳过标记与名字之间的冒号和空白。
		rest = strings.TrimLeft(rest, ": \t")
		open := strings.Index(rest, "(")
		if open < 0 {
			continue
		}
		name := stripDecorations(rest[:open])
		if name == "" || strings.ContainsAny(name, "\n") {
			continue
		}
		close := balancedSpan(rest, open, '(', ')')
		if close < 0 {
			continue
		}
		args, ok := decodeArguments(rest[open+1 : close])
		if !ok {
			continue
		}
		out = append(out, toolCandidate{Name: name, Args: args, At: at})
	}
	return out
}

// extractEnvelopeCandidates 找出所有 {"calls":[...]} 信封里的候选。
// 逐个平衡对象尝试，因此思考中出现的示例 JSON 不会打断真正的决策。
func extractEnvelopeCandidates(text string) ([]toolCandidate, bool) {
	var out []toolCandidate
	found := false
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		end := balancedSpan(text, i, '{', '}')
		if end < 0 {
			continue
		}
		fragment := text[i : end+1]
		var probe struct {
			Calls []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"calls"`
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal([]byte(fragment), &raw) == nil {
			if _, has := raw["calls"]; has && json.Unmarshal([]byte(fragment), &probe) == nil {
				// 空 calls 也是一个有效决策（明确表示不调用工具）。
				found = true
				for _, c := range probe.Calls {
					if strings.TrimSpace(c.Name) == "" {
						continue
					}
					args := c.Arguments
					if args == nil {
						args = map[string]any{}
					}
					out = append(out, toolCandidate{Name: strings.TrimSpace(c.Name), Args: args, At: i})
				}
			}
		}
		i = end
	}
	return out, found
}

// decodeArguments 解析参数体。允许空参数、允许被空白或围栏包裹。
func decodeArguments(body string) (map[string]any, bool) {
	body = strings.TrimSpace(body)
	body = strings.Trim(body, "`")
	body = strings.TrimSpace(body)
	if body == "" {
		return map[string]any{}, true
	}
	var args map[string]any
	if json.Unmarshal([]byte(body), &args) == nil {
		if args == nil {
			args = map[string]any{}
		}
		return args, true
	}
	return nil, false
}

// hasTrailingNoTool 判断 NO_TOOL_NEEDED 是否作为收尾结论出现。
// 只看最后若干行：思考正文里的提及（「这里不该回 NO_TOOL_NEEDED」）不算。
func hasTrailingNoTool(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-3; i-- {
		line := strings.ToLower(stripDecorations(lines[i]))
		line = strings.Trim(line, " .。!！\"'`*_")
		// 去掉标点后可能又露出装饰字符（如 **NO_TOOL_NEEDED**），再剥一次。
		line = strings.ToLower(stripDecorations(line))
		if line == noToolMarker {
			return true
		}
	}
	return false
}

// selectDecision 对候选做统一校验，返回最后一个通过的调用。
func selectDecision(candidates []toolCandidate, tools []map[string]any, choice any) ([]detectedToolCall, bool) {
	for i := len(candidates) - 1; i >= 0; i-- {
		c := candidates[i]
		if !toolChoiceAllows(choice, c.Name) {
			continue
		}
		fn := toolFunction(c.Name, tools)
		if fn == nil || schemaValid(c.Args, fn) != nil {
			continue
		}
		encoded, err := json.Marshal(c.Args)
		if err != nil {
			continue
		}
		return []detectedToolCall{{
			ID:        callID(c.Name, string(encoded), 0),
			Type:      toolType(c.Name, tools),
			Name:      c.Name,
			Arguments: encoded,
		}}, true
	}
	return nil, false
}

// selectAllValid 保留信封里全部合法调用，顺序不变（一次可返回多个工具）。
func selectAllValid(candidates []toolCandidate, tools []map[string]any, choice any) []detectedToolCall {
	out := make([]detectedToolCall, 0, len(candidates))
	for i, c := range candidates {
		if !toolChoiceAllows(choice, c.Name) {
			continue
		}
		fn := toolFunction(c.Name, tools)
		if fn == nil || schemaValid(c.Args, fn) != nil {
			continue
		}
		encoded, err := json.Marshal(c.Args)
		if err != nil {
			continue
		}
		out = append(out, detectedToolCall{
			ID:        callID(c.Name, string(encoded), i),
			Type:      toolType(c.Name, tools),
			Name:      c.Name,
			Arguments: encoded,
		})
	}
	return out
}
