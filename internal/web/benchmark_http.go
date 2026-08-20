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
	// 字段集对齐原版 /api/admin/account-health：coolingDown 计数与
	// 冷却窗口毫秒值，不暴露上游的 accountConcurrency/maxConcurrentChats。
	jsonOut(w, map[string]any{
		"accounts":       accounts,
		"coolingDown":    len(cooldowns),
		"cooldownBaseMs": accountCooldownStep.Milliseconds(),
		"cooldownMaxMs":  accountCooldownMax.Milliseconds(),
		"retryAttempts":  routerRetryAttempts(),
	})
}

var benchmarkReasoningEfforts = []string{"low", "medium", "high", "xhigh", "max"}

func (s *Server) adminBenchmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 原 APK 实测还返回 pythonOk 与 runtime：评分器是纯 Go 实现，
	// 不依赖外部 Python，因此固定为 true / "pure-go"。
	jsonOut(w, map[string]any{
		"run":      s.benchmark.snapshot(),
		"tasks":    benchTaskCatalog(),
		"efforts":  benchmarkReasoningEfforts,
		"pythonOk": true,
		"runtime":  "pure-go",
	})
}

// benchmarkEffort keeps the user-visible effort token intact. In particular,
// "max" is a first-class upstream/Codex tier, not an alias to "xhigh"; turning
// it into a different value made the UI, run log, and actual request disagree.
func benchmarkEffort(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "max", nil
	}
	return normalizeReasoningEffort(value)
}

// benchmarkDefaultModel is kept pure so request handlers and tests do not
// need to mutate the process-wide settings singleton merely to select a route.
// The configured gpt-5.6-* aliases depend on a tenant-specific tone. The APK
// benchmark defaults to the stable built-in reasoning route instead.
func benchmarkDefaultModel(mappings []modelMapping) string {
	for _, model := range gatewayModels {
		if model.ID == "gpt-5.6-reasoning" {
			return model.ID
		}
	}
	for _, mapping := range mappings {
		if model := strings.TrimSpace(mapping.PublicModel); model != "" && strings.TrimSpace(mapping.UpstreamTone) != "" {
			return model
		}
	}
	return "gpt-5.6-reasoning"
}

func defaultBenchmarkModel() string {
	return benchmarkDefaultModel(currentSettings().ModelMappings)
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
		body.Model = defaultBenchmarkModel()
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
