package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

const (
	acceptRetryDelay = 100 * time.Millisecond
	idleTimeout      = 5 * time.Minute
	writeTimeout     = 5 * time.Second
)

type Server struct {
	addr string
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	defer listener.Close()

	cancelListenerClose := context.AfterFunc(ctx, func() {
		listener.Close()
	})
	defer cancelListenerClose()

	var wg sync.WaitGroup
	defer wg.Wait()

	log.Printf("listening on %s", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				log.Printf("shutting down, waiting for active connections")
				return nil
			}
			log.Printf("accept: %v", err)
			time.Sleep(acceptRetryDelay)
			continue
		}
		wg.Go(func() {
			s.handleConnection(ctx, conn)
		})
	}
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() {
		conn.Close()
		log.Printf("client disconnected: %s", conn.RemoteAddr())
	}()

	cancelReadInterrupt := context.AfterFunc(ctx, func() {
		conn.SetReadDeadline(time.Now())
	})
	defer cancelReadInterrupt()

	log.Printf("client connected: %s", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(idleTimeout))
		if ctx.Err() != nil {
			return
		}
		if !scanner.Scan() {
			break
		}

		conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := fmt.Fprintln(conn, scanner.Text()); err != nil {
			log.Printf("write to %s: %v", conn.RemoteAddr(), err)
			return
		}
	}

	err := scanner.Err()
	switch {
	case err == nil || ctx.Err() != nil:
	case errors.Is(err, os.ErrDeadlineExceeded):
		log.Printf("idle timeout: %s", conn.RemoteAddr())
	default:
		log.Printf("read from %s: %v", conn.RemoteAddr(), err)
	}
}
