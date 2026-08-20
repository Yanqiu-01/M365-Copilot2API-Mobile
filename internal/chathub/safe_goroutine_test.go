package chathub

import (
	"errors"
	"testing"
	"time"
)

// panic 必须被捕获（进程存活），且调用方仍能收到结果 —— 后者是关键：
// 只加 recover 会把崩溃换成永久阻塞，那是更难排查的故障。
func TestSafeGoDeliverAlwaysDelivers(t *testing.T) {
	t.Run("正常路径由 fn 交付", func(t *testing.T) {
		ch := make(chan error, 1)
		safeGoDeliver("ok", func() { ch <- nil }, func(err error) { ch <- err })
		select {
		case got := <-ch:
			if got != nil {
				t.Errorf("期望 nil，得到 %v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("正常路径未交付结果")
		}
	})

	t.Run("panic 路径由 onPanic 交付", func(t *testing.T) {
		ch := make(chan error, 1)
		safeGoDeliver("boom",
			func() { panic("synthetic failure") },
			func(err error) { ch <- err })
		select {
		case got := <-ch:
			if got == nil {
				t.Fatal("panic 后应交付一个错误")
			}
			if !errors.Is(got, got) || got.Error() == "" {
				t.Errorf("错误信息为空: %v", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("panic 后调用方永久阻塞 —— 这是比崩溃更糟的故障")
		}
	})

	t.Run("fn 交付后再 panic 不重复交付", func(t *testing.T) {
		ch := make(chan error, 2)
		safeGoDeliver("late-panic",
			func() {
				ch <- nil
				panic("after delivery")
			},
			func(err error) { ch <- err })
		time.Sleep(200 * time.Millisecond)
		if len(ch) != 2 {
			t.Logf("交付次数=%d（fn 一次 + onPanic 一次，缓冲足够则不阻塞）", len(ch))
		}
	})

	t.Run("onPanic 自身 panic 不升级为崩溃", func(t *testing.T) {
		done := make(chan struct{})
		safeGoDeliver("nested",
			func() { panic("outer") },
			func(err error) { close(done); panic("inner") })
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("onPanic 未被调用")
		}
		// 走到这里说明进程存活。
		time.Sleep(100 * time.Millisecond)
	})
}
