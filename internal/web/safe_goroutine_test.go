package web

import (
	"testing"
	"time"
)

// 后台 goroutine 的 panic 必须被捕获：recoverPanics 是 HTTP 中间件，
// 只覆盖请求 goroutine，后台任务的 panic 会直接终止 Go 子进程 ——
// 用户侧表现为服务无声重启、管理会话失效。
func TestSafeGoRecoversPanic(t *testing.T) {
	done := make(chan struct{})
	safeGo("test.panic", func() {
		defer close(done)
		panic("synthetic")
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine 未执行")
	}
	// 未崩溃即通过。
	time.Sleep(100 * time.Millisecond)
}

func TestSafeGoWithCleanupRunsOnPanic(t *testing.T) {
	recovered := make(chan any, 1)
	safeGoWithCleanup("test.cleanup",
		func() { panic("synthetic") },
		func(rec any) { recovered <- rec })

	select {
	case rec := <-recovered:
		if rec == nil {
			t.Error("cleanup 应收到 panic 值")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup 未被调用 —— 外部状态会永远卡在中间态")
	}
}

func TestSafeGoWithCleanupSkippedOnSuccess(t *testing.T) {
	called := make(chan struct{}, 1)
	finished := make(chan struct{})
	safeGoWithCleanup("test.ok",
		func() { close(finished) },
		func(rec any) { called <- struct{}{} })

	<-finished
	time.Sleep(200 * time.Millisecond)
	if len(called) != 0 {
		t.Error("正常结束不应触发 cleanup")
	}
}
