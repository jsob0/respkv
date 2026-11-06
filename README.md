# respkv

[![CI](https://github.com/jsob0/respkv/actions/workflows/ci.yml/badge.svg)](https://github.com/jsob0/respkv/actions/workflows/ci.yml)

A Redis-inspired key-value store in Go, built from the TCP layer up.

> **Status: early development.** The server currently handles TCP connections
> and echoes back every line it receives. Commands, storage and the RESP protocol
> are planned; see the [roadmap](#roadmap).

## Features

- Concurrent client handling, one goroutine per connection
- Graceful shutdown on `SIGINT` and `SIGTERM`: stops accepting new connections
  and waits for active ones to finish
- Idle timeout (5 minutes) and write timeout (5 seconds), so slow or silent
  clients cannot hold a connection forever
- Line length limit of 64 KB, including the newline
- Structured JSON logging with `log/slog`
- Integration tests run with the race detector in CI

## Getting started

Requires Go 1.27 or later.

```bash
git clone git@github.com:jsob0/respkv.git
cd respkv
go run .
```

In another terminal, connect with `nc` and type a few lines:

```console
$ nc localhost 9000
hello
hello
ping
ping
```

Press `Ctrl+C` in the server terminal to shut it down. Logs are written to
stderr as JSON:

```json
{"time":"2026-11-06T18:41:41.015324873+01:00","level":"INFO","msg":"server listening","addr":"127.0.0.1:9000"}
{"time":"2026-11-06T18:41:41.428971597+01:00","level":"INFO","msg":"client connected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.429230093+01:00","level":"INFO","msg":"client disconnected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.634610905+01:00","level":"INFO","msg":"server shutting down"}
```

### Options

| Flag    | Default          | Description                 |
|---------|------------------|-----------------------------|
| `-addr` | `127.0.0.1:9000` | TCP address to listen on    |

By default the server only accepts connections from the local machine. Use
`-addr :9000` to listen on all interfaces.

## Testing

```bash
go test -race ./...
```

The tests start a real server on a random port and talk to it over TCP. They
cover echoing, lines split across writes, concurrent clients, timeouts, the
line length limit and graceful shutdown.

## Roadmap

- [x] TCP echo server with concurrent connections
- [x] Graceful shutdown, timeouts and structured logging
- [x] Integration tests and CI
- [ ] Text commands: `PING`, `ECHO`
- [ ] Storage: `SET`, `GET`, `DEL` and key expiration (TTL)
- [ ] RESP protocol, compatible with `redis-cli`
- [ ] Persistence with an append-only file
- [ ] Metrics, profiling and benchmarks
- [ ] Docker image and TLS

## License

[MIT](LICENSE)
