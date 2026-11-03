package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
)

func main() {
	listener, err := net.Listen("tcp", ":9000")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	log.Printf("listening on %s", listener.Addr())

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept: %s", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Printf("client connected: %s", conn.RemoteAddr())

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(conn, scanner.Text()); err != nil {
			log.Printf("write: %v", err)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("read: %v", err)
	}

	log.Printf("client disconnected: %s", conn.RemoteAddr())
}
