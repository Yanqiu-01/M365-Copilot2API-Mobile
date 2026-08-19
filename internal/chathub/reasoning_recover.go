package chathub

import (
	"encoding/json"
	"strings"
)

// reasoningFromFrames recovers Chain-of-Thought summary text from raw SignalR
// frames when the normal update path did not expose a reasoning event.
//
// APK evidence:
//   - m365-copilot2api/internal/chathub/reasoningFromFrames, lines 20–57
//   - candidateMessages, lines 58–176
//   - caller: (*Client).chatWithHandlers, which invokes this only when its
//     live reasoning buffer is empty.
func reasoningFromFrames(frames []json.RawMessage) string {
	var out []string
	for _, raw := range frames {
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		for _, message := range candidateMessages(frame) {
			text, _ := message["text"].(string)
			if text == "" {
				continue
			}
			origin, _ := message["contentOrigin"].(string)
			addToChainOfThought, _ := message["addToChainOfThought"].(bool)
			if origin == "ChainOfThoughtSummary" || addToChainOfThought {
				out = append(out, text)
			}
		}
	}
	return strings.Join(out, "")
}

// candidateMessages returns message maps reachable through the SignalR shapes
// present in the APK: arguments, item.messages, and messages.
func candidateMessages(frame map[string]any) []map[string]any {
	var out []map[string]any
	appendMaps := func(value any) {
		items, ok := value.([]any)
		if !ok {
			return
		}
		for _, item := range items {
			if message, ok := item.(map[string]any); ok {
				out = append(out, message)
			}
		}
	}

	if arguments, ok := frame["arguments"].([]any); ok {
		for _, rawArgument := range arguments {
			argument, ok := rawArgument.(map[string]any)
			if !ok {
				continue
			}
			// The APK walks arguments directly. Some SignalR variants put a
			// message map here rather than under a messages array.
			out = append(out, argument)
			// 常见形态是 arguments[].messages；必须下钻，否则这条最主流的
			// 路径会被漏掉（曾导致思考内容仅由完成帧兜底补发，
			// 客户端两个增量几乎同时到达，思考时长被算成 0）。
			appendMaps(argument["messages"])
		}
	}
	if item, ok := frame["item"].(map[string]any); ok {
		appendMaps(item["messages"])
	}
	appendMaps(frame["messages"])
	return out
}
