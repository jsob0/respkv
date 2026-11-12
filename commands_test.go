package main

import (
	"testing"
	"time"
)

func TestExecute(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "empty line", line: "", want: "ERR empty command"},
		{name: "whitespace only", line: "   ", want: "ERR empty command"},
		{name: "extra whitespace", line: "      ECHO   hello  ", want: "hello"},
		{name: "tab separator", line: "ECHO\thello", want: "hello"},
		{name: "trailing carriage return", line: "PING\r", want: "PONG"},

		{name: "ping", line: "PING", want: "PONG"},
		{name: "ping lowercase", line: "ping", want: "PONG"},
		{name: "ping mixed case", line: "PiNg", want: "PONG"},
		{name: "ping with message", line: "PING hello", want: "hello"},
		{name: "ping with too many arguments", line: "PING hello world", want: "ERR wrong number of arguments for 'ping' command"},

		{name: "echo", line: "ECHO hello", want: "hello"},
		{name: "echo lowercase", line: "echo hello", want: "hello"},
		{name: "echo keeps argument case", line: "ECHO Hello", want: "Hello"},
		{name: "echo non-ascii argument", line: "ECHO zażółć", want: "zażółć"},
		{name: "echo without argument", line: "ECHO", want: "ERR wrong number of arguments for 'echo' command"},
		{name: "echo with too many arguments", line: "ECHO hello world", want: "ERR wrong number of arguments for 'echo' command"},

		{name: "get missing key", line: "GET missing", want: "(nil)"},
		{name: "get without key", line: "GET", want: "ERR wrong number of arguments for 'get' command"},
		{name: "get with too many arguments", line: "GET a b", want: "ERR wrong number of arguments for 'get' command"},

		{name: "set without arguments", line: "SET", want: "ERR wrong number of arguments for 'set' command"},
		{name: "set without value", line: "SET key", want: "ERR wrong number of arguments for 'set' command"},
		{name: "set with too many arguments", line: "SET key a b", want: "ERR wrong number of arguments for 'set' command"},

		{name: "del missing key", line: "DEL missing", want: "0"},
		{name: "del without keys", line: "DEL", want: "ERR wrong number of arguments for 'del' command"},

		{name: "expire missing key", line: "EXPIRE missing 10", want: "0"},
		{name: "expire without arguments", line: "EXPIRE", want: "ERR wrong number of arguments for 'expire' command"},
		{name: "expire without seconds", line: "EXPIRE key", want: "ERR wrong number of arguments for 'expire' command"},
		{name: "expire with too many arguments", line: "EXPIRE key 10 20", want: "ERR wrong number of arguments for 'expire' command"},
		{name: "expire with non-integer seconds", line: "EXPIRE key abc", want: "ERR value is not an integer or out of range"},
		{name: "expire with fractional seconds", line: "EXPIRE key 1.5", want: "ERR value is not an integer or out of range"},
		{name: "expire with too large seconds", line: "EXPIRE key 9223372037", want: "ERR value is not an integer or out of range"},

		{name: "ttl missing key", line: "TTL missing", want: "-2"},
		{name: "ttl without key", line: "TTL", want: "ERR wrong number of arguments for 'ttl' command"},
		{name: "ttl with too many arguments", line: "TTL a b", want: "ERR wrong number of arguments for 'ttl' command"},

		{name: "unknown command", line: "FOO bar", want: "ERR unknown command 'FOO'"},
		{name: "unknown command keeps its case", line: "foo bar", want: "ERR unknown command 'foo'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execute(NewStore(), tt.line); got != tt.want {
				t.Errorf("execute(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestExecuteSequence(t *testing.T) {
	steps := []struct {
		input string
		want  string
	}{
		{input: "SET k1 v1", want: "OK"},
		{input: "GET k1", want: "v1"},
		{input: "SET k1 v2", want: "OK"},
		{input: "GET k1", want: "v2"},
		{input: "DEL k1", want: "1"},
		{input: "GET k1", want: "(nil)"},
		{input: "SET k2 v3", want: "OK"},
		{input: "SET k3 v4", want: "OK"},
		{input: "DEL k2 k3", want: "2"},
		{input: "DEL k4", want: "0"},
		{input: "set k5 v5", want: "OK"},
		{input: "get k5", want: "v5"},
		{input: "del k5", want: "1"},
	}

	store := NewStore()
	for _, step := range steps {
		if got := execute(store, step.input); got != step.want {
			t.Errorf("execute(%q) = %q, want %q", step.input, got, step.want)
		}
	}
}

func TestExecuteExpiration(t *testing.T) {
	steps := []struct {
		advance time.Duration
		input   string
		want    string
	}{
		{input: "SET k v", want: "OK"},
		{input: "TTL k", want: "-1"},
		{input: "EXPIRE k 10", want: "1"},
		{input: "TTL k", want: "10"},
		{advance: 3200 * time.Millisecond, input: "TTL k", want: "7"},
		{advance: 6800 * time.Millisecond, input: "TTL k", want: "-2"},
		{input: "GET k", want: "(nil)"},
		{input: "EXPIRE k 10", want: "0"},

		{input: "SET a v", want: "OK"},
		{input: "EXPIRE a 10", want: "1"},
		{input: "SET a w", want: "OK"},
		{input: "TTL a", want: "-1"},

		{input: "SET b v", want: "OK"},
		{input: "EXPIRE b -5", want: "1"},
		{input: "GET b", want: "(nil)"},

		{input: "SET c v", want: "OK"},
		{input: "EXPIRE c -9223372037", want: "1"},
		{input: "GET c", want: "(nil)"},

		{input: "SET m v", want: "OK"},
		{input: "EXPIRE m 9223372036", want: "1"},
		{input: "TTL m", want: "9223372036"},
	}

	s, advance := newStoreWithClock()
	for _, step := range steps {
		advance(step.advance)
		if got := execute(s, step.input); got != step.want {
			t.Errorf("execute(%q) = %q, want %q", step.input, got, step.want)
		}
	}
}
