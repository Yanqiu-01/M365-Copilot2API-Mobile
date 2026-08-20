package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
)
// errNoAccounts marks "no usable account" conditions. The original APK reports
// these with the account's own message and a 4xx status (never as a generic
// 502 upstream failure), so clients can tell "you must log in" apart from
// "the upstream call broke".
var errNoAccounts = errors.New("no accounts; login first")

// isAccountResolveFailure reports whether err is a local account-selection
// failure rather than a failed upstream call.
func isAccountResolveFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errNoAccounts) {
		return true
	}
	var he *UpstreamHTTPError
	if errors.As(err, &he) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no accounts; login first") ||
		strings.Contains(msg, "no accounts enabled for scheduling")
}

// writeAccountResolveError renders an account-selection failure using the
// original APK contract: the real reason, verbatim, with the caller's own
// error type.
func writeAccountResolveError(w http.ResponseWriter, err error, errType string) {
	writeOpenAIError(w, http.StatusBadRequest, errType, err.Error())
}


// upstreamError keeps transport details, including URLs and credentials, out
// of client-visible responses while retaining a server-side diagnostic.
func upstreamError(err error) string {
	if err == nil {
		return "upstream request failed"
	}
	log.Printf("upstream request failed: %v", err)
	return "upstream request failed"
}

// upstreamStatus maps a failed upstream call to the client-visible HTTP status:
// rate limits stay 429 (with Retry-After when known), auth failures become 401,
// everything else is 502. Unknown upstream failures must never leak internals.
func upstreamStatus(err error) int {
	if IsRateLimited(err) {
		return http.StatusTooManyRequests
	}
	if IsAuthFailure(err) {
		return http.StatusUnauthorized
	}
	return http.StatusBadGateway
}

// writeUpstreamError renders a failed upstream call as an HTTP response,
// surfacing the Retry-After hint for rate limits so clients can back off.
func writeUpstreamError(w http.ResponseWriter, err error) {
	if retry := RetryAfterSeconds(err); retry > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retry))
	}
	status := upstreamStatus(err)
	if status == http.StatusTooManyRequests {
		if w.Header().Get("Retry-After") == "" {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(rateLimitCooldown.Seconds())))
		}
		writeOpenAIError(w, status, "rate_limit_error", "upstream is rate limiting; try again shortly")
		return
	}
	if IsEmptyCompletion(err) {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "upstream returned empty completion; the requested model may be unavailable for this tenant")
		return
	}
	writeOpenAIError(w, status, "upstream_error", upstreamError(err))
}
