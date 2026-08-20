package web

import (
	"net/http"
	"os"
)

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'; form-action 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; script-src 'self' 'unsafe-inline' https://unpkg.com https://cdn.jsdelivr.net")
		if r.URL.Path == "/" || r.URL.Path == "/login" || r.URL.Path == "/api/admin/login" || r.URL.Path == "/api/admin/session" || r.URL.Path == "/api/admin/change-password" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// rootPage 只提供两个页面：/ 与 /login。
//
// APK 证据（tools/apktool，security_http.go:22-45，848 字节）：
//   - +0x0054 CMP #47 判单字符 '/'，+0x0064 CMP #6 配整数化比较 "/login"；
//   - +0x00d4 CMP #3 与 +0x00f8 CMP #4 分别判方法 "GET" / "HEAD"；
//   - rodata 中只存在 "web/index.html"（@0x4d4181），
//     不存在 "web/conversation.html" 或 "conversation.html"。
//
// 此前本地多出的 /conversation 分支属虚构：APK 的 assets/web 仅有
// index.html / login.html / debug.html 三个文件。
func (s *Server) rootPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/login" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	// 原 APK 实测 /login 与 / 返回同一份 index.html（逐字节相同，102579
	// 字节）：登录态由前端 JS 依据 /api/admin/session 切换，没有独立的
	// 登录页路由。上游那句 name = "login.html" 的分支在二开版里已被删除，
	// 恢复时误将其带回，导致 /login 只返回 10611 字节的空壳页面。
	name := "web/index.html"
	f, err := os.Open(name)
	if err != nil {
		http.Error(w, "web interface unavailable", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "web interface unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, name, st.ModTime(), f)
}
