package chathub

import (
	"encoding/json"
	"strings"
)

// reasoningPump 把每一帧里新出现的思考文本即时推送出去。
//
// 为什么需要它：客户端（如 RikkaHub）按「第一个思考增量到最后一个思考增量
// 的时间差」计算思考时长。此前思考内容有两条来源，覆盖面不一致：
//
//	实时路径  只看 type=1 target=update 帧的 arguments[].messages
//	兜底路径  reasoningFromFrames 覆盖 arguments、arguments[].messages、
//	          item.messages、messages，且只在完成帧执行一次
//
// 差集里的思考内容（尤其挂在 item.messages 的）只会在完成帧被一次性补发，
// 客户端看到的首末增量几乎同时抵达，思考时长于是被算成 0。
//
// reasoningPump 用与兜底完全相同的取材逻辑（candidateMessages）逐帧扫描，
// 因此两条路径覆盖面一致；已发送过的前缀不再重复，保证增量语义。
type reasoningPump struct {
	sent  strings.Builder
	seen  map[string]bool
	emit  func(StreamEvent) error
	total strings.Builder
}

func newReasoningPump(emit func(StreamEvent) error) *reasoningPump {
	return &reasoningPump{seen: map[string]bool{}, emit: emit}
}

// push 扫描一帧，按出现顺序推送尚未发送过的思考片段。
func (p *reasoningPump) push(raw json.RawMessage) error {
	if p == nil {
		return nil
	}
	var frame map[string]any
	if json.Unmarshal(raw, &frame) != nil {
		return nil
	}
	for _, message := range candidateMessages(frame) {
		text, _ := message["text"].(string)
		if text == "" {
			continue
		}
		origin, _ := message["contentOrigin"].(string)
		addToChainOfThought, _ := message["addToChainOfThought"].(bool)
		if origin != "ChainOfThoughtSummary" && !addToChainOfThought {
			continue
		}
		// ChatHub 会重复投递同一张思考卡片（内容逐步增长），
		// 因此按「累计文本的新增后缀」推送，而不是整段重发。
		if p.seen[text] {
			continue
		}
		p.seen[text] = true
		delta := text
		if prev := p.total.String(); prev != "" && strings.HasPrefix(text, prev) {
			delta = text[len(prev):]
			p.total.Reset()
			p.total.WriteString(text)
		} else {
			p.total.WriteString(text)
		}
		if delta == "" {
			continue
		}
		p.sent.WriteString(delta)
		if p.emit == nil {
			continue
		}
		if err := p.emit(StreamEvent{Kind: "reasoning", Text: delta, Raw: raw}); err != nil {
			return err
		}
	}
	return nil
}

// text 返回已推送的全部思考内容，用作 Result.Reasoning，
// 避免完成帧再从原始帧重算而产生重复。
func (p *reasoningPump) text() string {
	if p == nil {
		return ""
	}
	return p.sent.String()
}
