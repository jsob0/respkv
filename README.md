# respkv

[![CI](https://github.com/jsob0/respkv/actions/workflows/ci.yml/badge.svg)](https://github.com/jsob0/respkv/actions/workflows/ci.yml)

A Redis-inspired key-value store in Go, built from the TCP layer up.

> **Status: early development.** The server handles the `PING`, `ECHO`, `SET`,
> `GET`, `DEL`, `EXPIRE`, `PEXPIREAT` and `TTL` commands over the Redis protocol
> (RESP), so it works with `redis-cli`, and can persist data to an append-only
> file. Metrics and benchmarks are next; see the [roadmap](#roadmap).

## Features

- In-memory key-value store shared by all clients and safe for concurrent use
- Key expiration: expired keys are hidden as soon as they expire and removed
  from memory by a background task every second
- Optional persistence with an append-only file (AOF), replayed on startup, with
  the `always`, `everysec` and `no` fsync policies known from Redis
- RESP2 protocol: commands as arrays of bulk strings, as sent by `redis-cli`
  and Redis client libraries, plus inline commands typed by hand
- Case-insensitive command names; keys and values can hold any bytes, including
  spaces and line breaks
- Pipelining: replies are buffered and sent together once all received commands
  have been handled
- Only complete commands are executed, so a command cut off by a disconnect or
  timeout is discarded
- A malformed command gets an error reply and the connection is closed
- Concurrent client handling, one goroutine per connection
- Graceful shutdown on `SIGINT` and `SIGTERM`: stops accepting new connections
  and waits for active ones to finish
- Idle timeout (5 minutes) and write timeout (5 seconds), so slow or silent
  clients cannot hold a connection forever
- Limits: 1024 arguments per command, 1 MiB per argument and 64 KiB per inline
  command, including the newline
- Structured JSON logging with `log/slog`
- Optional Prometheus metrics and `pprof` profiling on a separate HTTP port
- Unit and integration tests run with the race detector in CI

## Getting started

Requires Go 1.27 or later.

```bash
git clone git@github.com:jsob0/respkv.git
cd respkv
go run .
```

In another terminal, connect with `redis-cli`. If it is not installed, run it
from the Redis Docker image (`--network host` works on Linux):

```bash
redis-cli -p 9000
# or
docker run --rm -it --network host redis:7 redis-cli -p 9000
```

```console
127.0.0.1:9000> PING
PONG
127.0.0.1:9000> SET greeting "hello world"
OK
127.0.0.1:9000> GET greeting
"hello world"
127.0.0.1:9000> SET empty ""
OK
127.0.0.1:9000> GET empty
""
127.0.0.1:9000> GET missing
(nil)
127.0.0.1:9000> EXPIRE greeting 60
(integer) 1
127.0.0.1:9000> TTL greeting
(integer) 60
127.0.0.1:9000> DEL greeting empty other
(integer) 2
127.0.0.1:9000> TTL greeting
(integer) -2
127.0.0.1:9000> ECHO
(error) ERR wrong number of arguments for 'echo' command
127.0.0.1:9000> FOO
(error) ERR unknown command 'FOO'
```

Bulk loading with `redis-cli --pipe` works too:

```console
$ for i in $(seq 10000); do printf 'SET key:%d value%d\r\n' $i $i; done | redis-cli -p 9000 --pipe
All data transferred. Waiting for the last reply...
Last reply received from server.
errors: 0, replies: 10000
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

| Command                  | Reply                                                                               |
|--------------------------|-------------------------------------------------------------------------------------|
| `PING`                   | Simple string `PONG`                                                                |
| `PING <message>`         | Bulk string `<message>`                                                             |
| `ECHO <message>`         | Bulk string `<message>`                                                             |
| `SET <key> <value>`      | Simple string `OK`; any expiration on the key is cleared                            |
| `GET <key>`              | Bulk string with the value, or a null bulk string if there is none                  |
| `DEL <key> [key...]`     | Integer: number of keys removed                                                     |
| `EXPIRE <key> <seconds>` | Integer: `1` if the timeout was set, `0` if the key does not exist                  |
| `PEXPIREAT <key> <ms>`   | Like `EXPIRE`, but with an absolute Unix time in milliseconds                       |
| `TTL <key>`              | Integer: seconds left, `-1` if the key has no expiration, `-2` if it does not exist |

`EXPIRE` with zero or a negative number of seconds, and `PEXPIREAT` with a time
in the past, delete the key. `TTL` rounds up to whole seconds.

Errors are returned as RESP errors starting with `ERR`. Empty inline commands
are ignored, as in Redis. Without `-aof`, data is lost when the server stops.

### Options

| Flag            | Default          | Description                                                      |
|-----------------|------------------|------------------------------------------------------------------|
| `-addr`         | `127.0.0.1:9000` | TCP address to listen on                                         |
| `-aof`          | empty            | Append-only file path; empty disables persistence                |
| `-appendfsync`  | `everysec`       | When to fsync the append-only file: `always`, `everysec` or `no` |
| `-metrics-addr` | empty            | HTTP address for metrics and `pprof`; empty disables it          |

By default the server only accepts connections from the local machine. Use
`-addr :9000` to listen on all interfaces.

## Persistence

With `-aof`, every change is appended to the file as a RESP command before the
reply is sent, and the file is replayed when the server starts:

```console
$ go run . -aof appendonly.aof
$ redis-cli -p 9000 SET greeting "hello world"
OK
$ redis-cli -p 9000 SET session abc
OK
$ redis-cli -p 9000 EXPIRE session 3600
(integer) 1
```

After a restart with the same file:

```console
$ redis-cli -p 9000 GET greeting
"hello world"
$ redis-cli -p 9000 TTL session
(integer) 3599
```

```json
{"time":"2026-11-15T18:16:58.459157989+01:00","level":"INFO","msg":"append-only file loaded","path":"appendonly.aof","commands":3}
```

How it works:

- Only changes are logged: `SET`, `DEL` with the keys that were actually
  deleted, and expirations as `PEXPIREAT` with an absolute time, so a key does
  not live longer because the server was down.
- Records are appended while the store holds its lock, so the file order always
  matches the order in which changes were applied.
- Records are handed to the operating system before the reply is sent, so a
  crash of the server process loses no acknowledged write. The fsync policy
  decides what survives a power loss:

  | Policy     | fsync                   | Lost on power loss                               |
  |------------|-------------------------|--------------------------------------------------|
  | `always`   | after every change      | nothing                                          |
  | `everysec` | once a second (default) | up to about 1 second                             |
  | `no`       | left to the OS          | up to the OS (Linux usually writes within ~30 s) |

- If the last record is incomplete, for example after a crash in the middle of a
  write, it is removed and a warning is logged. Damage anywhere else stops the
  server with an error that gives the byte offset, and the file is left
  untouched.
- The file only grows; rewriting it to a compact form is not implemented yet.

Checked by hand: 10,000 `SET` commands loaded with `redis-cli --pipe` and the
server killed with `kill -9` right after the last reply. All 10,000 keys were
back after a restart with each fsync policy.

## Metrics and profiling

Start the server with `-metrics-addr` to serve Prometheus metrics and Go's
`pprof` profiles over HTTP, on a port separate from the clients:

```bash
go run . -metrics-addr 127.0.0.1:9100
curl -s 127.0.0.1:9100/metrics | grep '^respkv_'
```

| Metric                                       | Type      | Description                                                     |
|----------------------------------------------|-----------|-----------------------------------------------------------------|
| `respkv_command_duration_seconds{command}`   | histogram | Time spent executing commands; `_count` is the number of calls  |
| `respkv_connected_clients`                   | gauge     | Open client connections                                         |
| `respkv_connections_total`                   | counter   | Client connections accepted since start                         |
| `respkv_keys`                                | gauge     | Keys in the store, including expired keys not removed yet       |
| `respkv_aof_failed`                          | gauge     | 1 when writing the append-only file failed; only with `-aof`    |

Unknown commands are counted under `command="unknown"`, so clients cannot
create new label values. Go runtime and process metrics (`go_*`, `process_*`)
are included too. Measuring commands adds about 0.1 µs per command, mostly the
two clock reads.

Profiles are served under `/debug/pprof/`, for example a 10-second CPU profile:

```bash
go tool pprof -http :8080 'http://127.0.0.1:9100/debug/pprof/profile?seconds=10'
```

The endpoint has no authentication, so keep it on `127.0.0.1` or a private
network.

## Testing

```bash
go test -race ./...
```

Unit tests cover the RESP encoder and parser, commands and their replies, and
the store, including concurrent access. Parser tests feed every input both at
once and one byte at a time. Expiration unit tests use a fake clock or
`testing/synctest`, so they do not wait in real time. Integration tests start a
real server on a random port and talk to it over TCP. They cover commands,
commands split across writes, pipelining, protocol errors, incomplete commands,
concurrent clients, data shared between clients, background removal of expired
keys, timeouts, the length limits, graceful shutdown and persistence across a
restart. Append-only file tests write to temporary directories and cover the
exact file contents, the fsync policies, concurrent appends, kept write and
fsync errors, truncated last records and corrupt files. Metrics tests read
the `/metrics` output after real client sessions and cover the `pprof` handlers.

The parser also has fuzz tests:

```bash
go test -fuzz=FuzzReadCommand$ -fuzztime=30s
go test -fuzz=FuzzReadCommandRoundTrip -fuzztime=30s
```

## Roadmap

- [x] TCP echo server with concurrent connections
- [x] Graceful shutdown, timeouts and structured logging
- [x] Integration tests and CI
- [x] Text commands: `PING`, `ECHO`
- [x] In-memory storage: `SET`, `GET`, `DEL`
- [x] Key expiration (TTL)
- [x] RESP protocol, compatible with `redis-cli`
- [x] Persistence with an append-only file
- [ ] Metrics, profiling and benchmarks
- [ ] Docker image and TLS

## License

[MIT](LICENSE)
