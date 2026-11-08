package main

import "testing"

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
