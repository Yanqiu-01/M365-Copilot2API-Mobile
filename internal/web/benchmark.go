package web

import (
	"fmt"
	"time"
)

// gradeBenchTask wraps the task-specific grader functions and normalizes their
// (passed, total, failures) return into the front-end benchTaskResult. The APK
// uses this adapter so each grader remains a pure function without knowledge of
// the runtime state or HTTP serialization.
func gradeBenchTask(task benchTask, files map[string]string) benchTaskResult {
	var passed, total int
	var failures []string

	switch task.Kind {
	case "reasoning/schedule":
		passed, total, failures = gradeShift(files)
	case "reasoning/sales":
		passed, total, failures = gradeSales(files)
	case "reasoning/ledger":
		passed, total, failures = gradeLedger(files)
	case "reasoning/route":
		passed, total, failures = gradeRoute(files)
	case "programming/inventory":
		passed, total, failures = gradeInventory(files)
	default:
		return benchTaskResult{
			Verdict:  "error",
			Score:    0,
			Details:  fmt.Sprintf("unknown task kind: %s", task.Kind),
			Failures: nil,
		}
	}

	score := 0
	if total > 0 {
		score = (passed * 100) / total
	}

	verdict := "fail"
	if passed == total {
		verdict = "pass"
	} else if passed > 0 {
		verdict = "partial"
	}

	details := fmt.Sprintf("%d/%d checks passed", passed, total)
	if len(failures) > 0 {
		summary := failures[0]
		if len(failures) > 1 {
			summary = fmt.Sprintf("%s (+%d more)", summary, len(failures)-1)
		}
		details = fmt.Sprintf("%s — %s", details, summary)
	}

	return benchTaskResult{
		Verdict:  verdict,
		Score:    score,
		Details:  details,
		Failures: failures,
	}
}

// runBenchTask executes a single benchmark task by sending the prompt to the
// model and collecting the response. It returns the raw model output before
// grading; the HTTP handler invokes gradeBenchTask separately.
//
// NOTE: This function requires integration with the router/upstream layer which
// defines conversationContext, message, upstreamRequest, and sendUpstream. The
// APK implementation routes through the existing M365 Copilot handler.
func runBenchTask(task benchTask, model string) (map[string]string, error) {
	// Placeholder implementation - the full version requires:
	// 1. Building a message with task.Prompt
	// 2. Sending to upstream via sendUpstream or equivalent router
	// 3. Extracting the response content
	// 4. Parsing artifacts from the response
	
	// For now, return an error indicating this needs router integration
	return nil, fmt.Errorf("runBenchTask requires router integration (not yet connected)")
}

// extractArtifacts parses the model response for fenced code blocks and returns
// a map of filename to content. The APK recognizes both ```language and
// ```filename patterns.
func extractArtifacts(content string) map[string]string {
	files := make(map[string]string)
	
	// Simple fence extraction: split by ``` and process pairs
	parts := splitByFence(content)
	for i := 0; i+1 < len(parts); i += 2 {
		header := parts[i]
		body := parts[i+1]
		
		// Try to extract filename from header line
		filename := parseFilename(header)
		if filename == "" {
			// Use language as extension
			if lang := parseLanguage(header); lang != "" {
				switch lang {
				case "python", "py":
					filename = "solution.py"
				case "json":
					filename = "output.json"
				case "javascript", "js":
					filename = "solution.js"
				default:
					filename = fmt.Sprintf("artifact.%s", lang)
				}
			} else {
				continue
			}
		}
		
		files[filename] = body
	}
	
	return files
}

// splitByFence splits content by ``` markers, returning alternating text and
// code sections. The first element is text before the first fence.
func splitByFence(content string) []string {
	var parts []string
	remaining := content
	
	for {
		start := findFenceStart(remaining)
		if start < 0 {
			parts = append(parts, remaining)
			break
		}
		
		// Add text before fence
		parts = append(parts, remaining[:start])
		
		// Find end of first line (the fence header)
		lineEnd := start
		for lineEnd < len(remaining) && remaining[lineEnd] != '\n' {
			lineEnd++
		}
		if lineEnd < len(remaining) {
			lineEnd++ // include the newline
		}
		
		header := remaining[start:lineEnd]
		remaining = remaining[lineEnd:]
		
		// Find closing fence
		end := findFenceEnd(remaining)
		if end < 0 {
			// No closing fence, treat rest as code
			parts = append(parts, header)
			parts = append(parts, remaining)
			break
		}
		
		parts = append(parts, header)
		parts = append(parts, remaining[:end])
		
		// Skip the closing fence line
		remaining = remaining[end:]
		for len(remaining) > 0 && (remaining[0] == '`' || remaining[0] == '\n' || remaining[0] == '\r') {
			remaining = remaining[1:]
		}
	}
	
	return parts
}

func findFenceStart(s string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '`' && s[i+1] == '`' && s[i+2] == '`' {
			return i
		}
	}
	return -1
}

func findFenceEnd(s string) int {
	for i := 0; i+2 < len(s); i++ {
		if s[i] == '`' && s[i+1] == '`' && s[i+2] == '`' {
			return i
		}
	}
	return -1
}

func parseFilename(header string) string {
	// Remove leading ``` and whitespace
	header = trimPrefix(header, "```")
	header = trimSpace(header)
	
	// Check common patterns: ```filename.ext or ```language:filename.ext
	if idx := indexByte(header, ':'); idx >= 0 {
		header = header[idx+1:]
		header = trimSpace(header)
	}
	
	// If header looks like a filename (has . and no spaces before it), use it
	dotIdx := indexByte(header, '.')
	if dotIdx > 0 && dotIdx < len(header)-1 {
		// Extract up to first whitespace or newline
		for i := 0; i < len(header); i++ {
			if header[i] == ' ' || header[i] == '\t' || header[i] == '\n' || header[i] == '\r' {
				return header[:i]
			}
		}
		return trimSpace(header)
	}
	
	return ""
}

func parseLanguage(header string) string {
	header = trimPrefix(header, "```")
	header = trimSpace(header)
	
	// Language is first word before : or whitespace
	for i := 0; i < len(header); i++ {
		if header[i] == ' ' || header[i] == '\t' || header[i] == '\n' || header[i] == ':' {
			return header[:i]
		}
	}
	return header
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// startBenchmark initializes a new benchmark session for the given account and
// returns the session ID. The APK stores session state in memory with TTL-based
// cleanup.
func startBenchmark(account string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	// Generate session ID
	sessionID := fmt.Sprintf("bench_%d_%s", time.Now().Unix(), account[:min(8, len(account))])

	// Initialize session state
	session := &benchSession{
		ID:        sessionID,
		Account:   account,
		StartedAt: time.Now(),
		Tasks:     make(map[string]benchTaskState),
	}

	// Register all catalog tasks
	for _, task := range benchTaskCatalog() {
		session.Tasks[task.ID] = benchTaskState{
			Status:    "pending",
			StartedAt: time.Time{},
		}
	}

	benchSessions[sessionID] = session
	return sessionID, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Global session store is defined in benchmark_test.go
// Type definitions for conversationContext, message, upstreamRequest are in router.go
