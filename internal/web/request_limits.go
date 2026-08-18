package web

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// requestSizeLimits is recovered from the APK's requestSizeLimits function.
// Zero means disabled; only positive, strictly parsed environment values apply.
func requestSizeLimits() (maxMessages, maxRequestBytes int) {
	parse := func(name string) int {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return 0
		}
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return 0
		}
		return parsed
	}
	return parse("M365_MAX_MESSAGES"), parse("M365_MAX_REQUEST_BYTES")
}

// oversizeReason returns the APK-compatible diagnostic for a configured
// message-count or raw-request-byte limit. An empty result means accepted.
func oversizeReason(messageCount, requestBytes int) string {
	maxMessages, maxBytes := requestSizeLimits()
	if maxMessages > 0 && messageCount > maxMessages {
		return fmt.Sprintf("request carries %d messages, limit is %d (set M365_MAX_MESSAGES to change)", messageCount, maxMessages)
	}
	if maxBytes > 0 && requestBytes > maxBytes {
		return fmt.Sprintf("request body is %d bytes, limit is %d (set M365_MAX_REQUEST_BYTES to change)", requestBytes, maxBytes)
	}
	return ""
}
