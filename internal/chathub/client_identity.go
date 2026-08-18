package chathub

import (
	"os"
	"strings"
	"sync"
)

// clientIdentity is the five-string plus []string layout returned by the APK's
// identityFor function. The values below were recovered directly from the two
// static Identity objects in libm365.so.
type clientIdentity struct {
	Source         string
	ClientPlatform string
	ClientAppName  string
	ProductThread  string
	UserAgent      string
	ExtraOptions   []string
}

var (
	identityMu     sync.RWMutex
	clientProfile  string
	officeIdentity = clientIdentity{
		Source:         "officeweb",
		ClientPlatform: "mcmcopilot-web",
		ClientAppName:  "Office",
		ProductThread:  "Office",
		UserAgent:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:148.0) Gecko/20100101 Firefox/148.0",
	}
	cliIdentity = clientIdentity{
		Source:         "cli",
		ClientPlatform: "cli",
		ClientAppName:  "CopilotCli",
		ProductThread:  "Developer",
		UserAgent:      "claude-cli/1.0 (external, cli)",
	}
)

// identityFor returns the profile selected by in-process settings or the
// M365_CLIENT_PROFILE environment override. APK accepted cli, agent, claude,
// and claude-cli as aliases for the CLI identity; all other values select the
// Office Web identity.
func identityFor() clientIdentity {
	profile := configuredProfile()
	if fromEnv := strings.ToLower(strings.TrimSpace(os.Getenv("M365_CLIENT_PROFILE"))); fromEnv != "" {
		profile = fromEnv
	}
	switch profile {
	case "cli", "agent", "claude", "claude-cli":
		return cloneIdentity(cliIdentity)
	default:
		return cloneIdentity(officeIdentity)
	}
}

// SetClientProfile changes the in-process profile. It is intentionally kept
// separate from the environment override because the APK gives the latter
// precedence for non-interactive deployments.
func SetClientProfile(profile string) {
	identityMu.Lock()
	clientProfile = strings.ToLower(strings.TrimSpace(profile))
	identityMu.Unlock()
}

func configuredProfile() string {
	identityMu.RLock()
	profile := clientProfile
	identityMu.RUnlock()
	return profile
}

// ActiveIdentitySummary mirrors the APK diagnostic helper and deliberately
// exposes only fields useful for troubleshooting, not the User-Agent.
func ActiveIdentitySummary() map[string]any {
	identity := identityFor()
	return map[string]any{
		"source":         identity.Source,
		"clientAppName":  identity.ClientAppName,
		"clientPlatform": identity.ClientPlatform,
		"productThread":  identity.ProductThread,
		"extraOptions":   append([]string(nil), identity.ExtraOptions...),
	}
}

func cloneIdentity(identity clientIdentity) clientIdentity {
	identity.ExtraOptions = append([]string(nil), identity.ExtraOptions...)
	return identity
}
