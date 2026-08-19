package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func Verifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func Challenge(v string) string {
	h := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// AuthorizationURL 读取 M365_PROMPT 后委托给 AuthorizationURLWithPrompt。
//
// APK 证据（tools/apktool，internal/auth/pkce.go:34-41，288 字节）：
// 调用图仅 os.Getenv 与 AuthorizationURLWithPrompt，是个薄壳。
// M365_PROMPT 的 rodata 地址 0x49b523，唯一引用者即本函数 +0x0048。
// Java 侧（GatewayService.smali）注入该变量，默认值为 "login"。
func AuthorizationURL(endpoint, clientID, redirect, state, challenge, scope string) string {
	return AuthorizationURLWithPrompt(endpoint, clientID, redirect, state, challenge, scope,
		os.Getenv("M365_PROMPT"))
}

// AuthorizationURLWithPrompt 构造 PKCE 授权地址。
//
// APK 证据（internal/auth/pkce.go:44-58，1696 字节）：
// 查询参数依次为 client_id(9)、response_type(13)、redirect_uri、
// response_mode(13)、scope(5)、state(5)、code_challenge、
// code_challenge_method(21)，末尾以 "%s?%s"(5) 拼接。
// +0x04ec CMP #4 配 +0x04fc MOVZ #28526 判定 prompt == "none"：
// 该值不写入查询串（Microsoft 身份平台对 prompt=none 有特殊语义，
// 与 PKCE 交互式登录冲突），其余非空值原样透传。
func AuthorizationURLWithPrompt(endpoint, clientID, redirect, state, challenge, scope, prompt string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirect)
	q.Set("response_mode", "query")
	q.Set("scope", scope)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if prompt = strings.TrimSpace(prompt); prompt != "" && prompt != "none" {
		q.Set("prompt", prompt)
	}
	return fmt.Sprintf("%s?%s", endpoint, q.Encode())
}
