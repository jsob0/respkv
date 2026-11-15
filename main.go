package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9000", "TCP address to listen on")
	aofPath := flag.String("aof", "", "append-only file path; empty disables persistence")
	var policy fsyncPolicy
	flag.Var(&policy, "appendfsync", "fsync policy for the append-only file: always, everysec or no")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := NewServer(*addr, logger)
	server.aofPath = *aofPath
	server.aofPolicy = policy
	if err := server.ListenAndServe(ctx); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
