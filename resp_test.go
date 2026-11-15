package main

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
)

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

		{name: "array", reply: bulkArray{"SET", "k", "hello world"}, want: "*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$11\r\nhello world\r\n"},
		{name: "array with empty element", reply: bulkArray{"ECHO", ""}, want: "*2\r\n$4\r\nECHO\r\n$0\r\n\r\n"},
		{name: "empty array", reply: bulkArray{}, want: "*0\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.reply.encode()); got != tt.want {
				t.Errorf("encode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func encodeCommand(args ...string) string {
	var b strings.Builder
	b.WriteString("*" + strconv.Itoa(len(args)) + "\r\n")
	for _, arg := range args {
		b.Write(bulkString(arg).encode())
	}
	return b.String()
}

func TestReadCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr error
	}{
		{name: "array", input: encodeCommand("PING"), want: []string{"PING"}},
		{name: "array with several arguments", input: encodeCommand("SET", "k", "v"), want: []string{"SET", "k", "v"}},
		{name: "argument with space", input: encodeCommand("SET", "k", "hello world"), want: []string{"SET", "k", "hello world"}},
		{name: "argument with line break", input: encodeCommand("ECHO", "a\r\nb"), want: []string{"ECHO", "a\r\nb"}},
		{name: "empty argument", input: encodeCommand("ECHO", ""), want: []string{"ECHO", ""}},
		{name: "non-ascii argument", input: encodeCommand("ECHO", "zażółć"), want: []string{"ECHO", "zażółć"}},
		{name: "largest bulk", input: encodeCommand(strings.Repeat("a", maxBulkLength)), want: []string{strings.Repeat("a", maxBulkLength)}},
		{name: "most arguments", input: encodeCommand(make([]string, maxArgs)...), want: make([]string, maxArgs)},

		{name: "inline", input: "PING\r\n", want: []string{"PING"}},
		{name: "inline with bare newline", input: "SET k v\n", want: []string{"SET", "k", "v"}},
		{name: "inline with extra whitespace", input: "  ECHO \t hello  \n", want: []string{"ECHO", "hello"}},
		{name: "empty inline line", input: "\r\n", want: []string{}},
		{name: "longest inline line", input: strings.Repeat("a", maxInlineLength-1) + "\n", want: []string{strings.Repeat("a", maxInlineLength-1)}},

		{name: "empty array", input: "*0\r\n", wantErr: errProtocol},
		{name: "negative array length", input: "*-1\r\n", wantErr: errProtocol},
		{name: "non-integer array length", input: "*abc\r\n", wantErr: errProtocol},
		{name: "too many arguments", input: encodeCommand(make([]string, maxArgs+1)...), wantErr: errProtocol},
		{name: "array header without CR", input: "*11\n$4\r\nPING\r\n", wantErr: errProtocol},
		{name: "bulk header without CR", input: "*1\r\n$44\nPING\r\n", wantErr: errProtocol},
		{name: "array header without length", input: "*\r\n", wantErr: errProtocol},
		{name: "element is not a bulk string", input: "*1\r\n:4\r\nPING\r\n", wantErr: errProtocol},
		{name: "null bulk", input: "*1\r\n$-1\r\n", wantErr: errProtocol},
		{name: "non-integer bulk length", input: "*1\r\n$abc\r\n", wantErr: errProtocol},
		{name: "bulk too long", input: "*1\r\n$1048577\r\n", wantErr: errProtocol},
		{name: "bulk shorter than declared", input: "*1\r\n$5\r\nPING\r\nxx", wantErr: errProtocol},
		{name: "bulk longer than declared", input: "*1\r\n$3\r\nPING\r\n", wantErr: errProtocol},
		{name: "bulk without CRLF", input: "*1\r\n$4\r\nPINGxx", wantErr: errProtocol},
		{name: "inline line too long", input: strings.Repeat("a", maxInlineLength) + "\n", wantErr: errProtocol},
		{name: "inline with too many arguments", input: strings.Repeat("a ", maxArgs+1) + "\n", wantErr: errProtocol},

		{name: "empty input", input: "", wantErr: io.EOF},
		{name: "array cut after header", input: "*2\r\n", wantErr: io.ErrUnexpectedEOF},
		{name: "array cut after an element", input: "*2\r\n$3\r\nGET\r\n", wantErr: io.ErrUnexpectedEOF},
		{name: "array cut inside bulk", input: "*1\r\n$4\r\nPI", wantErr: io.ErrUnexpectedEOF},
		{name: "array cut inside header", input: "*1", wantErr: io.ErrUnexpectedEOF},
		{name: "inline without newline", input: "PING", wantErr: io.ErrUnexpectedEOF},
	}

	readers := []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{name: "whole", wrap: func(r io.Reader) io.Reader { return r }},
		{name: "one byte at a time", wrap: iotest.OneByteReader},
	}

	for _, tt := range tests {
		for _, rd := range readers {
			t.Run(tt.name+"/"+rd.name, func(t *testing.T) {
				r := newCommandReader(rd.wrap(strings.NewReader(tt.input)))

				got, err := readCommand(r)
				if !slices.Equal(got, tt.want) || !errors.Is(err, tt.wantErr) {
					t.Errorf("readCommand() = %q, %v, want %q, %v", truncate(got), err, truncate(tt.want), tt.wantErr)
				}
				if tt.wantErr == nil && tt.want != nil {
					if _, err := readCommand(r); !errors.Is(err, io.EOF) {
						t.Errorf("readCommand() after the command: err = %v, want %v", err, io.EOF)
					}
				}
			})
		}
	}
}

func truncate(args []string) []string {
	out := make([]string, 0, min(len(args), 4))
	for _, arg := range args[:min(len(args), 4)] {
		if len(arg) > 20 {
			arg = arg[:20] + "..."
		}
		out = append(out, arg)
	}
	return out
}

func TestReadCommandPipelined(t *testing.T) {
	input := encodeCommand("SET", "k", "v") + "PING\r\n" + encodeCommand("GET", "k")
	want := [][]string{{"SET", "k", "v"}, {"PING"}, {"GET", "k"}}

	r := newCommandReader(strings.NewReader(input))
	for _, w := range want {
		if got, err := readCommand(r); !slices.Equal(got, w) || err != nil {
			t.Errorf("readCommand() = %q, %v, want %q, nil", got, err, w)
		}
	}
	if _, err := readCommand(r); !errors.Is(err, io.EOF) {
		t.Errorf("readCommand() err = %v, want %v", err, io.EOF)
	}
}

func FuzzReadCommand(f *testing.F) {
	seeds := []string{
		encodeCommand("SET", "k", "hello world"),
		"PING\r\n",
		"*2\r\n$3\r\nGET\r\n",
		"*1\r\n$-1\r\n",
		"*1\r\n$5\r\nPING\r\n",
		"",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		r := newCommandReader(strings.NewReader(input))
		for {
			args, err := readCommand(r)
			if err != nil {
				return
			}
			if len(args) > maxArgs {
				t.Errorf("readCommand() returned %d arguments, want at most %d", len(args), maxArgs)
			}
		}
	})
}

func FuzzReadCommandRoundTrip(f *testing.F) {
	f.Add("SET", "hello world")
	f.Add("", "a\r\nb")
	f.Add("*1", "$-1")

	f.Fuzz(func(t *testing.T, a, b string) {
		input := encodeCommand(a, b)
		got, err := readCommand(newCommandReader(strings.NewReader(input)))
		if want := []string{a, b}; !slices.Equal(got, want) || err != nil {
			t.Errorf("readCommand(%q) = %q, %v, want %q, nil", input, got, err, want)
		}
	})
}
