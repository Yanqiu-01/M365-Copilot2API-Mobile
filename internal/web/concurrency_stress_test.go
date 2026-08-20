package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// 并发压力测试：替代 -race 的实证手段。
//
// 本项目运行环境的用户地址空间是 39 位（Android 内核 CONFIG_ARM64_VA_BITS=39），
// 而 Go 的 arm64 TSan 运行时要求 48 位。实测把 race 运行时里的版本检查
// （__tsan::InitializePlatformEarly 中的 cmp w0,#0x30 / b.ne）改成 nop 后，
// 报错变为 "ThreadSanitizer failed to allocate 0x2010000 bytes at address
// 200002b60000" —— TSan 要在约 35 TB 处映射影子内存，而 39 位空间上限是
// 512 GB，根本不存在该地址。这是内核编译选项，非 root 不可改，QEMU 用户态
// 也继承宿主布局。
//
// 因此改用高并发压力 + 状态一致性断言。它不保证捕获所有数据竞争，但能实际
// 暴露 map 并发读写崩溃（Go 对此直接 fatal，不可 recover）、计数丢失、结构
// 被写坏等问题。若将来能在支持 48 位的环境运行，应补跑 go test -race。

const stressAdminCookie = "m365_admin_session"

func newStressServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	return &Server{
		settings:      &settingsStore{path: filepath.Join(dir, "s.json"), v: defaultRuntimeSettings()},
		adminPassword: "secret",
		adminSessions: map[string]time.Time{},
		loginAttempts: map[string]loginAttempt{},
		pkce:          map[string]pendingPKCE{},
	}
}

// 管理会话 map 的并发读写。这是最关键的一处：adminSessions 被登录、校验、
// 过期清理三条路径同时访问。
func TestConcurrentAdminSessionAccess(t *testing.T) {
	s := newStressServer(t)

	const workers = 40
	const iterations = 150

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch i % 3 {
				case 0:
					s.mu.Lock()
					s.adminSessions[fmt.Sprintf("tok-%d-%d", id, i)] = time.Now().Add(time.Hour)
					s.mu.Unlock()
				case 1:
					req := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
					req.AddCookie(&http.Cookie{Name: stressAdminCookie, Value: fmt.Sprintf("tok-%d-%d", id, i-1)})
					_ = s.validAdminSession(req)
				case 2:
					s.mu.Lock()
					pruneAdminSessions(s.adminSessions, time.Now())
					s.mu.Unlock()
				}
			}
		}(w)
	}
	wg.Wait()

	s.mu.Lock()
	remaining := len(s.adminSessions)
	s.mu.Unlock()
	t.Logf("并发结束，残留会话 %d 条", remaining)
}

// 设置读写并发：settings 在请求路径上被高频读取，管理页偶发写入。
func TestConcurrentSettingsAccess(t *testing.T) {
	dir := t.TempDir()
	store := &settingsStore{path: filepath.Join(dir, "s.json"), v: defaultRuntimeSettings()}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []string

	for r := 0; r < 30; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				got := store.get()
				// 读到的结构必须自洽，不能是半写入状态
				if got.MaxOutputTokens < 0 {
					mu.Lock()
					failures = append(failures, fmt.Sprintf("非法 MaxOutputTokens=%d", got.MaxOutputTokens))
					mu.Unlock()
					return
				}
			}
		}()
	}

	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 60; i++ {
				cur := store.get()
				cur.MaxOutputTokens = 4096 + id*100
				_ = store.save(cur)
			}
		}(w)
	}
	wg.Wait()

	for _, f := range failures {
		t.Error(f)
	}
	if final := store.get(); final.MaxOutputTokens < 4096 {
		t.Errorf("最终值异常: %d", final.MaxOutputTokens)
	}
}

// 用量累加不得丢记录：丢失会让统计页显示偏低。
func TestConcurrentUsageRecording(t *testing.T) {
	// 必须像 openUsageLog 那样初始化 persist，否则 record 会解引用空指针。
	// （这是测试构造问题，不是产品缺陷：生产路径只通过 openUsageLog 创建。）
	log := &usageLog{Path: filepath.Join(t.TempDir(), "usage.jsonl")}
	log.persist = &persistStore{flush: log.flush}

	const workers = 32
	const perWorker = 100

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				log.record(UsageRecord{
					Time:         time.Now(),
					Model:        "gpt-5.6-reasoning",
					Endpoint:     "/v1/chat/completions",
					InputTokens:  10,
					OutputTokens: 5,
					Status:       200,
				})
			}
		}(w)
	}
	wg.Wait()

	got := len(log.snapshotRecords())
	want := workers * perWorker
	if got != want {
		t.Errorf("记录数 %d，期望 %d —— 并发累加有丢失", got, want)
	}
}

// 帧捕获在并发下的稳定性：抓帧发生在每个请求的读循环里，多请求同时写入。
func TestConcurrentFrameCapture(t *testing.T) {
	// 必须先打开捕获开关：recordRouterFrames 在关闭时直接 return，
	// 否则这个测试只是空转（实测「保留 0 组帧」）。
	restore := resetRouterFramesForTest(t)
	defer restore()

	var wg sync.WaitGroup
	for w := 0; w < 24; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 80; i++ {
				recordRouterFrames(routerFrameInput{
					RequestID: fmt.Sprintf("req-%d-%d", id, i),
					Stage:     "router",
					Prompt:    "prompt text",
					Text:      "CALL_TOOL: read_file {}",
					Events:    []json.RawMessage{json.RawMessage(`{"type":1}`)},
				})
			}
		}(w)
	}
	wg.Wait()

	groups := routerFrameSnapshot()
	t.Logf("并发写入后保留 %d 组帧", len(groups))
	if len(groups) == 0 {
		t.Fatal("未记录任何帧 —— 测试未真正触达代码路径")
	}
	for _, g := range groups {
		if g.RequestID == "" {
			t.Error("出现 RequestID 为空的帧组 —— 结构被并发写坏")
			break
		}
		if g.Stage == "" {
			t.Error("出现 Stage 为空的帧组 —— 结构被并发写坏")
			break
		}
	}
}

// 账号健康状态的并发更新：失败上报来自多个并发请求，同一账号会被多个
// goroutine 同时标记失败、标记成功、查询可用性。
func TestConcurrentAccountStateUpdates(t *testing.T) {
	health := newAccountHealth()
	// 必须用能被 IsRateLimited 识别的错误类型，否则 MarkFailure 直接返回，
	// cooldown map 永不写入，测试是空转（实测「健康记录 0 条」）。
	rateLimited := &UpstreamHTTPError{Status: 429}

	// 分两组账号：failing 组只标记失败（因此快照里必然留下 cooldown 记录），
	// churn 组混合成功与失败。MarkSuccess 会删除 cooldown 项，若所有账号都
	// 混合调用，快照可能恰好为空而使断言失去意义。
	var wg sync.WaitGroup
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			account := fmt.Sprintf("failing%d@example.com", id%3)
			for i := 0; i < 60; i++ {
				health.MarkFailure(account, rateLimited, time.Minute)
				_ = health.Available(account)
			}
		}(w)
	}
	for w := 0; w < 12; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			account := fmt.Sprintf("churn%d@example.com", id%3)
			for i := 0; i < 60; i++ {
				switch i % 3 {
				case 0:
					health.MarkFailure(account, rateLimited, time.Minute)
				case 1:
					health.MarkSuccess(account)
				case 2:
					_ = health.Snapshot()
				}
			}
		}(w)
	}
	wg.Wait()

	// 快照结构必须完整可读，且无 fatal（内部 map 未被并发写坏）。
	snap := health.Snapshot()
	t.Logf("并发结束，健康记录 %d 条", len(snap))
	if len(snap) == 0 {
		t.Fatal("无任何健康记录 —— 测试未真正触达代码路径")
	}
	for account, fields := range snap {
		if account == "" || fields == nil {
			t.Error("快照中出现空账号或空字段 —— 结构被并发写坏")
			break
		}
	}
}

// 端到端并发：多个 HTTP 请求同时打同一个 Server 实例，覆盖中间件、鉴权、
// 设置读取整条链路。这是最接近真实负载的一项 —— maxConcurrentChats 允许
// 4 路并发聊天，每路都会读设置、写用量、可能抓帧。
func TestConcurrentHTTPRequests(t *testing.T) {
	s := newStressServer(t)

	// 预置一个有效会话，让请求能穿过鉴权进入 handler。
	token := "stress-token"
	s.mu.Lock()
	s.adminSessions[token] = time.Now().Add(time.Hour)
	s.mu.Unlock()

	handler := s.adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 在 handler 内部读设置，模拟真实请求路径。
		_ = currentSettings()
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{
		"/api/stages",
		"/api/admin/debug/router-frames",
		"/api/admin/account-health",
		"/api/admin/settings",
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}

	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				req := httptest.NewRequest(http.MethodGet, paths[(id+i)%len(paths)], nil)
				req.RemoteAddr = "127.0.0.1:40000"
				if i%2 == 0 {
					req.AddCookie(&http.Cookie{Name: stressAdminCookie, Value: token})
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				mu.Lock()
				codes[rec.Code]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	total := 0
	for code, n := range codes {
		t.Logf("  HTTP %d: %d 次", code, n)
		total += n
	}
	if want := 32 * 40; total != want {
		t.Errorf("响应总数 %d，期望 %d —— 有请求未完成", total, want)
	}
	// 不应出现 5xx：并发下的 panic 会被 recoverPanics 转成 500。
	for code, n := range codes {
		if code >= 500 {
			t.Errorf("出现 %d 响应 %d 次 —— 并发下 handler 崩溃", code, n)
		}
	}
}
