package web

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRouterRetryAttemptsAPKConfig(t *testing.T) {
	for _, test := range []struct {
		value string
		want  int
	}{
		{"", 4}, {"bad", 4}, {"-1", 4}, {"6", 4}, {"0", 1}, {"1", 2}, {"5", 6},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("M365_UPSTREAM_RETRIES", test.value)
			if got := routerRetryAttempts(); got != test.want {
				t.Fatalf("routerRetryAttempts()=%d want %d", got, test.want)
			}
		})
	}
}

func TestIsRetryableUpstreamAPKClassifier(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("ws read before completion: unexpected EOF"),
		fmt.Errorf("websocket: close 1006"),
		fmt.Errorf("connection reset by peer"),
		fmt.Errorf("write: broken pipe"),
		fmt.Errorf("i/o timeout"),
	} {
		if !isRetryableUpstream(err) {
			t.Fatalf("expected retryable: %v", err)
		}
	}
	for _, err := range []error{
		nil,
		context.Canceled,
		context.DeadlineExceeded,
		fmt.Errorf("downstream stream write: client disconnected"),
		fmt.Errorf("chathub completion error: rejected"),
		fmt.Errorf("invalid tool arguments"),
	} {
		if isRetryableUpstream(err) {
			t.Fatalf("unexpected retryable: %v", err)
		}
	}
}

func TestRetryUpstreamRetriesTransientFailures(t *testing.T) {
	t.Setenv("M365_UPSTREAM_RETRIES", "2") // three total attempts
	attempts := 0
	err := retryUpstream(context.Background(), "test", func(attempt int) error {
		attempts++
		if attempt < 3 {
			return errors.New("ws read before completion: unexpected EOF")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestRetryUpstreamStopsForPermanentFailure(t *testing.T) {
	t.Setenv("M365_UPSTREAM_RETRIES", "5")
	attempts := 0
	want := errors.New("invalid request")
	err := retryUpstream(context.Background(), "test", func(int) error {
		attempts++
		return want
	})
	if !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d", err, attempts)
	}
}

func TestSleepUnlessDoneHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := sleepUnlessDone(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled sleep took %s", elapsed)
	}
}
