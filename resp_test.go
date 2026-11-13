package main

import "testing"

func TestReplyEncode(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  string
	}{
		{name: "simple string", reply: simpleString("OK"), want: "+OK\r\n"},
		{name: "simple string with line breaks", reply: simpleString("a\r\nb"), want: "+a  b\r\n"},

		{name: "error", reply: errorReply("ERR boom"), want: "-ERR boom\r\n"},
		{
			name:  "error with injected reply",
			reply: errorReply("ERR unknown command 'FOO\r\n+OK'"),
			want:  "-ERR unknown command 'FOO  +OK'\r\n",
		},
		{name: "error with bare newline", reply: errorReply("ERR a\nb"), want: "-ERR a b\r\n"},

		{name: "zero", reply: integer(0), want: ":0\r\n"},
		{name: "positive integer", reply: integer(42), want: ":42\r\n"},
		{name: "negative integer", reply: integer(-2), want: ":-2\r\n"},

		{name: "bulk string", reply: bulkString("hello"), want: "$5\r\nhello\r\n"},
		{name: "empty bulk string", reply: bulkString(""), want: "$0\r\n\r\n"},
		{name: "bulk string with space", reply: bulkString("hello world"), want: "$11\r\nhello world\r\n"},
		{name: "bulk string keeps line breaks", reply: bulkString("a\r\nb"), want: "$4\r\na\r\nb\r\n"},
		{name: "bulk string length in bytes", reply: bulkString("zażółć"), want: "$10\r\nzażółć\r\n"},

		{name: "null bulk string", reply: nullBulk{}, want: "$-1\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.reply.encode()); got != tt.want {
				t.Errorf("encode() = %q, want %q", got, tt.want)
			}
		})
	}
}
