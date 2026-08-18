package web

import "strings"

func toolPlanningMode(raw string) string {
	if strings.EqualFold(strings.TrimSpace(raw), "native") {
		return "native"
	}
	return "router"
}
