package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	maxArgs         = 1024
	maxBulkLength   = 1 << 20
	maxInlineLength = 64 * 1024
)

var errProtocol = errors.New("protocol error")

type reply interface{ encode() []byte }

type (
	simpleString string
	errorReply   string
	integer      int
	bulkString   string
	nullBulk     struct{}
)

var lineBreakReplacer = strings.NewReplacer("\r", " ", "\n", " ")

func (r simpleString) encode() []byte {
	return []byte("+" + lineBreakReplacer.Replace(string(r)) + "\r\n")
}

func (r errorReply) encode() []byte {
	return []byte("-" + lineBreakReplacer.Replace(string(r)) + "\r\n")
}

func (r integer) encode() []byte {
	return []byte(":" + strconv.Itoa(int(r)) + "\r\n")
}

func (r bulkString) encode() []byte {
	return []byte("$" + strconv.Itoa(len(r)) + "\r\n" + string(r) + "\r\n")
}

func (nullBulk) encode() []byte {
	return []byte("$-1\r\n")
}

func newCommandReader(rd io.Reader) *bufio.Reader {
	return bufio.NewReaderSize(rd, maxInlineLength)
}

func readCommand(r *bufio.Reader) ([]string, error) {
	first, err := r.Peek(1)
	if err != nil {
		return nil, err
	}
	if first[0] == '*' {
		return readArray(r)
	}
	return readInline(r)
}

func readInline(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	args := strings.Fields(string(line))
	if len(args) > maxArgs {
		return nil, fmt.Errorf("%w: too many arguments", errProtocol)
	}
	return args, nil
}

func readArray(r *bufio.Reader) ([]string, error) {
	n, err := readLength(r, '*', 1, maxArgs)
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, n)
	for range n {
		size, err := readLength(r, '$', 0, maxBulkLength)
		if err != nil {
			return nil, err
		}

		data := make([]byte, size+2)
		if _, err := io.ReadFull(r, data); err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if data[size] != '\r' || data[size+1] != '\n' {
			return nil, fmt.Errorf("%w: bulk data does not end with CRLF", errProtocol)
		}
		args = append(args, string(data[:size]))
	}
	return args, nil
}

func readLength(r *bufio.Reader, prefix byte, low, high int) (int, error) {
	line, err := readLine(r)
	if err != nil {
		return 0, err
	}
	if len(line) < 3 || line[0] != prefix || line[len(line)-2] != '\r' {
		return 0, fmt.Errorf("%w: expected '%c' header", errProtocol, prefix)
	}
	n, err := strconv.Atoi(string(line[1 : len(line)-2]))
	if err != nil || n < low || n > high {
		return 0, fmt.Errorf("%w: invalid length in '%c' header", errProtocol, prefix)
	}
	return n, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadSlice('\n')
	switch {
	case errors.Is(err, bufio.ErrBufferFull):
		return nil, fmt.Errorf("%w: line too long", errProtocol)
	case errors.Is(err, io.EOF):
		return nil, io.ErrUnexpectedEOF
	case err != nil:
		return nil, err
	}
	return line, nil
}
