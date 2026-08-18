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
	s := benchmarkHTTPServer()
	for _, body := range []string{
		`{"model":"gpt-5.6-reasoning","effort":"max","tasks":["bugfix"]}`,
		`{"model":"","effort":"","tasks":[]}`,
	} {
		w := httptest.NewRecorder()
		s.adminBenchmarkRun(w, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/run", strings.NewReader(body)))
		if w.Code != http.StatusNotImplemented {
			t.Fatalf("body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{
		`{"effort":"unsupported"}`,
		`{"tasks":["not-a-task"]}`,
		`{`,
	} {
		w := httptest.NewRecorder()
		s.adminBenchmarkRun(w, httptest.NewRequest(http.MethodPost, "/api/admin/benchmark/run", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid body=%s status=%d response=%s", body, w.Code, w.Body.String())
		}
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
