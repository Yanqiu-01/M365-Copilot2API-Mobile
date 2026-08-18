package web

import (
	"os"
	"strconv"
	"strings"
)

// routerMaxGroups controls the number of user-turn groups shown to the tool
// routing model. APK behavior: M365_ROUTER_MAX_MESSAGES defaults to 40; zero
// disables this layer; negative/invalid values fall back to 40.
func routerMaxGroups() int {
	value := strings.TrimSpace(os.Getenv("M365_ROUTER_MAX_MESSAGES"))
	if value == "" {
		return 40
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 40
	}
	return parsed
}

// trimHistoryForRouter retains instruction messages and the newest complete
// user-turn groups for the tool-routing prompt. It uses the same grouping as
// context budgeting but deliberately has no token estimator: this is an
// independent upper bound on router history breadth.
func trimHistoryForRouter(messages []oaiMsg) []oaiMsg {
	maxGroups := routerMaxGroups()
	if maxGroups == 0 {
		return messages
	}
	instructions, groups := messageGroups(messages, "")
	if len(groups) <= maxGroups {
		return messages
	}
	out := make([]oaiMsg, 0, len(instructions)+len(messages))
	out = append(out, instructions...)
	for _, group := range groups[len(groups)-maxGroups:] {
		out = append(out, group.messages...)
	}
	return out
}

// routerPromptMessages applies the APK's router-only history limit before
// flattening role boundaries. Attachments belong to the active request and are
// passed separately to ChatHub, so they are not re-collected here.
func routerPromptMessages(messages []oaiMsg) string {
	prompt, _ := flattenPromptMessages(trimHistoryForRouter(messages), nil)
	return strings.TrimSpace(prompt)
}
