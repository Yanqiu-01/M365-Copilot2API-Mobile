package web

import "strings"

// reasoningDelta returns the new reasoning text that has not already been
// delivered in previous. ChatHub may emit either a growing snapshot or a
// replacement snapshot; only an extension of the previous snapshot is a delta.
//
// Recovered from the APK's Go pclntab entry and AArch64 implementation at
// 0x342820–0x3429d0.
func reasoningDelta(previous, current string) string {
	if current == "" {
		return ""
	}
	if previous == "" {
		return current
	}
	if previous == current {
		return ""
	}

	if len(previous) >= len(current) {
		if strings.HasPrefix(previous, current) {
			return ""
		}
		return current
	}

	if strings.HasPrefix(current, previous) {
		return current[len(previous):]
	}
	return current
}
