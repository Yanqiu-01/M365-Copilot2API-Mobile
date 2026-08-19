package chathub

import (
	"encoding/json"
	"testing"
)

// 思考时间显示为 0 的根因回归测试。
//
// 客户端（如 RikkaHub）按「第一个思考增量到最后一个思考增量的时间差」
// 计算思考时长。修复前思考内容有两条来源，覆盖面不一致：
//
//	实时路径  只看 type=1 target=update 帧的 arguments[].messages
//	兜底路径  reasoningFromFrames 覆盖 arguments、item.messages、messages，
//	          且只在完成帧执行一次
//
// 差集里的内容只在完成帧被一次性补发，首末增量几乎同时抵达，
// 思考时长于是被算成 0。reasoningPump 让两条路径取材一致。
func TestReasoningPumpCoversAllFrameShapes(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		want  string
	}{
		{
			name: "update arg.messages",
			frame: `{"type":1,"target":"update","arguments":[{"messages":[
				{"text":"先分析约束","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
			want: "先分析约束",
		},
		{
			name: "top level messages",
			frame: `{"type":1,"target":"update","messages":[
				{"text":"再检查边界","addToChainOfThought":true}]}`,
			want: "再检查边界",
		},
		{
			// 修复前这一形态完全依赖完成帧兜底，是思考时长归零的主因。
			name: "completion item.messages",
			frame: `{"type":2,"item":{"messages":[
				{"text":"最后汇总","contentOrigin":"ChainOfThoughtSummary"}]}}`,
			want: "最后汇总",
		},
		{
			name: "argument itself is the message",
			frame: `{"type":1,"target":"update","arguments":[
				{"text":"直接挂在 argument 上","addToChainOfThought":true}]}`,
			want: "直接挂在 argument 上",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var emitted []string
			pump := newReasoningPump(func(ev StreamEvent) error {
				if ev.Kind != "reasoning" {
					t.Errorf("unexpected event kind %q", ev.Kind)
				}
				emitted = append(emitted, ev.Text)
				return nil
			})
			if err := pump.push(json.RawMessage(tc.frame)); err != nil {
				t.Fatal(err)
			}
			if len(emitted) == 0 {
				t.Fatalf("pump 未实时推送思考内容；该形态会退化为完成帧一次性补发，导致思考时长为 0")
			}
			if pump.text() != tc.want {
				t.Errorf("accumulated=%q want %q", pump.text(), tc.want)
			}
			// 与兜底路径的产出必须一致，否则两条路径仍有差集。
			if fallback := reasoningFromFrames([]json.RawMessage{json.RawMessage(tc.frame)}); fallback != pump.text() {
				t.Errorf("pump=%q fallback=%q 两条路径覆盖面不一致", pump.text(), fallback)
			}
		})
	}
}

// ChatHub 会重复投递同一张思考卡片且内容逐步增长，
// pump 必须只推送新增后缀，否则客户端会看到重复文本。
func TestReasoningPumpEmitsIncrementsOnly(t *testing.T) {
	var emitted []string
	pump := newReasoningPump(func(ev StreamEvent) error {
		emitted = append(emitted, ev.Text)
		return nil
	})

	frames := []string{
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"第一步","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"第一步第二步","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"第一步第二步第三步","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
	}
	for _, frame := range frames {
		if err := pump.push(json.RawMessage(frame)); err != nil {
			t.Fatal(err)
		}
	}

	if len(emitted) != 3 {
		t.Fatalf("emitted=%v want 3 increments", emitted)
	}
	for i, want := range []string{"第一步", "第二步", "第三步"} {
		if emitted[i] != want {
			t.Errorf("increment[%d]=%q want %q", i, emitted[i], want)
		}
	}
	if pump.text() != "第一步第二步第三步" {
		t.Errorf("accumulated=%q", pump.text())
	}
}

// 完全相同的帧重复到达时不得重复推送。
func TestReasoningPumpDeduplicatesIdenticalFrames(t *testing.T) {
	count := 0
	pump := newReasoningPump(func(StreamEvent) error { count++; return nil })
	frame := json.RawMessage(`{"type":1,"target":"update","arguments":[{"messages":[
		{"text":"只应出现一次","contentOrigin":"ChainOfThoughtSummary"}]}]}`)
	for i := 0; i < 3; i++ {
		if err := pump.push(frame); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Errorf("emitted %d times, want 1", count)
	}
	if pump.text() != "只应出现一次" {
		t.Errorf("accumulated=%q", pump.text())
	}
}

// 普通正文与工具帧不得被误判为思考内容。
func TestReasoningPumpIgnoresNonReasoning(t *testing.T) {
	pump := newReasoningPump(func(ev StreamEvent) error {
		t.Errorf("unexpected reasoning emit: %q", ev.Text)
		return nil
	})
	for _, frame := range []string{
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"这是正文"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"搜索中","messageType":"Progress"}]}]}`,
		`{"type":6}`,
		`not json`,
	} {
		if err := pump.push(json.RawMessage(frame)); err != nil {
			t.Fatal(err)
		}
	}
	if pump.text() != "" {
		t.Errorf("accumulated=%q want empty", pump.text())
	}
}

// candidateMessages 必须下钻 arguments[].messages —— 这是最主流的形态，
// 漏掉它会让思考内容只能靠完成帧兜底。
func TestCandidateMessagesReachesArgumentMessages(t *testing.T) {
	var frame map[string]any
	raw := `{"type":1,"target":"update","arguments":[{"messages":[
		{"text":"嵌套在 argument.messages 里","contentOrigin":"ChainOfThoughtSummary"}]}]}`
	if err := json.Unmarshal([]byte(raw), &frame); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, message := range candidateMessages(frame) {
		if text, _ := message["text"].(string); text == "嵌套在 argument.messages 里" {
			found = true
		}
	}
	if !found {
		t.Error("candidateMessages 未下钻到 arguments[].messages")
	}
}
