package auth

import (
	"net/url"
	"strings"
	"testing"
)

// AuthorizationURLWithPrompt 的参数集与 prompt 处理规则。
//
// APK 证据（internal/auth/pkce.go:44-58，1696 字节）：
// 查询参数 client_id(9)、response_type(13)、redirect_uri、
// response_mode(13)、scope(5)、state(5)、code_challenge、
// code_challenge_method(21)，以 "%s?%s"(5) 拼接。
// +0x04ec CMP #4 配 +0x04fc MOVZ #28526 判定 prompt == "none"。
func TestAuthorizationURLWithPrompt(t *testing.T) {
	const endpoint = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"

	build := func(prompt string) url.Values {
		raw := AuthorizationURLWithPrompt(endpoint, "cid", "http://127.0.0.1/cb",
			"st", "ch", "openid profile", prompt)
		if !strings.HasPrefix(raw, endpoint+"?") {
			t.Fatalf("URL 前缀不符: %s", raw)
		}
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed.Query()
	}

	// 固定参数必须齐备。
	q := build("")
	for key, want := range map[string]string{
		"client_id":             "cid",
		"response_type":         "code",
		"redirect_uri":          "http://127.0.0.1/cb",
		"response_mode":         "query",
		"scope":                 "openid profile",
		"state":                 "st",
		"code_challenge":        "ch",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s=%q want %q", key, got, want)
		}
	}
	// 空 prompt 不写入。
	if q.Has("prompt") {
		t.Error("空 prompt 不应出现在查询串中")
	}

	// prompt=none 被丢弃：Microsoft 身份平台对该值有特殊语义，
	// 与 PKCE 交互式登录冲突。
	if build("none").Has("prompt") {
		t.Error(`prompt="none" 不应写入查询串`)
	}
	if build("  none  ").Has("prompt") {
		t.Error("带空白的 none 也应被丢弃")
	}

	// 其余值原样透传（Java 侧默认注入 "login"）。
	for _, prompt := range []string{"login", "consent", "select_account"} {
		if got := build(prompt).Get("prompt"); got != prompt {
			t.Errorf("prompt=%q 未透传，得到 %q", prompt, got)
		}
	}
	if got := build("  login  ").Get("prompt"); got != "login" {
		t.Errorf("prompt 应去除首尾空白，得到 %q", got)
	}
}

// AuthorizationURL 是读 M365_PROMPT 的薄壳，与显式传参结果一致。
func TestAuthorizationURLReadsEnvPrompt(t *testing.T) {
	const endpoint = "https://example.invalid/authorize"

	t.Setenv("M365_PROMPT", "login")
	viaEnv := AuthorizationURL(endpoint, "cid", "http://127.0.0.1/cb", "st", "ch", "openid")
	viaArg := AuthorizationURLWithPrompt(endpoint, "cid", "http://127.0.0.1/cb", "st", "ch", "openid", "login")
	if viaEnv != viaArg {
		t.Errorf("环境变量与显式传参结果不一致:\n  env=%s\n  arg=%s", viaEnv, viaArg)
	}

	t.Setenv("M365_PROMPT", "none")
	parsed, err := url.Parse(AuthorizationURL(endpoint, "cid", "http://127.0.0.1/cb", "st", "ch", "openid"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Has("prompt") {
		t.Error("M365_PROMPT=none 时不应写入 prompt")
	}
}
