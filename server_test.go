package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const testTimeout = 2 * time.Second

func newTestServer() *Server {
	return NewServer("", slog.New(slog.DiscardHandler))
}

func startServer(t *testing.T, s *Server) (addr string, stop func() error) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, listener)
	}()

	stop = sync.OnceValue(func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(testTimeout):
			return errors.New("server did not stop in time")
		}
	})
	t.Cleanup(func() {
		if err := stop(); err != nil {
			t.Error(err)
		}
	})

	return listener.Addr().String(), stop
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(testTimeout)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	return conn
}

func assertReply(t *testing.T, conn net.Conn, input, want string) {
	t.Helper()

	if _, err := io.WriteString(conn, input); err != nil {
		t.Fatalf("write: %v", err)
	}

	reader := bufio.NewReader(conn)
	var got strings.Builder
	for range strings.Count(input, "\n") {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got.WriteString(line)
	}

	if got.String() != want {
		t.Errorf("got %q, want %q", got.String(), want)
	}
	if n := reader.Buffered(); n > 0 {
		extra, _ := reader.Peek(n)
		t.Errorf("unexpected extra data: %q", extra)
	}
}

func assertClosedByServer(t *testing.T, conn net.Conn) {
	t.Helper()

	if _, err := conn.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("read error = %v, want EOF", err)
	}
}

// A close with unread data is reported as a reset rather than EOF.
func isConnectionClosed(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

func TestCommands(t *testing.T) {
	maxArgumentSize := 64*1024 - 6

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "single line", input: "PING\n", want: "PONG\n"},
		{name: "multiple lines", input: "PING\nECHO hello\n", want: "PONG\nhello\n"},
		{name: "empty line", input: "\n", want: "ERR empty command\n"},
		{name: "windows line ending", input: "PING\r\n", want: "PONG\n"},
		{name: "unknown command", input: "FOO\n", want: "ERR unknown command 'FOO'\n"},
		{
			name:  "longest allowed line",
			input: "ECHO " + strings.Repeat("a", maxArgumentSize) + "\n",
			want:  strings.Repeat("a", maxArgumentSize) + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := startServer(t, newTestServer())
			assertReply(t, dial(t, addr), tt.input, tt.want)
		})
	}
}

func TestLineSplitAcrossWrites(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	conn := dial(t, addr)

	if _, err := io.WriteString(conn, "PI"); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertReply(t, conn, "NG\n", "PONG\n")
}

func TestClientsAreHandledConcurrently(t *testing.T) {
	addr, _ := startServer(t, newTestServer())

	conns := make([]net.Conn, 5)
	for i := range conns {
		conns[i] = dial(t, addr)
	}

	// The last client would never get a reply if earlier connections blocked it.
	for i := len(conns) - 1; i >= 0; i-- {
		assertReply(t, conns[i], "PING\n", "PONG\n")
	}
}

func TestClientsShareStore(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	first, second := dial(t, addr), dial(t, addr)

	assertReply(t, first, "SET name jakub\n", "OK\n")
	assertReply(t, second, "GET name\n", "jakub\n")
}

func TestServerRemovesExpiredKeys(t *testing.T) {
	s := newTestServer()
	s.expireInterval = time.Millisecond
	s.store.data["key"] = entry{value: "value", expiresAt: time.Unix(1, 0)}
	startServer(t, s)

	deadline := time.Now().Add(testTimeout)
	for {
		s.store.mu.RLock()
		n := len(s.store.data)
		s.store.mu.RUnlock()

		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("stored keys = %d, want 0", n)
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func TestIdleClientIsDisconnected(t *testing.T) {
	s := newTestServer()
	s.idleTimeout = 50 * time.Millisecond
	addr, _ := startServer(t, s)

	assertClosedByServer(t, dial(t, addr))
}

func TestShutdownDisconnectsClientsAndStopsListening(t *testing.T) {
	addr, stop := startServer(t, newTestServer())
	conn := dial(t, addr)
	// Make sure the connection is being handled before shutting down.
	assertReply(t, conn, "PING\n", "PONG\n")

	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}

	assertClosedByServer(t, conn)
	if conn, err := net.Dial("tcp", addr); err == nil {
		conn.Close()
		t.Error("server still accepts connections after shutdown")
	}
}

func TestClientSendingTooLongLineIsDisconnected(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	conn := dial(t, addr)

	// bufio.Scanner accepts lines of up to 64 KB including the newline,
	// so 64 KB without a newline is already too long.

	if _, err := conn.Write(bytes.Repeat([]byte{'a'}, 64*1024)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := conn.Read(make([]byte, 1)); !isConnectionClosed(err) {
		t.Errorf("read error = %v, want connection closed", err)
	}
}

func TestClientNotReadingIsDisconnected(t *testing.T) {
	s := newTestServer()
	s.writeTimeout = 50 * time.Millisecond
	addr, _ := startServer(t, s)
	conn := dial(t, addr)

	line := strings.Repeat("a", 1023) + "\n"
	for {
		_, err := io.WriteString(conn, line)
		if err == nil {
			continue
		}
		if !isConnectionClosed(err) {
			t.Errorf("write error = %v, want connection closed", err)
		}
		return
	}
}

func TestIncompleteLineIsNotExecutedOnHalfClose(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	conn := dial(t, addr)

	if _, err := io.WriteString(conn, "PING"); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatalf("close write: %v", err)
	}

	got, err := io.ReadAll(conn)
	if len(got) > 0 {
		t.Errorf("server responded with: %q", string(got))
	}
	if err != nil {
		t.Errorf("read: %v", err)
	}
}

func TestIncompleteLineIsNotExecutedOnIdleTimeout(t *testing.T) {
	s := newTestServer()
	s.idleTimeout = 50 * time.Millisecond
	addr, _ := startServer(t, s)
	conn := dial(t, addr)

	if _, err := io.WriteString(conn, "PING"); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := io.ReadAll(conn)
	if len(got) > 0 {
		t.Errorf("server responded with: %q", string(got))
	}
	if err != nil {
		t.Errorf("read: %v", err)
	}
}
