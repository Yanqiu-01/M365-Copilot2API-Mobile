package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func benchmarkHTTPServer() *Server {
	return &Server{
		benchmark:          &benchmarkStore{run: benchmarkRun{State: "idle"}},
		accountPool:        newAccountHealth(),
		upstreamCooldown:   newAccountCooldown(),
		accountConcurrency: newAccountConcurrency(),
	}
}

func TestBenchmarkHTTPStatusAndStop(t *testing.T) {
	s := benchmarkHTTPServer()
	get := httptest.NewRecorder()
	s.adminBenchmark(get, httptest.NewRequest(http.MethodGet, "/api/admin/benchmark", nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"state":"idle"`) || !strings.Contains(get.Body.String(), `"bugfix"`) || !strings.Contains(get.Body.String(), `"efforts"`) {
		t.Fatalf("benchmark status=%d body=%s", get.Code, get.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.benchmark.run = benchmarkRun{State: "running", Cancellation: cancel}
	stop := httptest.NewRecorder()
	s.adminBenchmarkStop(stop, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/stop", nil))
	if stop.Code != http.StatusOK || !strings.Contains(stop.Body.String(), `"ok":true`) || s.benchmark.snapshot().State != "cancelled" {
		t.Fatalf("stop status=%d body=%s run=%#v", stop.Code, stop.Body.String(), s.benchmark.snapshot())
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stop did not cancel active benchmark")
	}
}

func TestBenchmarkRunValidatesAPKRequestShape(t *testing.T) {
	// 非法请求一律 400，且不得启动任何评测。
	for _, body := range []string{
		`{"effort":"unsupported"}`,
		`{"tasks":["not-a-task"]}`,
		`{`,
	} {
		s := benchmarkHTTPServer()
		w := httptest.NewRecorder()
		s.adminBenchmarkRun(w, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/run", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
		if state := s.benchmark.snapshot().State; state == "running" {
			t.Fatalf("invalid body=%s must not start a run", body)
		}
	}

	// 合法请求接受并进入 running；随后立刻停止，避免测试期真跑评测。
	for _, body := range []string{
		`{"model":"gpt-5.6-reasoning","effort":"max","tasks":["bugfix"]}`,
		`{"model":"","effort":"","tasks":[]}`,
	} {
		s := benchmarkHTTPServer()
		w := httptest.NewRecorder()
		s.adminBenchmarkRun(w, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/run", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"state":"running"`) {
			t.Fatalf("body=%s response=%s", body, w.Body.String())
		}
		s.benchmark.stop()
	}
}

// 已在运行时重入必须被拒绝，错误文案取自 APK
// startBenchmark +0x030c "已有评测在运行"(21 字节)。
func TestBenchmarkRunRejectsReentry(t *testing.T) {
	s := benchmarkHTTPServer()
	defer s.benchmark.stop()

	if err := s.startBenchmark("gpt-5.6-reasoning", "max", []string{"shift"}); err != nil {
		t.Fatalf("first start failed: %v", err)
	}
	err := s.startBenchmark("gpt-5.6-reasoning", "max", []string{"shift"})
	if err == nil {
		t.Fatal("second start must be rejected while running")
	}
	if err.Error() != "已有评测在运行" {
		t.Errorf("error=%q want 已有评测在运行", err.Error())
	}

	w := httptest.NewRecorder()
	s.adminBenchmarkRun(w, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/run",
		strings.NewReader(`{"model":"gpt-5.6-reasoning","effort":"max"}`)))
	if w.Code != http.StatusConflict {
		t.Errorf("reentry via HTTP status=%d want 409", w.Code)
	}
}

// 运行 ID 采用 APK 的 "20060102T150405Z" 格式（startBenchmark +0x011c）。
func TestBenchmarkRunIDFormat(t *testing.T) {
	s := benchmarkHTTPServer()
	if err := s.startBenchmark("gpt-5.6-reasoning", "max", []string{"route"}); err != nil {
		t.Fatal(err)
	}
	current := s.benchmark.snapshot().Current
	s.benchmark.stop()
	// 形如 20260819T071500Z：8 位日期 + T + 6 位时间 + Z。
	if len(current) != 16 || current[8] != 'T' || current[15] != 'Z' {
		t.Errorf("run id=%q does not match 20060102T150405Z", current)
	}
}

// 未知任务 ID 不应被静默忽略成"跑全部"。
func TestBenchmarkRunUnknownTaskSelectsNothing(t *testing.T) {
	s := benchmarkHTTPServer()
	err := s.startBenchmark("gpt-5.6-reasoning", "max", []string{"no-such-task"})
	if err == nil {
		s.benchmark.stop()
		t.Fatal("unknown task id must not start a run")
	}
}

func TestAccountHealthHTTPContract(t *testing.T) {
	s := benchmarkHTTPServer()
	w := httptest.NewRecorder()
	s.adminAccountHealth(w, httptest.NewRequest(http.MethodGet, "/api/admin/account-health", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"accounts"`) || !strings.Contains(w.Body.String(), `"retryAttempts"`) || !strings.Contains(w.Body.String(), `"maxConcurrentChats"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestBenchmarkHTTPMethods(t *testing.T) {
	s := benchmarkHTTPServer()
	for _, test := range []struct {
		name string
		h    http.HandlerFunc
		meth string
	}{
		{"health", s.adminAccountHealth, http.MethodPost},
		{"benchmark", s.adminBenchmark, http.MethodPost},
		{"run", s.adminBenchmarkRun, http.MethodGet},
		{"stop", s.adminBenchmarkStop, http.MethodGet},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			test.h(w, httptest.NewRequest(test.meth, "/", nil))
			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status=%d", w.Code)
			}
		})
	}
}
