package main

import (
	"strconv"
	"strings"
)

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
