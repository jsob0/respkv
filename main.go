package main

import (
	"flag"
	"log"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9000", "TCP address to listen on")
	flag.Parse()

	if err := NewServer(*addr).ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
