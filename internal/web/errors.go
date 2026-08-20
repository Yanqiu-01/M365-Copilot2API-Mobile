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

// classifyUpstream 把传输层失败翻译成一句可操作的诊断。
//
// APK 证据：internal/web/errors.go 的函数表含 classifyUpstream 与
// upstreamStageError；rodata 中并列存在五句诊断（各 1 次）：
//
//	upstream handshake failed / upstream rejected the request /
//	upstream closed the connection early / upstream stalled, read timed out /
//	upstream stopped responding mid-stream
//
// 判定关键词同样存在于二进制：bad handshake、abnormal closure、close 1006、
// i/o timeout、use of closed network connection、connection reset by peer、
// unexpected EOF。
//
// 恢复版此前缺失该函数，任何上游失败都被压成无信息的
// "upstream request failed"，用户在评测与对话里只能看到一句
// 502 upstream request failed，无法区分握手被拒、读超时还是中途断流。
func classifyUpstream(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	contains := func(needles ...string) bool {
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("handshake send", "handshake recv", "bad handshake", "ws dial"):
		return "upstream handshake failed"
	case contains("403", "401", "rejected", "forbidden", "unauthorized"):
		return "upstream rejected the request"
	case contains("abnormal closure", "close 1006", "use of closed network connection",
		"connection reset by peer", "broken pipe", "unexpected eof"):
		return "upstream closed the connection early"
	case contains("i/o timeout", "deadline exceeded", "timeout"):
		return "upstream stalled, read timed out"
	case contains("ws read before completion", "before completion", "mid-stream"):
		return "upstream stopped responding mid-stream"
	}
	return ""
}

// upstreamStageError 给出带阶段标注的客户端消息。
// APK rodata: "upstream request failed (" 与 "stage=router_error err=%v"。
func upstreamStageError(stage string, err error) string {
	if stage == "" {
		return upstreamError(err)
	}
	if detail := classifyUpstream(err); detail != "" {
		log.Printf("upstream request failed (%s): %v", stage, err)
		return detail + " (" + stage + ")"
	}
	log.Printf("upstream request failed (%s): %v", stage, err)
	return "upstream request failed (" + stage + ")"
}

// upstreamError keeps transport details, including URLs and credentials, out
// of client-visible responses while retaining a server-side diagnostic. It now
// surfaces the classified reason so clients can act on it; the raw error (which
// may embed URLs or tokens) still only reaches the server log.
func upstreamError(err error) string {
	if err == nil {
		return "upstream request failed"
	}
	log.Printf("upstream request failed: %v", err)
	if detail := classifyUpstream(err); detail != "" {
		return detail
	}
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
