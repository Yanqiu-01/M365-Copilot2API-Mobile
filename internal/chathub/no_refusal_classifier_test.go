package chathub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 原 APK 的 chathub 错误哨兵只有四条传输层错误：
//
//	ws dial: %w
//	ws read before completion: %w
//	chathub completion error: %v
//	chathub response deadline exceeded before completion
//
// 二进制中不存在任何拒绝语或限流语文本（"很抱歉，我无法响应"、
// "too many requests"、"太多请求"、"content policy"、"contentfilter" 等
// 全部为 0 次），也不存在 ErrRateLimitNotice / ErrEmptyCompletion 的错误
// 字符串。取证方式已用已知字符串（writeAtCursor、ConciseWithPadding、
// no accounts; login first）交叉验证可靠。
//
// 结论：模型的短回答属于成功响应，必须原样返回给客户端。上游 HEXUXIU 版
// 的 rateLimited() / contentPolicyBlocked() 文本判定在二开版中已被删除；
// 恢复过程曾把它们带回，导致模型对敏感内容的正常拒绝、以及任何空补全被
// 当作上游故障抛出 —— 表现为能力评测每一步都 HTTP 502 tool_router_error，
// 以及普通问候偶发失败。
func TestNoTextBasedRefusalOrRateLimitClassifier(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean("client.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	// 判定函数本身不得存在。
	for _, sym := range []string{
		"rateLimited :=",
		"contentPolicyBlocked :=",
		"meteringOutOfCredits :=",
	} {
		if strings.Contains(src, sym) {
			t.Errorf("client.go defines %q: the original APK classifies transport failures only, never reply text", sym)
		}
	}

	// 也不得在返回路径上使用这些哨兵。
	for _, sym := range []string{
		"return ErrRateLimitNotice",
		"return Result{}, ErrRateLimitNotice",
		"return Result{}, ErrEmptyCompletion",
		"ErrContentPolicyBlocked",
		"ErrMeteringOutOfCredits",
	} {
		if strings.Contains(src, sym) {
			t.Errorf("client.go returns %q: a short or empty model reply is a successful response in the original APK", sym)
		}
	}

	// 拒绝语字面量不得出现在代码里（注释中的说明不计）。
	var code strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
			continue
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	lowered := strings.ToLower(code.String())
	for _, phrase := range []string{
		"无法响应",
		"太多请求",
		"too many requests",
		"content policy",
		"contentfilter",
		"safetyblocked",
	} {
		if strings.Contains(lowered, strings.ToLower(phrase)) {
			t.Errorf("client.go matches reply text %q: absent from the original APK binary", phrase)
		}
	}
}
