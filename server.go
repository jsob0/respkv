package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

const acceptRetryDelay = 100 * time.Millisecond

type Server struct {
	addr string
}

func NewServer(addr string) *Server {
	return &Server{addr: addr}
}

func (s *Server) ListenAndServe() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}
	defer listener.Close()

	log.Printf("listening on %s", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept: %v", err)
			time.Sleep(acceptRetryDelay)
			continue
		}
		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		conn.Close()
		log.Printf("client disconnected: %s", conn.RemoteAddr())
	}()

	log.Printf("client connected: %s", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(conn, scanner.Text()); err != nil {
			log.Printf("write to %s: %v", conn.RemoteAddr(), err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("read from %s: %v", conn.RemoteAddr(), err)
	}
}
