package main

import (
	"strconv"
	"strings"
)

func runInline(store *Store, line string) (reply, bool) {
	args := strings.Fields(line)
	if len(args) == 0 {
		return nil, false
	}
	return execute(store, args), true
}

func formatReply(r reply) string {
	switch r := r.(type) {
	case simpleString:
		return string(r)
	case errorReply:
		return "(error) " + string(r)
	case integer:
		return "(integer) " + strconv.Itoa(int(r))
	case bulkString:
		return strconv.Quote(string(r))
	case nullBulk:
		return "(nil)"
	default:
		return string(r.encode())
	}
}
