package main

import (
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

// assertReply reads exactly len(want) bytes, so extra data after the reply
// is only noticed by the next read on the same connection.
func assertReply(t *testing.T, conn net.Conn, input, want string) {
	t.Helper()

	if _, err := io.WriteString(conn, input); err != nil {
		t.Fatalf("write: %v", err)
	}

	got := make([]byte, len(want))
	if n, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reply to %.100q: read %.100q, err = %v, want %.100q", input, got[:n], err, want)
	}
	if string(got) != want {
		t.Errorf("reply to %.100q = %.100q, want %.100q", input, got, want)
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
	largestBulk := strings.Repeat("a", maxBulkLength)
	longestInlineArgument := strings.Repeat("a", maxInlineLength-len("ECHO \n"))

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "ping", input: encodeCommand("PING"), want: "+PONG\r\n"},
		{name: "set", input: encodeCommand("SET", "k", "hello world"), want: "+OK\r\n"},
		{name: "get missing key", input: encodeCommand("GET", "missing"), want: "$-1\r\n"},
		{name: "del missing key", input: encodeCommand("DEL", "missing"), want: ":0\r\n"},
		{name: "unknown command", input: encodeCommand("FOO"), want: "-ERR unknown command 'FOO'\r\n"},
		{
			name:  "wrong number of arguments",
			input: encodeCommand("GET"),
			want:  "-ERR wrong number of arguments for 'get' command\r\n",
		},
		{name: "largest bulk", input: encodeCommand("ECHO", largestBulk), want: "$1048576\r\n" + largestBulk + "\r\n"},

		{name: "inline", input: "PING\r\n", want: "+PONG\r\n"},
		{name: "inline with bare newline", input: "PING\n", want: "+PONG\r\n"},
		{name: "empty inline line", input: "\r\n", want: "-ERR empty command\r\n"},
		{
			name:  "longest inline line",
			input: "ECHO " + longestInlineArgument + "\n",
			want:  "$65530\r\n" + longestInlineArgument + "\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := startServer(t, newTestServer())
			assertReply(t, dial(t, addr), tt.input, tt.want)
		})
	}
}

func TestCommandSplitAcrossWrites(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	conn := dial(t, addr)

	command := encodeCommand("ECHO", "hello")
	half := len(command) - len("llo\r\n")
	if _, err := io.WriteString(conn, command[:half]); err != nil {
		t.Fatalf("write: %v", err)
	}
	assertReply(t, conn, command[half:], "$5\r\nhello\r\n")
}

func TestPipelinedCommands(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	conn := dial(t, addr)

	input := encodeCommand("SET", "k", "v") +
		encodeCommand("GET", "k") +
		"PING\r\n" +
		encodeCommand("DEL", "k") +
		encodeCommand("GET", "k")
	want := "+OK\r\n" + "$1\r\nv\r\n" + "+PONG\r\n" + ":1\r\n" + "$-1\r\n"

	assertReply(t, conn, input, want)
}

func TestProtocolErrorClosesConnection(t *testing.T) {
	// Each input ends where the error is found, so the server leaves no
	// unread data behind and closes with EOF instead of a reset.
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty array", input: "*0\r\n", want: "-ERR protocol error: invalid length in '*' header\r\n"},
		{name: "non-integer bulk length", input: "*1\r\n$abc\r\n", want: "-ERR protocol error: invalid length in '$' header\r\n"},
		{name: "element is not a bulk string", input: "*1\r\n:4\r\n", want: "-ERR protocol error: expected '$' header\r\n"},
		{name: "bulk without CRLF", input: "*1\r\n$4\r\nPINGxx", want: "-ERR protocol error: bulk data does not end with CRLF\r\n"},
		{
			name:  "error after a valid command",
			input: encodeCommand("PING") + "*0\r\n",
			want:  "+PONG\r\n" + "-ERR protocol error: invalid length in '*' header\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := startServer(t, newTestServer())
			conn := dial(t, addr)

			assertReply(t, conn, tt.input, tt.want)
			assertClosedByServer(t, conn)
		})
	}
}

func TestClientsAreHandledConcurrently(t *testing.T) {
	addr, _ := startServer(t, newTestServer())

	conns := make([]net.Conn, 5)
	for i := range conns {
		conns[i] = dial(t, addr)
	}

	// The last client would never get a reply if earlier connections blocked it.
	for i := len(conns) - 1; i >= 0; i-- {
		assertReply(t, conns[i], encodeCommand("PING"), "+PONG\r\n")
	}
}

func TestClientsShareStore(t *testing.T) {
	addr, _ := startServer(t, newTestServer())
	first, second := dial(t, addr), dial(t, addr)

	assertReply(t, first, encodeCommand("SET", "name", "jakub"), "+OK\r\n")
	assertReply(t, second, encodeCommand("GET", "name"), "$5\r\njakub\r\n")
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
	assertReply(t, conn, encodeCommand("PING"), "+PONG\r\n")

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

	// The reply content is not checked: a reset may discard it before it
	// is read. TestProtocolErrorClosesConnection covers the reply.
	if _, err := io.WriteString(conn, strings.Repeat("a", maxInlineLength)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.ReadAll(conn); err != nil && !isConnectionClosed(err) {
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

var incompleteCommands = []struct {
	name  string
	input string
}{
	{name: "inline", input: "PING"},
	{name: "array", input: "*1\r\n$4\r\nPI"},
}

func TestIncompleteCommandIsNotExecutedOnHalfClose(t *testing.T) {
	for _, tt := range incompleteCommands {
		t.Run(tt.name, func(t *testing.T) {
			addr, _ := startServer(t, newTestServer())
			conn := dial(t, addr)

			if _, err := io.WriteString(conn, tt.input); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatalf("close write: %v", err)
			}

			assertNoReply(t, conn)
		})
	}
}

func TestIncompleteCommandIsNotExecutedOnIdleTimeout(t *testing.T) {
	for _, tt := range incompleteCommands {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer()
			s.idleTimeout = 50 * time.Millisecond
			addr, _ := startServer(t, s)
			conn := dial(t, addr)

			if _, err := io.WriteString(conn, tt.input); err != nil {
				t.Fatalf("write: %v", err)
			}

			assertNoReply(t, conn)
		})
	}
}

func assertNoReply(t *testing.T, conn net.Conn) {
	t.Helper()

	got, err := io.ReadAll(conn)
	if len(got) > 0 {
		t.Errorf("server responded with: %q", got)
	}
	if err != nil {
		t.Errorf("read: %v", err)
	}
}
