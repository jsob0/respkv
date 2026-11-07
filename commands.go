package main

import (
	"fmt"
	"strings"
)

type commandFunc func(args []string) (string, error)

var commands = map[string]commandFunc{
	"echo": echoCommand,
	"ping": pingCommand,
}

func execute(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "ERR empty command"
	}

	cmd, ok := commands[strings.ToLower(fields[0])]
	if !ok {
		return fmt.Sprintf("ERR unknown command '%s'", fields[0])
	}

	reply, err := cmd(fields[1:])
	if err != nil {
		return "ERR " + err.Error()
	}
	return reply
}

func echoCommand(args []string) (string, error) {
	if len(args) != 1 {
		return "", errWrongArgs("echo")
	}
	return args[0], nil
}

func pingCommand(args []string) (string, error) {
	switch len(args) {
	case 0:
		return "PONG", nil
	case 1:
		return args[0], nil
	default:
		return "", errWrongArgs("ping")
	}
}

func errWrongArgs(command string) error {
	return fmt.Errorf("wrong number of arguments for '%s' command", command)
}
