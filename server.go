package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
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

	scanner := bufio.NewScanner(conn)
	scanner.Split(scanCompleteLines)
	for {
		conn.SetReadDeadline(time.Now().Add(s.idleTimeout))
		if ctx.Err() != nil {
			return
		}
		if !scanner.Scan() {
			break
		}

		reply := execute(s.store, scanner.Text())

		conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
		if _, err := fmt.Fprintln(conn, reply); err != nil {
			logger.Warn("write failed", "err", err)
			return
		}
	}

	err := scanner.Err()
	switch {
	case err == nil || ctx.Err() != nil:
	case errors.Is(err, os.ErrDeadlineExceeded):
		logger.Info("idle timeout")
	default:
		logger.Warn("read failed", "err", err)
	}
}

func scanCompleteLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if bytes.IndexByte(data, '\n') < 0 {
		return 0, nil, nil
	}
	return bufio.ScanLines(data, atEOF)
}
