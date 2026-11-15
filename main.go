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
	metricsAddr := flag.String("metrics-addr", "", "HTTP address for Prometheus metrics and pprof; empty disables it")
	tlsCert := flag.String("tls-cert", "", "TLS certificate file in PEM format; enables TLS together with -tls-key")
	tlsKey := flag.String("tls-key", "", "TLS private key file in PEM format")
	var policy fsyncPolicy
	flag.Var(&policy, "appendfsync", "fsync policy for the append-only file: always, everysec or no")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	tlsConfig, err := loadTLSConfig(*tlsCert, *tlsKey)
	if err != nil {
		logger.Error("invalid TLS configuration", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := NewServer(*addr, logger)
	server.aofPath = *aofPath
	server.aofPolicy = policy
	server.metricsAddr = *metricsAddr
	server.tlsConfig = tlsConfig
	if err := server.ListenAndServe(ctx); err != nil {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
