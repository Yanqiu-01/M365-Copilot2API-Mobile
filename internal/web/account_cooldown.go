package web

import (
	"strings"
	"sync"
	"time"
)

// accountCooldown is the APK's short-lived, email-keyed upstream cooldown.
// It is intentionally separate from accountHealth: accountHealth tracks
// rate-limit/auth outcomes by account ID, while this structure backs off after
// early retryable WebSocket closes before an upstream response is complete.
type accountCooldown struct {
	mu       sync.Mutex
	until    map[string]time.Time
	attempts map[string]int
}

const (
	accountCooldownStep = 45 * time.Second
	accountCooldownMax  = 10 * time.Minute
)

func newAccountCooldown() *accountCooldown {
	return &accountCooldown{
		until:    map[string]time.Time{},
		attempts: map[string]int{},
	}
}

// penalise follows the APK formula: 45 seconds times consecutive attempts,
// capped at ten minutes.
func (c *accountCooldown) penalise(email string) {
	email = strings.TrimSpace(email)
	if c == nil || email == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.until == nil {
		c.until = map[string]time.Time{}
	}
	if c.attempts == nil {
		c.attempts = map[string]int{}
	}
	c.attempts[email]++
	cooldown := time.Duration(c.attempts[email]) * accountCooldownStep
	if cooldown > accountCooldownMax {
		cooldown = accountCooldownMax
	}
	c.until[email] = time.Now().Add(cooldown)
}

func (c *accountCooldown) clear(email string) {
	email = strings.TrimSpace(email)
	if c == nil || email == "" {
		return
	}
	c.mu.Lock()
	delete(c.until, email)
	delete(c.attempts, email)
	c.mu.Unlock()
}

func (c *accountCooldown) blocked(email string) bool {
	email = strings.TrimSpace(email)
	if c == nil || email == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until, ok := c.until[email]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(c.until, email)
		return false
	}
	return true
}

// snapshot returns only still-active cooldowns, matching the APK's expiry
// filtering rather than exposing stale entries.
func (c *accountCooldown) snapshot() map[string]time.Time {
	if c == nil {
		return map[string]time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	out := make(map[string]time.Time, len(c.until))
	for email, until := range c.until {
		if now.After(until) {
			delete(c.until, email)
			continue
		}
		out[email] = until
	}
	return out
}

// isEarlyUpstreamClose identifies retryable transport failures emitted before
// SignalR completion. The APK first checks its retryable-error classifier, then
// matches these close/read signatures.
func isEarlyUpstreamClose(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if !strings.Contains(message, "ws read before completion") &&
		!strings.Contains(message, "unexpected") &&
		!strings.Contains(message, "close 1006") &&
		!strings.Contains(message, "close 1011") &&
		!strings.Contains(message, "close 1012") &&
		!strings.Contains(message, "connection ") &&
		!strings.Contains(message, "broken pipe") &&
		!strings.Contains(message, "software caused") {
		return false
	}
	return strings.Contains(message, "ws") ||
		strings.Contains(message, "close") ||
		strings.Contains(message, "connection") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "software caused")
}
