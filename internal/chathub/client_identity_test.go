package chathub

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestIdentityForAPKProfiles(t *testing.T) {
	t.Setenv("M365_CLIENT_PROFILE", "")
	SetClientProfile("")
	t.Cleanup(func() { SetClientProfile("") })

	office := identityFor()
	if office.Source != "officeweb" || office.ClientPlatform != "mcmcopilot-web" || office.ClientAppName != "Office" || office.ProductThread != "Office" {
		t.Fatalf("office identity = %#v", office)
	}

	for _, profile := range []string{"cli", "agent", "claude", "claude-cli"} {
		SetClientProfile(profile)
		got := identityFor()
		if got.Source != "cli" || got.ClientPlatform != "cli" || got.ClientAppName != "CopilotCli" || got.ProductThread != "Developer" {
			t.Fatalf("profile %q identity = %#v", profile, got)
		}
	}
}

func TestEnvironmentProfileOverridesInProcessProfile(t *testing.T) {
	SetClientProfile("office")
	t.Cleanup(func() { SetClientProfile("") })
	t.Setenv("M365_CLIENT_PROFILE", "claude-cli")
	if got := identityFor(); got.Source != "cli" || got.UserAgent != "claude-cli/1.0 (external, cli)" {
		t.Fatalf("environment override identity = %#v", got)
	}
}

func TestActiveIdentitySummaryDoesNotExposeUserAgent(t *testing.T) {
	SetClientProfile("cli")
	t.Cleanup(func() { SetClientProfile("") })
	summary := ActiveIdentitySummary()
	if summary["source"] != "cli" || summary["clientAppName"] != "CopilotCli" || summary["clientPlatform"] != "cli" || summary["productThread"] != "Developer" {
		t.Fatalf("summary = %#v", summary)
	}
	if _, ok := summary["userAgent"]; ok {
		t.Fatalf("summary exposed user agent: %#v", summary)
	}
}

func TestChatPayloadUsesIdentity(t *testing.T) {
	SetClientProfile("cli")
	t.Cleanup(func() { SetClientProfile("") })
	frames := strings.Split(chatPayload("hello", "session", "conversation", "request", "magic", true, nil, nil, nil, ""), rs)
	var frame map[string]any
	if err := json.Unmarshal([]byte(frames[0]), &frame); err != nil {
		t.Fatal(err)
	}
	arg := frame["arguments"].([]any)[0].(map[string]any)
	if arg["source"] != "cli" {
		t.Fatalf("source = %#v", arg["source"])
	}
	clientInfo := arg["clientInfo"].(map[string]any)
	if clientInfo["clientPlatform"] != "cli" || clientInfo["clientAppName"] != "CopilotCli" {
		t.Fatalf("clientInfo = %#v", clientInfo)
	}
}

func TestWSURLUsesIdentitySource(t *testing.T) {
	SetClientProfile("cli")
	t.Cleanup(func() { SetClientProfile("") })
	u, err := buildWSURL(Account{AccessToken: "token", OID: "oid", TID: "tid"}, "session", "conversation", "request")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("source"); got != `"cli"` {
		t.Fatalf("source query = %q", got)
	}
	if got := parsed.Query().Get("product"); got != "Developer" {
		t.Fatalf("product query = %q", got)
	}
}
