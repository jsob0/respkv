package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	acceptRetryDelay       = 100 * time.Millisecond
	defaultIdleTimeout     = 5 * time.Minute
	defaultWriteTimeout    = 5 * time.Second
	defaultExpireInterval  = time.Second
	defaultAOFSyncInterval = time.Second
	metricsHeaderTimeout   = 5 * time.Second
	metricsShutdownTimeout = 5 * time.Second
)

type Server struct {
	addr            string
	logger          *slog.Logger
	idleTimeout     time.Duration
	writeTimeout    time.Duration
	store           *Store
	expireInterval  time.Duration
	aofPath         string
	aofPolicy       fsyncPolicy
	aofSyncInterval time.Duration
	aof             *AOF
	metricsAddr     string
	metrics         *metrics
}

func NewServer(addr string, logger *slog.Logger) *Server {
	store := NewStore()
	return &Server{
		addr:            addr,
		logger:          logger,
		idleTimeout:     defaultIdleTimeout,
		writeTimeout:    defaultWriteTimeout,
		store:           store,
		expireInterval:  defaultExpireInterval,
		aofSyncInterval: defaultAOFSyncInterval,
		metrics:         newMetrics(store),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	if s.metricsAddr != "" {
		metricsListener, err := net.Listen("tcp", s.metricsAddr)
		if err != nil {
			listener.Close()
			return fmt.Errorf("start metrics server: %w", err)
		}
		stopMetrics := s.serveMetrics(metricsListener)
		defer stopMetrics()
	}

	return s.Serve(ctx, listener)
}

func (s *Server) serveMetrics(listener net.Listener) (stop func()) {
	server := &http.Server{
		Handler:           s.metrics.handler(),
		ReadHeaderTimeout: metricsHeaderTimeout,
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelWarn),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.logger.Info("metrics listening", "addr", listener.Addr().String())
		if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("metrics server failed", "err", err)
		}
	}()

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), metricsShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			server.Close()
		}
		<-done
	}
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()

	if s.aofPath != "" {
		if err := s.openAOF(); err != nil {
			return err
		}
		defer func() {
			if err := s.aof.Close(); err != nil {
				s.logger.Error("append-only file close failed", "err", err)
			}
		}()
	}

	cancelListenerClose := context.AfterFunc(ctx, func() {
		listener.Close()
	})
	defer cancelListenerClose()

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() {
		s.store.RunExpiration(ctx, s.expireInterval)
	})
	if s.aof != nil {
		wg.Go(func() {
			s.runAOFSync(ctx)
		})
	}

	s.logger.Info("server listening", "addr", listener.Addr().String())

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				s.logger.Info("server shutting down")
				return nil
			}
			s.logger.Error("accept failed", "err", err)
			time.Sleep(acceptRetryDelay)
			continue
		}
		s.metrics.connectionsTotal.Inc()
		wg.Go(func() {
			s.handleConnection(ctx, conn)
		})
	}
}

func (s *Server) openAOF() error {
	result, err := loadAOF(s.aofPath, s.store)
	if err != nil {
		return fmt.Errorf("load append-only file: %w", err)
	}
	if result.truncatedBytes > 0 {
		s.logger.Warn("append-only file had a truncated last record, removed it",
			"path", s.aofPath, "bytes", result.truncatedBytes)
	}
	s.logger.Info("append-only file loaded", "path", s.aofPath, "commands", result.commands)

	aof, err := OpenAOF(s.aofPath, s.aofPolicy)
	if err != nil {
		return err
	}
	s.aof = aof
	s.store.aof = aof
	s.metrics.registerAOF(aof)
	return nil
}

func (s *Server) runAOFSync(ctx context.Context) {
	ticker := time.NewTicker(s.aofSyncInterval)
	defer ticker.Stop()

	reported := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.aof.Sync(); err != nil && !reported {
				s.logger.Error("append-only file sync failed", "err", err)
				reported = true
			}
		}
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	logger := s.logger.With("remote_addr", conn.RemoteAddr().String())

	s.metrics.connectedClients.Inc()
	defer func() {
		conn.Close()
		s.metrics.connectedClients.Dec()
		logger.Info("client disconnected")
	}()

	cancelReadInterrupt := context.AfterFunc(ctx, func() {
		conn.SetReadDeadline(time.Now())
	})
	defer cancelReadInterrupt()

	logger.Info("client connected")

	reader := newCommandReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(s.idleTimeout))
		if ctx.Err() != nil {
			return
		}

		args, err := readCommand(reader)
		if err != nil {
			if errors.Is(err, errProtocol) {
				conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
				writer.Write(errorReply("ERR " + err.Error()).encode())
				writer.Flush()
			}
			logReadError(ctx, logger, err)
			return
		}

		if len(args) > 0 {
			start := time.Now()
			r := execute(s.store, args)
			s.metrics.observeCommand(args[0], time.Since(start))

			conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
			if _, err := writer.Write(r.encode()); err != nil {
				logger.Warn("write failed", "err", err)
				return
			}
		}
		if reader.Buffered() == 0 {
			if s.aof != nil {
				s.aof.Flush()
			}
			if err := writer.Flush(); err != nil {
				logger.Warn("write failed", "err", err)
				return
			}
		}
	}
}

func logReadError(ctx context.Context, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, io.EOF), ctx.Err() != nil:
	case errors.Is(err, errProtocol):
		logger.Warn("protocol error", "err", err)
	case errors.Is(err, io.ErrUnexpectedEOF):
		logger.Info("client disconnected mid-command")
	case errors.Is(err, os.ErrDeadlineExceeded):
		logger.Info("idle timeout")
	default:
		logger.Warn("read failed", "err", err)
	}
}
