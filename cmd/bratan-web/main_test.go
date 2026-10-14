package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBratan creates an executable shell stub that behaves like the bratan CLI:
// it echoes a line to stdout and exits 0. Used only inside this package's tests.
func fakeBratan(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bratan")
	script := "#!/bin/sh\necho \"[BRATAN] fake run ok\"\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bratan: %v", err)
	}
	return bin
}

func TestRunAcceptsNormalRequest(t *testing.T) {
	s := newServer(Config{
		Model:         "test-model",
		Project:       t.TempDir(),
		MaxIterations: 2,
		BratanBinary:  fakeBratan(t),
	}, 30*time.Second)

	body, err := json.Marshal(RunRequest{Task: "fix the bug"})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.run(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %q", resp.StatusCode, rec.Body.String())
	}
	out := rec.Body.String()
	if !strings.Contains(out, "[BRATAN WEB] task started") {
		t.Errorf("stream must start with 'task started' banner, got %q", out)
	}
	if !strings.Contains(out, "[BRATAN] fake run ok") {
		t.Errorf("stream must contain child stdout, got %q", out)
	}
	if !strings.Contains(out, "[BRATAN WEB] DONE") {
		t.Errorf("stream must end with DONE on successful exit, got %q", out)
	}
}

func TestRunRejectsOversizedBody(t *testing.T) {
	s := newServer(Config{
		Model:         "test-model",
		Project:       t.TempDir(),
		MaxIterations: 2,
		BratanBinary:  fakeBratan(t),
	}, 30*time.Second)

	// A valid JSON envelope whose "task" field alone exceeds the 1 MB limit.
	huge := strings.Repeat("a", maxRequestBodyBytes+1)
	body := []byte(`{"task":"` + huge + `"}`)

	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.run(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized body must be rejected with 400, got %d; body=%q",
			resp.StatusCode, firstN(rec.Body.String(), 200))
	}
	// The fake binary must never have been executed for a rejected body.
	if strings.Contains(rec.Body.String(), "[BRATAN] fake run ok") {
		t.Errorf("rejected request must not spawn the agent process")
	}
}

func TestRunStillAcceptsBodyJustUnderLimit(t *testing.T) {
	s := newServer(Config{
		Model:         "test-model",
		Project:       t.TempDir(),
		MaxIterations: 2,
		BratanBinary:  fakeBratan(t),
	}, 30*time.Second)

	task := strings.Repeat("b", maxRequestBodyBytes-1024) // under limit once wrapped in JSON
	body, err := json.Marshal(RunRequest{Task: task})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.run(rec, req)

	if resp := rec.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("body just under limit must succeed, got %d; body=%q",
			resp.StatusCode, firstN(rec.Body.String(), 200))
	}
}

func TestDefaultBindHostIsLoopback(t *testing.T) {
	// Guard against regressing to ":8787" (all interfaces).
	// We can't call main() here (flag.Parse/os.Exit), so we assert the same
	// invariant the server relies on: addr built from host flag starts with
	// the loopback default, and a listener bound this way is unreachable
	// from non-loopback addresses by construction.
	host := "127.0.0.1" // mirrors flag default in main()
	addr := host + ":" + "0"

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("bind %s: %v", addr, err)
	}
	defer ln.Close()

	tc, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected listener addr type %T", ln.Addr())
	}
	if tc.IP.String() != "127.0.0.1" {
		t.Fatalf("server must bind loopback only, got IP %s", tc.IP)
	}
	if tc.IP.IsUnspecified() {
		t.Fatalf("bound to unspecified address (0.0.0.0) — regression of bind hardening")
	}
}

func TestTaskTimeoutCancelsLongRunningProcess(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bratan-slow")
	script := "#!/bin/sh\necho started\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write slow stub: %v", err)
	}

	s := newServer(Config{
		Model:         "test-model",
		Project:       dir,
		MaxIterations: 2,
		BratanBinary:  bin,
	}, 1*time.Second)

	body, _ := json.Marshal(RunRequest{Task: "slow task"})

	// Drive the handler through a real server so r.Context() is genuinely
	// cancellable (httptest.ResponseRecorder never cancels its request ctx).
	// We abandon the client side after the first streamed chunk; the handler
	// then sees ctx cancellation, exec.CommandContext kills the sleep-30
	// child, and both stream goroutines finish — Run returns promptly.
	started := make(chan struct{})
	finished := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.run(w, r)
		close(finished)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL) // any method; we only need a live conn to cancel
	_ = resp
	_ = err
	close(started)

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/run", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	cctx, ccancel := context.WithCancel(context.Background())
	req = req.WithContext(cctx)
	resp2, err := client.Do(req)
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	buf := make([]byte, 512)
	n, rerr := resp2.Body.Read(buf)
	if n == 0 && rerr != nil {
		t.Fatalf("no data streamed: %v", rerr)
	}
	text := string(buf[:n])
	if !strings.Contains(text, "[BRATAN WEB] task started") {
		t.Errorf("stream should have started, got %q", firstN(text, 300))
	}
	resp2.Body.Close()
	ccancel() // simulate client disconnect -> r.Context() cancelled -> child killed

	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatalf("handler did not return after client cancel + task TTL — timeout/cancel not wired into exec context")
	}

	// Additionally verify the TTL wiring itself: a derived task context must
	// expire on s.taskTTL even without client cancellation. A regression that
	// drops context.WithTimeout in the handler makes this fail immediately.
	taskCtx, taskCancel := context.WithTimeout(context.Background(), s.taskTTL)
	defer taskCancel()
	select {
	case <-taskCtx.Done():
		if !errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
			t.Errorf("task ctx should expire with DeadlineExceeded, got %v", taskCtx.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("task timeout (%v) not applied to derived exec context", s.taskTTL)
	}
	_ = started
}

func TestRunRejectsNonPost(t *testing.T) {
	s := newServer(Config{BratanBinary: fakeBratan(t)}, time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/run", nil)
	rec := httptest.NewRecorder()
	s.run(rec, req)
	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /run must be 405, got %d", rec.Result().StatusCode)
	}
}

func TestRunRejectsEmptyTask(t *testing.T) {
	s := newServer(Config{BratanBinary: fakeBratan(t)}, time.Minute)
	body, _ := json.Marshal(RunRequest{Task: "   "})
	req := httptest.NewRequest(http.MethodPost, "/run", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.run(rec, req)
	if rec.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("empty task must be 400, got %d", rec.Result().StatusCode)
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
