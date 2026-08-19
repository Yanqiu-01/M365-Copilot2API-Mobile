package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// adminAccountHealth exposes the routing health view recovered from the APK:
// per-account availability, the email-keyed upstream cooldowns, and the active
// retry/concurrency policy values.
func (s *Server) adminAccountHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cooldowns := map[string]time.Time{}
	if s.upstreamCooldown != nil {
		cooldowns = s.upstreamCooldown.snapshot()
	}
	accounts := make([]map[string]any, 0)
	if s.tokens != nil {
		for _, account := range s.tokens.List() {
			entry := map[string]any{
				"id":        account.ID,
				"email":     account.Email,
				"status":    account.Status,
				"available": s.accountAvailable(account.ID),
			}
			if until, blocked := cooldowns[account.Email]; blocked {
				entry["coolingDown"] = true
				entry["cooldownUntil"] = until
				entry["retryAfterSeconds"] = int(time.Until(until).Seconds())
			}
			accounts = append(accounts, entry)
		}
	}
	jsonOut(w, map[string]any{
		"accounts":           accounts,
		"cooldownCount":      len(cooldowns),
		"retryAttempts":      routerRetryAttempts(),
		"maxConcurrentChats": maxConcurrentChats(),
		"accountConcurrency": s.accountConcurrency.Snapshot(),
	})
}

func (s *Server) adminBenchmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOut(w, map[string]any{
		"run":     s.benchmark.snapshot(),
		"tasks":   benchTaskCatalog(),
		"efforts": advertisedReasoningEfforts,
	})
}

// adminBenchmarkRun validates the APK request shape. The task executor is not
// yet recovered, so this endpoint reports that explicitly instead of returning
// a fabricated run state.
func benchmarkEffort(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "max" {
		return "xhigh", nil
	}
	return normalizeReasoningEffort(value)
}

func (s *Server) adminBenchmarkRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Model  string   `json:"model"`
		Effort string   `json:"effort"`
		Tasks  []string `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		body.Model = "gpt-5.6-reasoning"
	}
	if strings.TrimSpace(body.Effort) == "" {
		body.Effort = "max"
	}
	if _, err := benchmarkEffort(body.Effort); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	known := map[string]bool{}
	for _, task := range benchTaskCatalog() {
		known[task.ID] = true
	}
	for _, id := range body.Tasks {
		if !known[id] {
			http.Error(w, "unknown benchmark task: "+id, http.StatusBadRequest)
			return
		}
	}
	if err := s.startBenchmark(body.Model, body.Effort, body.Tasks); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	jsonOut(w, map[string]any{"state": "running"})
}

func (s *Server) adminBenchmarkStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOut(w, map[string]any{"ok": s.benchmark.stop()})
}
