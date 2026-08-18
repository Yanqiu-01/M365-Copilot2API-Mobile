package web

import (
	"fmt"
	"testing"
	"time"
)

func TestAccountCooldownAPKBackoffAndClear(t *testing.T) {
	cooldown := newAccountCooldown()
	before := time.Now()
	cooldown.penalise(" user@example.com ")
	first := cooldown.snapshot()["user@example.com"]
	if first.Before(before.Add(44*time.Second)) || first.After(time.Now().Add(46*time.Second)) {
		t.Fatalf("first cooldown=%s, want about 45 seconds", first.Sub(before))
	}
	if !cooldown.blocked("user@example.com") {
		t.Fatal("first penalty should block account")
	}

	cooldown.penalise("user@example.com")
	second := cooldown.snapshot()["user@example.com"]
	if second.Before(time.Now().Add(89*time.Second)) || second.After(time.Now().Add(91*time.Second)) {
		t.Fatalf("second cooldown should be about 90 seconds, got %s", time.Until(second))
	}
	cooldown.clear("user@example.com")
	if cooldown.blocked("user@example.com") || len(cooldown.snapshot()) != 0 {
		t.Fatalf("clear did not reset cooldown: %#v", cooldown.snapshot())
	}
}

func TestAccountCooldownCapsAndExpires(t *testing.T) {
	cooldown := newAccountCooldown()
	for i := 0; i < 20; i++ {
		cooldown.penalise("user@example.com")
	}
	until := cooldown.snapshot()["user@example.com"]
	if remaining := time.Until(until); remaining < 9*time.Minute+59*time.Second || remaining > 10*time.Minute+time.Second {
		t.Fatalf("capped cooldown=%s", remaining)
	}

	cooldown.mu.Lock()
	cooldown.until["expired@example.com"] = time.Now().Add(-time.Second)
	cooldown.attempts["expired@example.com"] = 1
	cooldown.mu.Unlock()
	if cooldown.blocked("expired@example.com") {
		t.Fatal("expired cooldown must not block")
	}
	if _, ok := cooldown.snapshot()["expired@example.com"]; ok {
		t.Fatal("expired cooldown must not appear in snapshot")
	}
}

func TestEarlyUpstreamCloseAPKSignatures(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("ws read before completion: unexpected EOF"),
		fmt.Errorf("websocket: close 1006"),
		fmt.Errorf("connection reset by peer"),
		fmt.Errorf("write: broken pipe"),
	} {
		if !isEarlyUpstreamClose(err) {
			t.Fatalf("expected early upstream close for %v", err)
		}
	}
	for _, err := range []error{nil, fmt.Errorf("bad request"), fmt.Errorf("tool validation failed")} {
		if isEarlyUpstreamClose(err) {
			t.Fatalf("unexpected early upstream close for %v", err)
		}
	}
}

func TestEmailCooldownParticipatesInAccountAvailability(t *testing.T) {
	store := testAccountFiles(t)
	s := &Server{
		tokens:             store,
		accountPool:        newAccountHealth(),
		upstreamCooldown:   newAccountCooldown(),
		accountConcurrency: newAccountConcurrency(),
	}
	s.recordUpstreamCooldown("u-1", fmt.Errorf("ws read before completion: unexpected EOF"))
	if s.accountAvailable("u-1") {
		t.Fatal("email cooldown must make account unavailable")
	}
	if !s.accountAvailable("u-2") {
		t.Fatal("unrelated account must remain available")
	}
	s.recordUpstreamCooldown("u-1", nil)
	if !s.accountAvailable("u-1") {
		t.Fatal("successful request must clear email cooldown")
	}
}
