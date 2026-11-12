package main

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type commandFunc func(store *Store, args []string) (string, error)

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

func execute(store *Store, line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "ERR empty command"
	}

	cmd, ok := commands[strings.ToLower(fields[0])]
	if !ok {
		return fmt.Sprintf("ERR unknown command '%s'", fields[0])
	}

	reply, err := cmd(store, fields[1:])
	if err != nil {
		return "ERR " + err.Error()
	}
	return reply
}

func getCommand(store *Store, args []string) (string, error) {
	if len(args) != 1 {
		return "", errWrongArgs("get")
	}
	if value, ok := store.Get(args[0]); ok {
		return value, nil
	}
	return "(nil)", nil
}

func setCommand(store *Store, args []string) (string, error) {
	if len(args) != 2 {
		return "", errWrongArgs("set")
	}
	store.Set(args[0], args[1])
	return "OK", nil
}

func delCommand(store *Store, args []string) (string, error) {
	if len(args) == 0 {
		return "", errWrongArgs("del")
	}
	return strconv.Itoa(store.Delete(args...)), nil
}

func echoCommand(_ *Store, args []string) (string, error) {
	if len(args) != 1 {
		return "", errWrongArgs("echo")
	}
	return args[0], nil
}

func pingCommand(_ *Store, args []string) (string, error) {
	switch len(args) {
	case 0:
		return "PONG", nil
	case 1:
		return args[0], nil
	default:
		return "", errWrongArgs("ping")
	}
}

func expireCommand(store *Store, args []string) (string, error) {
	if len(args) != 2 {
		return "", errWrongArgs("expire")
	}
	seconds, err := strconv.Atoi(args[1])
	if err != nil || seconds > int(math.MaxInt64/time.Second) {
		return "", errNotInteger
	}
	if seconds < 0 {
		seconds = 0
	}
	ttl := time.Duration(seconds) * time.Second
	if store.Expire(args[0], ttl) {
		return "1", nil
	}
	return "0", nil
}

func ttlCommand(store *Store, args []string) (string, error) {
	if len(args) != 1 {
		return "", errWrongArgs("ttl")
	}
	remaining, hasExpiry, ok := store.TTL(args[0])
	if !ok {
		return "-2", nil
	}
	if !hasExpiry {
		return "-1", nil
	}
	seconds := remaining / time.Second
	if remaining%time.Second != 0 {
		seconds++
	}
	return strconv.Itoa(int(seconds)), nil
}

func errWrongArgs(command string) error {
	return fmt.Errorf("wrong number of arguments for '%s' command", command)
}
