package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, handler http.Handler, path string) string {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want %d", path, rec.Code, http.StatusOK)
	}
	return rec.Body.String()
}

func scrape(t *testing.T, s *Server) string {
	t.Helper()
	return get(t, s.metrics.handler(), "/metrics")
}

func assertMetric(t *testing.T, body, line string) {
	t.Helper()

	for got := range strings.Lines(body) {
		if strings.TrimSuffix(got, "\n") == line {
			return
		}
	}
	t.Errorf("metrics have no line %q", line)
}

func TestMetricsCountCommandsAndClients(t *testing.T) {
	s := newTestServer()
	addr, _ := startServer(t, s)
	conn := dial(t, addr)

	assertReply(t, conn, "SET a 1\r\n", "+OK\r\n")
	assertReply(t, conn, "set b 2\r\n", "+OK\r\n")
	assertReply(t, conn, "GET a\r\n", "$1\r\n1\r\n")
	assertReply(t, conn, "Get a\r\n", "$1\r\n1\r\n")
	assertReply(t, conn, "FOO\r\n", "-ERR unknown command 'FOO'\r\n")

	body := scrape(t, s)
	for _, line := range []string{
		`respkv_command_duration_seconds_count{command="set"} 2`,
		`respkv_command_duration_seconds_count{command="get"} 2`,
		`respkv_command_duration_seconds_count{command="unknown"} 1`,
		`respkv_command_duration_seconds_count{command="del"} 0`,
		`respkv_connected_clients 1`,
		`respkv_connections_total 1`,
		`respkv_keys 2`,
	} {
		assertMetric(t, body, line)
	}
	if strings.Contains(body, "respkv_aof_failed") {
		t.Error("metrics report respkv_aof_failed without an append-only file")
	}

	conn.Close()
	deadline := time.Now().Add(testTimeout)
	for !strings.Contains(scrape(t, s), "\nrespkv_connected_clients 0\n") {
		if time.Now().After(deadline) {
			t.Fatal("respkv_connected_clients did not drop to 0 after the client disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestMetricsReportAOFFailure(t *testing.T) {
	s := newTestServer()
	s.aofPath = filepath.Join(t.TempDir(), "appendonly.aof")
	if err := s.openAOF(); err != nil {
		t.Fatalf("openAOF: %v", err)
	}
	assertMetric(t, scrape(t, s), "respkv_aof_failed 0")

	s.aof.file.Close()
	s.store.Set("a", "1")
	if err := s.aof.Flush(); err == nil {
		t.Fatal("Flush after closing the file succeeded, want an error")
	}
	assertMetric(t, scrape(t, s), "respkv_aof_failed 1")
}

func TestMetricsDoNotReportClosedAOFAsFailed(t *testing.T) {
	s := newTestServer()
	s.aofPath = filepath.Join(t.TempDir(), "appendonly.aof")
	if err := s.openAOF(); err != nil {
		t.Fatalf("openAOF: %v", err)
	}
	if err := s.aof.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertMetric(t, scrape(t, s), "respkv_aof_failed 0")
}

func TestPprofEndpoints(t *testing.T) {
	handler := newTestServer().metrics.handler()

	if body := get(t, handler, "/debug/pprof/"); !strings.Contains(body, "goroutine") {
		t.Errorf("/debug/pprof/ does not list the goroutine profile:\n%.300s", body)
	}
	if body := get(t, handler, "/debug/pprof/goroutine?debug=1"); !strings.Contains(body, "goroutine profile:") {
		t.Errorf("/debug/pprof/goroutine is not a goroutine profile:\n%.300s", body)
	}
}

func TestServeMetricsOverHTTP(t *testing.T) {
	s := newTestServer()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stop := s.serveMetrics(listener)
	url := "http://" + listener.Addr().String() + "/metrics"

	client := &http.Client{Timeout: testTimeout}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read /metrics: %v", err)
	}
	assertMetric(t, string(body), "respkv_connections_total 0")

	stop()
	if _, err := client.Get(url); err == nil {
		t.Error("metrics server still answers after stop")
	}
}

func TestListenAndServeFailsOnBadMetricsAddr(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := free.Addr().String()
	free.Close()

	s := newTestServer()
	s.addr = addr
	s.metricsAddr = "127.0.0.1:-1"

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	err = s.ListenAndServe(ctx)
	if err == nil || !strings.Contains(err.Error(), "start metrics server") {
		t.Fatalf("ListenAndServe() = %v, want a metrics server error", err)
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("server address %s is still taken after the failed start: %v", addr, err)
	}
	listener.Close()
}
