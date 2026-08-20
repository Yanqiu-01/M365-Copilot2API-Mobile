package chathub

import (
	"strings"
	"testing"
)

// 抓帧脱敏必须覆盖三种形态：凭据作为字段名、凭据嵌在字符串值里、
// 以及 cookie 这类此前遗漏的头部字段。
//
// 审计实测：原实现只按 JSON key 名删除，5 种形态里 4 种原样泄漏 ——
// Cookie、Set-Cookie、值内嵌的 Bearer 令牌、裸 JWT。而抓帧内容会经
// /api/admin/debug/wire 读出（该端点对本机免密），必须在值一级也擦。
func TestWireRedactionCoversCredentialShapes(t *testing.T) {
	cases := []struct {
		label   string
		payload string
		secrets []string
	}{
		{
			"嵌套 access_token",
			`{"a":{"b":{"access_token":"secret-xyz"}}}`,
			[]string{"secret-xyz"},
		},
		{
			"Cookie 头",
			`{"headers":{"Cookie":"m365_admin=abcdef123456"}}`,
			[]string{"abcdef123456"},
		},
		{
			"Set-Cookie 头",
			`{"headers":{"Set-Cookie":"session=deadbeefcafe; HttpOnly"}}`,
			[]string{"deadbeefcafe"},
		},
		{
			"值内嵌 Bearer",
			`{"text":"curl -H 'Authorization: Bearer eyJabcDEFghi.payload.signature' https://x"}`,
			[]string{"eyJabcDEFghi"},
		},
		{
			"裸 JWT",
			`{"note":"token is eyJhbGciOiJIUzI1NiJ9.cGF5bG9hZA.c2ln"}`,
			[]string{"eyJhbGciOiJIUzI1NiJ9"},
		},
		{
			"数组内的凭据字符串",
			`{"lines":["Authorization: Bearer eyJqqqwwweee.aaa.bbb","ok"]}`,
			[]string{"eyJqqqwwweee"},
		},
		{
			"查询串形式",
			`{"url":"https://x/y?access_token=AbCdEf0123456789&mode=chat"}`,
			[]string{"AbCdEf0123456789"},
		},
		{
			"x-api-key 字段",
			`{"headers":{"x-api-key":"sk-abcdefghijklmnop"}}`,
			[]string{"sk-abcdefghijklmnop"},
		},
	}

	for _, c := range cases {
		got := sanitizeWirePayload(c.payload)
		for _, secret := range c.secrets {
			if strings.Contains(got, secret) {
				t.Errorf("[%s] 凭据未脱敏 %q\n  输出: %s", c.label, secret, got)
			}
		}
	}
}

// 脱敏不得把正常诊断信息一起抹掉，否则抓帧失去价值。
func TestWireRedactionKeepsDiagnosticContext(t *testing.T) {
	payload := `{"type":1,"target":"chat","arguments":[{"model":"gpt-5.6-reasoning","text":"hello world"}]}`
	got := sanitizeWirePayload(payload)
	for _, keep := range []string{"chat", "gpt-5.6-reasoning", "hello world"} {
		if !strings.Contains(got, keep) {
			t.Errorf("诊断信息被误删 %q\n  输出: %s", keep, got)
		}
	}
}

// 短小的普通赋值不应被当作凭据（避免把 id=3 之类抹成 [redacted]）。
func TestWireRedactionAvoidsFalsePositives(t *testing.T) {
	payload := `{"note":"retry=3 stage=router mode=auto","seq":"id=42"}`
	got := sanitizeWirePayload(payload)
	for _, keep := range []string{"retry=3", "stage=router", "id=42"} {
		if !strings.Contains(got, keep) {
			t.Errorf("正常字段被误擦 %q\n  输出: %s", keep, got)
		}
	}
}
