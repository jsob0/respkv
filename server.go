package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"
)

const (
	acceptRetryDelay      = 100 * time.Millisecond
	defaultIdleTimeout    = 5 * time.Minute
	defaultWriteTimeout   = 5 * time.Second
	defaultExpireInterval = time.Second
)

type Server struct {
	addr           string
	logger         *slog.Logger
	idleTimeout    time.Duration
	writeTimeout   time.Duration
	store          *Store
	expireInterval time.Duration
}

func NewServer(addr string, logger *slog.Logger) *Server {
	return &Server{
		addr:           addr,
		logger:         logger,
		idleTimeout:    defaultIdleTimeout,
		writeTimeout:   defaultWriteTimeout,
		store:          NewStore(),
		expireInterval: defaultExpireInterval,
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	return s.Serve(ctx, listener)
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()

	cancelListenerClose := context.AfterFunc(ctx, func() {
		listener.Close()
	})
	defer cancelListenerClose()

	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() {
		s.store.RunExpiration(ctx, s.expireInterval)
	})

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
		wg.Go(func() {
			s.handleConnection(ctx, conn)
		})
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	logger := s.logger.With("remote_addr", conn.RemoteAddr().String())

	defer func() {
		conn.Close()
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

		// Redis ignores empty inline lines, and redis-cli --pipe sends one.
		if len(args) > 0 {
			r := execute(s.store, args)

			conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
			if _, err := writer.Write(r.encode()); err != nil {
				logger.Warn("write failed", "err", err)
				return
			}
		}
		if reader.Buffered() == 0 {
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
