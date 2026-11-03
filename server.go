package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

const (
	acceptRetryDelay = 100 * time.Millisecond
	shutdownTimeout  = 5 * time.Second
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

	cancelDeadlines := context.AfterFunc(ctx, func() {
		conn.SetReadDeadline(time.Now())
		conn.SetWriteDeadline(time.Now().Add(shutdownTimeout))
	})
	defer cancelDeadlines()

	log.Printf("client connected: %s", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(conn, scanner.Text()); err != nil {
			log.Printf("write to %s: %v", conn.RemoteAddr(), err)
			return
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		log.Printf("read from %s: %v", conn.RemoteAddr(), err)
	}
}
