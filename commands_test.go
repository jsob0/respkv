package main

import (
	"strings"
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		line string
		want reply
	}{
		{name: "empty command", line: "", want: errorReply("ERR empty command")},

		{name: "ping", line: "PING", want: simpleString("PONG")},
		{name: "ping lowercase", line: "ping", want: simpleString("PONG")},
		{name: "ping mixed case", line: "PiNg", want: simpleString("PONG")},
		{name: "ping with message", line: "PING hello", want: bulkString("hello")},
		{name: "ping with too many arguments", line: "PING hello world", want: errorReply("ERR wrong number of arguments for 'ping' command")},

		{name: "echo", line: "ECHO hello", want: bulkString("hello")},
		{name: "echo lowercase", line: "echo hello", want: bulkString("hello")},
		{name: "echo keeps argument case", line: "ECHO Hello", want: bulkString("Hello")},
		{name: "echo non-ascii argument", line: "ECHO zażółć", want: bulkString("zażółć")},
		{name: "echo looking like a simple string", line: "ECHO OK", want: bulkString("OK")},
		{name: "echo without argument", line: "ECHO", want: errorReply("ERR wrong number of arguments for 'echo' command")},
		{name: "echo with too many arguments", line: "ECHO hello world", want: errorReply("ERR wrong number of arguments for 'echo' command")},

		{name: "get missing key", line: "GET missing", want: nullBulk{}},
		{name: "get without key", line: "GET", want: errorReply("ERR wrong number of arguments for 'get' command")},
		{name: "get with too many arguments", line: "GET a b", want: errorReply("ERR wrong number of arguments for 'get' command")},

		{name: "set", line: "SET key value", want: simpleString("OK")},
		{name: "set without arguments", line: "SET", want: errorReply("ERR wrong number of arguments for 'set' command")},
		{name: "set without value", line: "SET key", want: errorReply("ERR wrong number of arguments for 'set' command")},
		{name: "set with too many arguments", line: "SET key a b", want: errorReply("ERR wrong number of arguments for 'set' command")},

		{name: "del missing key", line: "DEL missing", want: integer(0)},
		{name: "del without keys", line: "DEL", want: errorReply("ERR wrong number of arguments for 'del' command")},

		{name: "expire missing key", line: "EXPIRE missing 10", want: integer(0)},
		{name: "expire without arguments", line: "EXPIRE", want: errorReply("ERR wrong number of arguments for 'expire' command")},
		{name: "expire without seconds", line: "EXPIRE key", want: errorReply("ERR wrong number of arguments for 'expire' command")},
		{name: "expire with too many arguments", line: "EXPIRE key 10 20", want: errorReply("ERR wrong number of arguments for 'expire' command")},
		{name: "expire with non-integer seconds", line: "EXPIRE key abc", want: errorReply("ERR value is not an integer or out of range")},
		{name: "expire with fractional seconds", line: "EXPIRE key 1.5", want: errorReply("ERR value is not an integer or out of range")},
		{name: "expire with too large seconds", line: "EXPIRE key 9223372037", want: errorReply("ERR value is not an integer or out of range")},

		{name: "pexpireat missing key", line: "PEXPIREAT missing 1800000000000", want: integer(0)},
		{name: "pexpireat without arguments", line: "PEXPIREAT", want: errorReply("ERR wrong number of arguments for 'pexpireat' command")},
		{name: "pexpireat without time", line: "PEXPIREAT key", want: errorReply("ERR wrong number of arguments for 'pexpireat' command")},
		{name: "pexpireat with too many arguments", line: "PEXPIREAT key 1 2", want: errorReply("ERR wrong number of arguments for 'pexpireat' command")},
		{name: "pexpireat with non-integer time", line: "PEXPIREAT key abc", want: errorReply("ERR value is not an integer or out of range")},
		{name: "pexpireat with fractional time", line: "PEXPIREAT key 1.5", want: errorReply("ERR value is not an integer or out of range")},
		{name: "pexpireat with too large time", line: "PEXPIREAT key 9223372036854775808", want: errorReply("ERR value is not an integer or out of range")},

		{name: "ttl missing key", line: "TTL missing", want: integer(-2)},
		{name: "ttl without key", line: "TTL", want: errorReply("ERR wrong number of arguments for 'ttl' command")},
		{name: "ttl with too many arguments", line: "TTL a b", want: errorReply("ERR wrong number of arguments for 'ttl' command")},

		{name: "unknown command", line: "FOO bar", want: errorReply("ERR unknown command 'FOO'")},
		{name: "unknown command keeps its case", line: "foo bar", want: errorReply("ERR unknown command 'foo'")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execute(NewStore(), strings.Fields(tt.line)); got != tt.want {
				t.Errorf("execute(%q) = %#v, want %#v", tt.line, got, tt.want)
			}
		})
	}
}

func TestExecuteArguments(t *testing.T) {
	steps := []struct {
		args []string
		want reply
	}{
		{args: []string{"SET", "greeting", "hello world"}, want: simpleString("OK")},
		{args: []string{"GET", "greeting"}, want: bulkString("hello world")},
		{args: []string{"SET", "lines", "a\r\nb"}, want: simpleString("OK")},
		{args: []string{"GET", "lines"}, want: bulkString("a\r\nb")},
		{args: []string{"SET", "empty", ""}, want: simpleString("OK")},
		{args: []string{"GET", "empty"}, want: bulkString("")},
		{args: []string{"SET", "nil", "(nil)"}, want: simpleString("OK")},
		{args: []string{"GET", "nil"}, want: bulkString("(nil)")},
		{args: []string{"SET", "my key", "v"}, want: simpleString("OK")},
		{args: []string{"DEL", "my key"}, want: integer(1)},
		{args: []string{"ECHO", ""}, want: bulkString("")},
	}

	store := NewStore()
	for _, step := range steps {
		if got := execute(store, step.args); got != step.want {
			t.Errorf("execute(%q) = %#v, want %#v", step.args, got, step.want)
		}
	}
}

func TestUnknownCommandCannotInjectReplies(t *testing.T) {
	got := string(execute(NewStore(), []string{"FOO\r\n+OK"}).encode())
	if want := "-ERR unknown command 'FOO  +OK'\r\n"; got != want {
		t.Errorf("execute().encode() = %q, want %q", got, want)
	}
}

func TestExecuteSequence(t *testing.T) {
	steps := []struct {
		input string
		want  reply
	}{
		{input: "SET k1 v1", want: simpleString("OK")},
		{input: "GET k1", want: bulkString("v1")},
		{input: "SET k1 v2", want: simpleString("OK")},
		{input: "GET k1", want: bulkString("v2")},
		{input: "DEL k1", want: integer(1)},
		{input: "GET k1", want: nullBulk{}},
		{input: "SET k2 v3", want: simpleString("OK")},
		{input: "SET k3 v4", want: simpleString("OK")},
		{input: "DEL k2 k3", want: integer(2)},
		{input: "DEL k4", want: integer(0)},
		{input: "set k5 v5", want: simpleString("OK")},
		{input: "get k5", want: bulkString("v5")},
		{input: "del k5", want: integer(1)},
	}

	store := NewStore()
	for _, step := range steps {
		if got := execute(store, strings.Fields(step.input)); got != step.want {
			t.Errorf("execute(%q) = %#v, want %#v", step.input, got, step.want)
		}
	}
}

func TestExecuteExpiration(t *testing.T) {
	steps := []struct {
		advance time.Duration
		input   string
		want    reply
	}{
		{input: "SET k v", want: simpleString("OK")},
		{input: "TTL k", want: integer(-1)},
		{input: "EXPIRE k 10", want: integer(1)},
		{input: "TTL k", want: integer(10)},
		{advance: 3200 * time.Millisecond, input: "TTL k", want: integer(7)},
		{advance: 6800 * time.Millisecond, input: "TTL k", want: integer(-2)},
		{input: "GET k", want: nullBulk{}},
		{input: "EXPIRE k 10", want: integer(0)},

		{input: "SET a v", want: simpleString("OK")},
		{input: "EXPIRE a 10", want: integer(1)},
		{input: "SET a w", want: simpleString("OK")},
		{input: "TTL a", want: integer(-1)},

		{input: "SET b v", want: simpleString("OK")},
		{input: "EXPIRE b -5", want: integer(1)},
		{input: "GET b", want: nullBulk{}},

		{input: "SET c v", want: simpleString("OK")},
		{input: "EXPIRE c -9223372037", want: integer(1)},
		{input: "GET c", want: nullBulk{}},

		{input: "SET m v", want: simpleString("OK")},
		{input: "EXPIRE m 9223372036", want: integer(1)},
		{input: "TTL m", want: integer(9223372036)},

		{input: "SET p v", want: simpleString("OK")},
		{input: "PEXPIREAT p 1000020000", want: integer(1)},
		{input: "TTL p", want: integer(10)},
		{input: "PEXPIREAT p 1000010001", want: integer(1)},
		{input: "TTL p", want: integer(1)},
		{input: "PEXPIREAT p 1000010000", want: integer(1)},
		{input: "GET p", want: nullBulk{}},
		{input: "PEXPIREAT p 1000020000", want: integer(0)},

		{input: "SET q v", want: simpleString("OK")},
		{input: "PEXPIREAT q -1", want: integer(1)},
		{input: "GET q", want: nullBulk{}},
	}

	s, advance := newStoreWithClock()
	for _, step := range steps {
		advance(step.advance)
		if got := execute(s, strings.Fields(step.input)); got != step.want {
			t.Errorf("execute(%q) = %#v, want %#v", step.input, got, step.want)
		}
	}
}
