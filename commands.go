package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type commandFunc func(store *Store, args []string) (reply, error)

var errNotInteger = errors.New("value is not an integer or out of range")

var commands = map[string]commandFunc{
	"get":    getCommand,
	"set":    setCommand,
	"del":    delCommand,
	"echo":   echoCommand,
	"ping":   pingCommand,
	"expire": expireCommand,
	"ttl":    ttlCommand,
}

func execute(store *Store, args []string) reply {
	if len(args) == 0 {
		return errorReply("ERR empty command")
	}

	cmd, ok := commands[strings.ToLower(args[0])]
	if !ok {
		return errorReply(fmt.Sprintf("ERR unknown command '%s'", args[0]))
	}

	r, err := cmd(store, args[1:])
	if err != nil {
		return errorReply("ERR " + err.Error())
	}
	return r
}

func getCommand(store *Store, args []string) (reply, error) {
	if len(args) != 1 {
		return nil, errWrongArgs("get")
	}
	if value, ok := store.Get(args[0]); ok {
		return bulkString(value), nil
	}
	return nullBulk{}, nil
}

func setCommand(store *Store, args []string) (reply, error) {
	if len(args) != 2 {
		return nil, errWrongArgs("set")
	}
	store.Set(args[0], args[1])
	return simpleString("OK"), nil
}

func delCommand(store *Store, args []string) (reply, error) {
	if len(args) == 0 {
		return nil, errWrongArgs("del")
	}
	return integer(store.Delete(args...)), nil
}

func echoCommand(_ *Store, args []string) (reply, error) {
	if len(args) != 1 {
		return nil, errWrongArgs("echo")
	}
	return bulkString(args[0]), nil
}

func pingCommand(_ *Store, args []string) (reply, error) {
	switch len(args) {
	case 0:
		return simpleString("PONG"), nil
	case 1:
		return bulkString(args[0]), nil
	default:
		return nil, errWrongArgs("ping")
	}
}

func expireCommand(store *Store, args []string) (reply, error) {
	if len(args) != 2 {
		return nil, errWrongArgs("expire")
	}
	seconds, err := strconv.Atoi(args[1])
	if err != nil || seconds > int(math.MaxInt64/time.Second) {
		return nil, errNotInteger
	}
	if seconds < 0 {
		seconds = 0
	}
	ttl := time.Duration(seconds) * time.Second
	if store.Expire(args[0], ttl) {
		return integer(1), nil
	}
	return integer(0), nil
}

func ttlCommand(store *Store, args []string) (reply, error) {
	if len(args) != 1 {
		return nil, errWrongArgs("ttl")
	}
	remaining, hasExpiry, ok := store.TTL(args[0])
	if !ok {
		return integer(-2), nil
	}
	if !hasExpiry {
		return integer(-1), nil
	}
	seconds := remaining / time.Second
	if remaining%time.Second != 0 {
		seconds++
	}
	return integer(seconds), nil
}

func errWrongArgs(command string) error {
	return fmt.Errorf("wrong number of arguments for '%s' command", command)
}
