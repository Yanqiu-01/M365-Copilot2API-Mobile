package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// isRetryableUpstream is the APK's conservative transport classifier. Client
// cancellation, deadline expiry, downstream write failures, and an explicit
// ChatHub completion error must not be retried. The remaining signatures are
// transient connection failures observed before a completed upstream response.
func isRetryableUpstream(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "downstream stream write") || strings.Contains(message, "chathub completion error") {
		return false
	}
	for _, marker := range []string{
		"ws read before completion",
		"unexpected eof",
		"close 1006",
		"close 1011",
		"close 1012",
		"close 1013",
		"connection reset",
		"broken pipe",
		"ws dial",
		"handshake",
		"chat send",
		"eof",
		"i/o timeout",
		"connection refused",
		"software caused connection abort",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// routerRetryAttempts returns total attempts, not additional retries. The APK
// uses M365_UPSTREAM_RETRIES: 0..5 becomes 1..6; invalid or unset defaults to 4.
func routerRetryAttempts() int {
	value := strings.TrimSpace(os.Getenv("M365_UPSTREAM_RETRIES"))
	if value != "" {
		if configured, err := strconv.Atoi(value); err == nil && configured >= 0 && configured <= 5 {
			return configured + 1
		}
	}
	return 4
}

// retryUpstream runs operation up to routerRetryAttempts times. The callback is
// given a 1-based attempt number so recovery callers can select a replacement
// account or a continuation prompt after the first failure.
func retryUpstream(ctx context.Context, stage string, operation func(attempt int) error) error {
	if operation == nil {
		return fmt.Errorf("%s: nil upstream operation", stage)
	}
	attempts := routerRetryAttempts()
	var last error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		last = operation(attempt)
		if last == nil || !isRetryableUpstream(last) || attempt == attempts {
			return last
		}

		// APK uses an attempt-scaled 750ms delay and caps it before sleeping.
		delay := time.Duration(attempt) * 750 * time.Millisecond
		if delay > 12*time.Second {
			delay = 12 * time.Second
		}
		if err := sleepUnlessDone(ctx, delay); err != nil {
			return err
		}
	}
	return last
}

// sleepUnlessDone performs cancellation-aware waiting. A 100ms ticker mirrors
// the APK helper's periodic context check while avoiding a goroutine leak.
func sleepUnlessDone(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
}
