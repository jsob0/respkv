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

		{name: "unknown command", line: "FOO bar", want: "ERR unknown command 'FOO'"},
		{name: "unknown command keeps its case", line: "foo bar", want: "ERR unknown command 'foo'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := execute(tt.line); got != tt.want {
				t.Errorf("execute(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
