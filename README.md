# respkv

[![CI](https://github.com/jsob0/respkv/actions/workflows/ci.yml/badge.svg)](https://github.com/jsob0/respkv/actions/workflows/ci.yml)

A Redis-inspired key-value store in Go, built from the TCP layer up.

> **Status: early development.** The server keeps data in memory and handles the
> `PING`, `ECHO`, `SET`, `GET`, `DEL`, `EXPIRE` and `TTL` commands over a simple
> line-based text protocol. The RESP protocol and persistence are planned; see
> the [roadmap](#roadmap).

## Features

- In-memory key-value store shared by all clients and safe for concurrent use
- Key expiration: expired keys are hidden as soon as they expire and removed
  from memory by a background task every second
- Line-based text protocol with case-insensitive command names
- Only complete lines are executed, so a command cut off by a disconnect or
  timeout is discarded
- Concurrent client handling, one goroutine per connection
- Graceful shutdown on `SIGINT` and `SIGTERM`: stops accepting new connections
  and waits for active ones to finish
- Idle timeout (5 minutes) and write timeout (5 seconds), so slow or silent
  clients cannot hold a connection forever
- Line length limit of 64 KB, including the newline
- Structured JSON logging with `log/slog`
- Unit and integration tests run with the race detector in CI

## Getting started

Requires Go 1.27 or later.

```bash
git clone git@github.com:jsob0/respkv.git
cd respkv
go run .
```

In another terminal, connect with `nc` and type a few commands:

```console
$ nc localhost 9000
PING
PONG
ping hello
hello
ECHO hi
hi
SET name respkv
OK
GET name
respkv
TTL name
-1
EXPIRE name 60
1
TTL name
60
DEL name other
1
GET name
(nil)
TTL name
-2
ECHO
ERR wrong number of arguments for 'echo' command
FOO
ERR unknown command 'FOO'
```

Press `Ctrl+C` in the server terminal to shut it down. Logs are written to
stderr as JSON:

```json
{"time":"2026-11-06T18:41:41.015324873+01:00","level":"INFO","msg":"server listening","addr":"127.0.0.1:9000"}
{"time":"2026-11-06T18:41:41.428971597+01:00","level":"INFO","msg":"client connected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.429230093+01:00","level":"INFO","msg":"client disconnected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.634610905+01:00","level":"INFO","msg":"server shutting down"}
```

### Commands

| Command                  | Reply                                                                      |
|--------------------------|----------------------------------------------------------------------------|
| `PING`                   | `PONG`                                                                     |
| `PING <message>`         | `<message>`                                                                |
| `ECHO <message>`         | `<message>`                                                                |
| `SET <key> <value>`      | `OK`, and any expiration on the key is cleared                             |
| `GET <key>`              | The value, or `(nil)` if there is none                                     |
| `DEL <key> [key...]`     | Number of keys removed                                                     |
| `EXPIRE <key> <seconds>` | `1` if the timeout was set, `0` if the key does not exist                  |
| `TTL <key>`              | Seconds left, `-1` if the key has no expiration, `-2` if it does not exist |

`EXPIRE` with zero or a negative number of seconds deletes the key. `TTL` rounds
up to whole seconds.

Arguments are separated by whitespace, so keys and values cannot contain spaces
yet. A stored value of `(nil)` looks the same as a missing key. Data is lost
when the server stops. Errors are returned as a line starting with `ERR`.

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

Unit tests cover command parsing and replies, as well as the store, including
concurrent access. Expiration unit tests use a fake clock or `testing/synctest`,
so they do not wait in real time. Integration tests start a real server on a
random port and talk to it over TCP. They cover commands, lines split across
writes, incomplete lines, concurrent clients, data shared between clients,
background removal of expired keys, timeouts, the line length limit and graceful
shutdown.

## Roadmap

- [x] TCP echo server with concurrent connections
- [x] Graceful shutdown, timeouts and structured logging
- [x] Integration tests and CI
- [x] Text commands: `PING`, `ECHO`
- [x] In-memory storage: `SET`, `GET`, `DEL`
- [x] Key expiration (TTL)
- [ ] RESP protocol, compatible with `redis-cli`
- [ ] Persistence with an append-only file
- [ ] Metrics, profiling and benchmarks
- [ ] Docker image and TLS

## License

[MIT](LICENSE)
