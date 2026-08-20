package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 本机只读诊断豁免。
//
// 原生 DiagActivity 固定访问 http://127.0.0.1:4141，而它把会话 cookie 存在
// 实例字段里（dex 中没有任何 SharedPreferences 调用），页面一关就丢：每次进
// 「网关诊断」都要重新输密码，退出再进就什么都捕获不到。服务端因此对回环地址
// 放行少数只读诊断端点与捕获开关 —— 它们不返回凭据，帧内容也已脱敏。
//
// 对外访问必须保持原样：任何非回环来源、以及全部敏感端点仍需管理员会话。
func TestLoopbackDiagnosticExemption(t *testing.T) {
	dir := t.TempDir()
	settings := &settingsStore{path: filepath.Join(dir, "settings.json"), v: defaultRuntimeSettings()}
	s := &Server{settings: settings, adminPassword: "secret", adminSessions: map[string]time.Time{}}
	handler := s.adminMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	}))

	call := func(method, path, remote string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"enabled":true}`))
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	const local = "127.0.0.1:54321"
	const remote = "203.0.113.7:54321"

	t.Run("本机只读诊断放行", func(t *testing.T) {
		for _, path := range []string{
			"/api/stages",
			"/api/admin/debug/router-frames",
			"/api/admin/debug/wire",
			"/api/admin/account-health",
		} {
			if code := call(http.MethodGet, path, local); code != http.StatusOK {
				t.Errorf("GET %s from loopback = %d, want 200", path, code)
			}
		}
	})

	t.Run("本机捕获开关放行", func(t *testing.T) {
		for _, path := range []string{
			"/api/admin/debug/router-frames/toggle",
			"/api/admin/debug/wire/toggle",
		} {
			if code := call(http.MethodPost, path, local); code != http.StatusOK {
				t.Errorf("POST %s from loopback = %d, want 200", path, code)
			}
		}
	})

	t.Run("非本机一律需要鉴权", func(t *testing.T) {
		for _, path := range []string{
			"/api/stages",
			"/api/admin/debug/router-frames",
			"/api/admin/debug/wire",
			"/api/admin/account-health",
			"/api/admin/debug/router-frames/toggle",
		} {
			if code := call(http.MethodGet, path, remote); code == http.StatusOK {
				t.Errorf("GET %s from %s must not be exempt", path, remote)
			}
		}
	})

	t.Run("敏感端点即使本机也需鉴权", func(t *testing.T) {
		for _, path := range []string{
			"/api/admin/settings",
			"/api/admin/keys",
			"/api/accounts",
			"/api/admin/models",
			"/api/usage",
		} {
			if code := call(http.MethodGet, path, local); code == http.StatusOK {
				t.Errorf("GET %s from loopback must still require a session, got %d", path, code)
			}
		}
	})

	t.Run("豁免清单必须保持最小", func(t *testing.T) {
		// 新增豁免路径时必须同步更新此断言，避免无意中放开写操作或凭据端点。
		for _, path := range []string{
			"/api/admin/login",
			"/api/admin/change-password",
			"/api/admin/keys/create",
			"/api/admin/settings",
		} {
			if isReadOnlyDiagnosticPath(path) || isCaptureToggle(path) {
				t.Errorf("%s must never be exempt", path)
			}
		}
	})
}
