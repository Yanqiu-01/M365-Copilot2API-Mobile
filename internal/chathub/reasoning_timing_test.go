package chathub

import (
	"encoding/json"
	"testing"
	"time"
)

// 端到端验证「思考时长不再为 0」：模拟一次带思考的完整帧序列，
// 断言思考增量是随帧分散抵达的，而不是在完成帧一次性补发。
//
// 客户端按首末思考增量的时间差计算思考时长，因此只要增量分散在
// 多个帧的处理时点上，时长就是真实的。
func TestReasoningIncrementsArriveProgressively(t *testing.T) {
	// 一次典型对话：思考卡片逐步增长，正文随后产生，最后是完成帧。
	frames := []string{
		`{"type":1,"target":"update","arguments":[{"messages":[
			{"text":"理解题意","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[
			{"text":"理解题意，列出约束","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[
			{"text":"这是正文的一部分"}]}]}`,
		`{"type":1,"target":"update","arguments":[{"messages":[
			{"text":"理解题意，列出约束，验证结论","contentOrigin":"ChainOfThoughtSummary"}]}]}`,
		// 完成帧里 item.messages 又带了一段思考 —— 修复前这一段是
		// 唯一被"补发"的内容，也是时长归零的直接原因。
		`{"type":2,"item":{"messages":[
			{"text":"补充：边界已复核","addToChainOfThought":true}]}}`,
	}

	type stamped struct {
		text string
		at   time.Time
	}
	var got []stamped
	pump := newReasoningPump(func(ev StreamEvent) error {
		got = append(got, stamped{ev.Text, time.Now()})
		// 让相邻帧之间有可测量的间隔，模拟真实网络节奏。
		time.Sleep(time.Millisecond)
		return nil
	})

	for _, frame := range frames {
		if err := pump.push(json.RawMessage(frame)); err != nil {
			t.Fatal(err)
		}
	}

	if len(got) < 4 {
		t.Fatalf("只收到 %d 个思考增量，期望 4 个（三次递增 + 完成帧补充）：%v", len(got), got)
	}

	wantTexts := []string{"理解题意", "，列出约束", "，验证结论", "补充：边界已复核"}
	for i, want := range wantTexts {
		if got[i].text != want {
			t.Errorf("increment[%d]=%q want %q", i, got[i].text, want)
		}
	}

	// 关键断言：首末增量之间必须有可观测的时间差。
	// 若思考内容在完成帧一次性补发，这个差值会是 0。
	span := got[len(got)-1].at.Sub(got[0].at)
	if span <= 0 {
		t.Fatalf("首末思考增量时间差为 %v，客户端会把思考时长显示为 0", span)
	}

	// 正文不得混进思考流。
	for _, entry := range got {
		if entry.text == "这是正文的一部分" {
			t.Error("正文被误当作思考内容推送")
		}
	}

	if pump.text() != "理解题意，列出约束，验证结论补充：边界已复核" {
		t.Errorf("accumulated=%q", pump.text())
	}
}

// 没有任何思考内容时，pump 不应产出，也不应影响正文。
func TestReasoningPumpSilentWithoutChainOfThought(t *testing.T) {
	emitted := 0
	pump := newReasoningPump(func(StreamEvent) error { emitted++; return nil })
	for _, frame := range []string{
		`{"type":1,"target":"update","arguments":[{"messages":[{"text":"只有正文"}]}]}`,
		`{"type":2,"item":{"result":{"message":"done"}}}`,
		`{"type":3}`,
	} {
		if err := pump.push(json.RawMessage(frame)); err != nil {
			t.Fatal(err)
		}
	}
	if emitted != 0 {
		t.Errorf("emitted=%d want 0", emitted)
	}
	if pump.text() != "" {
		t.Errorf("accumulated=%q want empty", pump.text())
	}
}
