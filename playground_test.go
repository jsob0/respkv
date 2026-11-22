package main

import "testing"

func TestFormatReply(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  string
	}{
		{"simple string", simpleString("OK"), "OK"},
		{"error", errorReply("ERR unknown command 'foo'"), "(error) ERR unknown command 'foo'"},
		{"integer", integer(-2), "(integer) -2"},
		{"bulk string", bulkString("hello"), `"hello"`},
		{"bulk string with quotes and line break", bulkString("a \"b\"\nc"), `"a \"b\"\nc"`},
		{"empty bulk string", bulkString(""), `""`},
		{"null bulk", nullBulk{}, "(nil)"},
		{"array", bulkArray{"GET", "k"}, "*2\r\n$3\r\nGET\r\n$1\r\nk\r\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatReply(tt.reply); got != tt.want {
				t.Errorf("formatReply() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunInline(t *testing.T) {
	store := NewStore()
	session := []struct {
		line string
		want string
	}{
		{"PING", "PONG"},
		{"  set   greeting   hello  ", "OK"},
		{"GET greeting", `"hello"`},
		{"ttl greeting", "(integer) -1"},
		{"EXPIRE greeting 100", "(integer) 1"},
		{"TTL greeting", "(integer) 100"},
		{"DEL greeting missing", "(integer) 1"},
		{"GET greeting", "(nil)"},
		{"GET", "(error) ERR wrong number of arguments for 'get' command"},
		{"FOO bar", "(error) ERR unknown command 'FOO'"},
	}
	for _, step := range session {
		r, ok := runInline(store, step.line)
		if !ok {
			t.Fatalf("runInline(%q) returned no reply", step.line)
		}
		if got := formatReply(r); got != step.want {
			t.Errorf("runInline(%q) = %q, want %q", step.line, got, step.want)
		}
	}
}

func TestRunInlineIgnoresBlankLines(t *testing.T) {
	for _, line := range []string{"", "   ", "\t\r\n"} {
		if r, ok := runInline(NewStore(), line); ok {
			t.Errorf("runInline(%q) = %v, true, want no reply", line, r)
		}
	}
}
