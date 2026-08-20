package chathub

import (
	"fmt"
	"log"
	"runtime/debug"
)

// safeGoDeliver 启动一个 goroutine，并保证「无论是否 panic，都会向调用方
// 交付一个结果」。
//
// chathub 的后台 goroutine 都是 channel 交付模式：调用方在 select 上等结果。
// 直接加 defer recover 会带来更糟的故障 —— panic 被吞掉后 channel 永远收不到
// 值，调用方阻塞到超时甚至永久挂住。因此这里把 recover 与「交付兜底值」绑在
// 一起：fn 正常结束时由它自己交付，panic 时由 onPanic 交付一个错误。
//
// 不加保护的后果同样明确：这些 goroutine 不经过任何 HTTP 中间件，一次 panic
// 会终止整个 Go 子进程。
func safeGoDeliver(label string, fn func(), onPanic func(err error)) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[recover] goroutine %s panic: %v\n%s", label, rec, debug.Stack())
				if onPanic != nil {
					func() {
						defer func() {
							if inner := recover(); inner != nil {
								log.Printf("[recover] goroutine %s cleanup panic: %v", label, inner)
							}
						}()
						onPanic(fmt.Errorf("internal panic in %s: %v", label, rec))
					}()
				}
			}
		}()
		fn()
	}()
}
