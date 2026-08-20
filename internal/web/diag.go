package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultDiagMaxBytes int64 = 4 << 20

var (
	diagOnce sync.Once
	diagFile *os.File
	diagMu   sync.Mutex
	inflight sync.Map // map[string]inflightRequest

	chatSlotsMu sync.Mutex
	chatSlots   chan struct{}
	chatSlotN   int
	// 原版 /api/live 的 chat 对象含 peak/rejected/total 累计量，
	// 只看 channel 长度无法还原，需要独立计数。
	chatSlotPeak     int
	chatSlotRejected uint64
	chatSlotTotal    uint64
)

type inflightRequest struct {
	ID      string    `json:"id"`
	Path    string    `json:"path"`
	Method  string    `json:"method"`
	Started time.Time `json:"started"`
	Stage   string    `json:"stage,omitempty"`
}

// diagPath is recovered from the APK's fixed path components.
func diagPath() string {
	if dir := strings.TrimSpace(os.Getenv("M365_DATA_DIR")); dir != "" {
		return filepath.Join(dir, "server-stages.log")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "m365-copilot2apid", "server-stages.log")
}

func diagEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("M365_STAGE_LOG"))) {
	case "", "0", "no", "off", "false":
		return false
	default:
		return true
	}
}

func diagWriter() *os.File {
	if !diagEnabled() {
		return nil
	}
	diagOnce.Do(func() {
		path := diagPath()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			diagFile = file
		}
	})
	return diagFile
}

func diagMaxBytes() int64 {
	value := strings.TrimSpace(os.Getenv("M365_STAGE_LOG_MAX_BYTES"))
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
		return parsed
	}
	return defaultDiagMaxBytes
}

// stage writes a bounded JSONL event. Fields are diagnostics only; callers
// must not pass tokens, authorization headers, or raw request bodies.
func stage(requestID, name string, fields map[string]any) {
	entry := map[string]any{
		"at":    time.Now().UTC().Format(time.RFC3339Nano),
		"id":    requestID,
		"stage": name,
	}
	if value, ok := inflight.Load(requestID); ok {
		if request, ok := value.(inflightRequest); ok {
			entry["elapsed_ms"] = time.Since(request.Started).Milliseconds()
		}
	}
	for key, value := range fields {
		entry[key] = value
	}
	if writer := diagWriter(); writer != nil {
		line, err := json.Marshal(entry)
		if err == nil {
			diagMu.Lock()
			if info, statErr := writer.Stat(); statErr == nil && info.Size()+int64(len(line)+1) > diagMaxBytes() {
				_ = writer.Truncate(0)
				_, _ = writer.Seek(0, 0)
			}
			_, _ = writer.Write(append(line, '\n'))
			diagMu.Unlock()
		}
	}
	if value, ok := inflight.Load(requestID); ok {
		request := value.(inflightRequest)
		request.Stage = name
		inflight.Store(requestID, request)
	}
}

func beginRequest(requestID string, request *http.Request) {
	if requestID == "" || request == nil {
		return
	}
	inflight.Store(requestID, inflightRequest{ID: requestID, Path: request.URL.Path, Method: request.Method, Started: time.Now().UTC(), Stage: "http_start"})
	stage(requestID, "http_start", map[string]any{"path": request.URL.Path, "method": request.Method})
}

func endRequest(requestID string, err error) {
	if requestID == "" {
		return
	}
	fields := map[string]any{}
	if err != nil {
		fields["error"] = sanitizeDiagnosticError(err.Error())
	}
	stage(requestID, "http_end", fields)
	inflight.Delete(requestID)
}

func sanitizeDiagnosticError(value string) string {
	for _, marker := range []string{"Bearer ", "access_token=", "accessToken=", "Authorization:"} {
		if index := strings.Index(value, marker); index >= 0 {
			end := index + len(marker)
			for end < len(value) && !strings.ContainsRune(" \t\n&\"',;", rune(value[end])) {
				end++
			}
			value = value[:index] + marker + "[redacted]" + value[end:]
		}
	}
	return value
}

func inflightSnapshot() []inflightRequest {
	out := make([]inflightRequest, 0)
	inflight.Range(func(_, value any) bool {
		if request, ok := value.(inflightRequest); ok {
			out = append(out, request)
		}
		return true
	})
	return out
}

func liveness() map[string]any {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	// 字段集对齐原版：status "alive"、chat 计数对象、numGC/sysBytes/
	// uptimeSeconds/stageLogPath；不使用上游的 chatSlots/inflight_count/now。
	return map[string]any{
		"status":         "alive",
		"chat":           chatSlotStats(),
		"inflight":       inflightSnapshot(),
		"heapAllocBytes": memory.HeapAlloc,
		"sysBytes":       memory.Sys,
		"numGC":          memory.NumGC,
		"goroutines":     runtime.NumGoroutine(),
		"uptimeSeconds":  int(time.Since(startedAt).Seconds()),
		"stageLogPath":   diagPath(),
	}
}

func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOut(w, liveness())
}

func (s *Server) handleStageLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 原版返回 {lines, path}，日志未创建时附 note；
	// 不返回上游的 enabled/max_bytes/inflight/log。
	payload := map[string]any{
		"path":  diagPath(),
		"lines": []string{},
	}
	if data, err := os.ReadFile(diagPath()); err == nil {
		lines := []string{}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
		payload["lines"] = lines
	} else {
		payload["note"] = "stage log not created yet"
	}
	jsonOut(w, payload)
}

// maxConcurrentChats is the APK global (not per-account) concurrency limit.
// M365_MAX_CONCURRENT_CHATS accepts 1..64 and defaults to four.
func maxConcurrentChats() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("M365_MAX_CONCURRENT_CHATS"))); err == nil && value >= 1 && value <= 64 {
		return value
	}
	return 4
}

func currentChatSlots() chan struct{} {
	limit := maxConcurrentChats()
	chatSlotsMu.Lock()
	defer chatSlotsMu.Unlock()
	if chatSlots == nil || chatSlotN != limit {
		chatSlots = make(chan struct{}, limit)
		chatSlotN = limit
	}
	return chatSlots
}

func acquireChatSlot(ctx context.Context) (func(), error) {
	slots := currentChatSlots()
	select {
	case slots <- struct{}{}:
		chatSlotsMu.Lock()
		chatSlotTotal++
		if n := len(slots); n > chatSlotPeak {
			chatSlotPeak = n
		}
		chatSlotsMu.Unlock()
		var once sync.Once
		return func() {
			once.Do(func() {
				select {
				case <-slots:
				default:
				}
			})
		}, nil
	case <-ctx.Done():
		chatSlotsMu.Lock()
		chatSlotRejected++
		chatSlotsMu.Unlock()
		return nil, ctx.Err()
	}
}

func releaseChatSlot() {
	slots := currentChatSlots()
	select {
	case <-slots:
	default:
	}
}

func chatSlotInflight() int {
	return len(currentChatSlots())
}

// chatSlotStats 复刻原版 /api/live 的 chat 对象。
func chatSlotStats() map[string]any {
	active := len(currentChatSlots())
	chatSlotsMu.Lock()
	defer chatSlotsMu.Unlock()
	return map[string]any{
		"active":   active,
		"limit":    chatSlotN,
		"peak":     chatSlotPeak,
		"rejected": chatSlotRejected,
		"total":    chatSlotTotal,
	}
}

func acquireChatSlotOrError(ctx context.Context) (func(), error) {
	release, err := acquireChatSlot(ctx)
	if err != nil {
		return nil, fmt.Errorf("global chat concurrency limit: %w", err)
	}
	return release, nil
}
