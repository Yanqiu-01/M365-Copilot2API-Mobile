package web

import (
	"log"
	"runtime/debug"
)

// safeGo 启动一个带 panic 兜底的 goroutine。
//
// recoverPanics 是 HTTP 中间件，只覆盖请求 goroutine。后台 goroutine 里的
// panic 不经过它，会直接终止整个 Go 子进程 —— 用户侧表现为服务无声重启、
// 管理会话失效（adminSessions 是纯内存 map，重启即清空）。这与此前排查过的
// 「登录态莫名丢失」是同一类现象的另一条成因。
//
// label 只用于日志定位，不参与控制流。
func safeGo(label string, fn func()) {
	safeGoWithCleanup(label, fn, nil)
}

// safeGoWithCleanup 同 safeGo，但在 panic 被捕获后额外执行 onPanic，
// 用于把外部可见状态收敛掉（例如把评测运行标记为 error，而不是永远 running）。
func safeGoWithCleanup(label string, fn func(), onPanic func(rec any)) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[recover] goroutine %s panic: %v\n%s", label, rec, debug.Stack())
				if onPanic != nil {
					// cleanup 自身再 panic 不应升级为进程级崩溃。
					func() {
						defer func() {
							if inner := recover(); inner != nil {
								log.Printf("[recover] goroutine %s cleanup panic: %v", label, inner)
							}
						}()
						onPanic(rec)
					}()
				}
			}
		}()
		fn()
	}()
}
