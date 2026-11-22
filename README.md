# respkv

[![CI](https://github.com/jsob0/respkv/actions/workflows/ci.yml/badge.svg)](https://github.com/jsob0/respkv/actions/workflows/ci.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/jsob0/respkv)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A Redis-inspired key-value store in Go, built from the TCP layer up.

respkv speaks the Redis protocol (RESP), so `redis-cli`, `redis-benchmark` and
Redis client libraries work with it. It keeps data in memory with key
expiration, can persist it to an append-only file, accepts TLS connections and
exposes Prometheus metrics. Apart from the Prometheus client, it uses only the
Go standard library.

Supported commands: `PING`, `ECHO`, `SET`, `GET`, `DEL`, `EXPIRE`, `PEXPIREAT`
and `TTL`.

## Contents

- [Features](#features)
- [Getting started](#getting-started)
- [Commands](#commands)
- [Options](#options)
- [Docker](#docker)
- [Persistence](#persistence)
- [TLS](#tls)
- [Metrics and profiling](#metrics-and-profiling)
- [WebAssembly](#webassembly)
- [Performance](#performance)
- [Design](#design)
- [Testing](#testing)
- [Roadmap](#roadmap)

## Features

Protocol:

- RESP2: commands as arrays of bulk strings, as sent by `redis-cli` and Redis
  client libraries, plus inline commands typed by hand
- Pipelining: replies are buffered and sent together once all received commands
  have been handled
- Case-insensitive command names; keys and values can hold any bytes, including
  spaces and line breaks
- Only complete commands are executed, so a command cut off by a disconnect or
  timeout is discarded
- A malformed command gets an error reply and the connection is closed
- Limits: 1024 arguments per command, 1 MiB per argument and 64 KiB per inline
  command, including the newline

Storage:

- In-memory key-value store shared by all clients and safe for concurrent use,
  with one goroutine per connection
- Key expiration: expired keys are hidden as soon as they expire and removed
  from memory by a background task every second
- Optional persistence with an append-only file (AOF), replayed on startup, with
  the `always`, `everysec` and `no` fsync policies known from Redis

Operations:

- Graceful shutdown on `SIGINT` and `SIGTERM`: stops accepting new connections
  and waits for active ones to finish
- Idle timeout (5 minutes) and write timeout (5 seconds), so slow or silent
  clients cannot hold a connection forever
- Optional TLS, compatible with `redis-cli --tls`
- Optional Prometheus metrics and `pprof` profiling on a separate HTTP port
- Structured JSON logging with `log/slog`
- Docker image with a static binary on a distroless base, running as a non-root
  user
- WebAssembly build of the store and commands for running in a browser
- Unit, integration and fuzz tests; CI runs them with the race detector and
  checks the Docker image

## Getting started

Requires Go 1.27 or later.

```bash
git clone git@github.com:jsob0/respkv.git
cd respkv
go run .
```

Or with Docker, without installing Go (see [Docker](#docker)):

```bash
docker build -t respkv .
docker run --rm -p 127.0.0.1:9000:6379 respkv
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
{"time":"2026-11-06T18:41:41.015324873+01:00","level":"INFO","msg":"server listening","addr":"127.0.0.1:9000","tls":false}
{"time":"2026-11-06T18:41:41.428971597+01:00","level":"INFO","msg":"client connected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.429230093+01:00","level":"INFO","msg":"client disconnected","remote_addr":"127.0.0.1:46576"}
{"time":"2026-11-06T18:41:41.634610905+01:00","level":"INFO","msg":"server shutting down"}
```

## Commands

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

`SET` takes no options such as `EX` or `NX`; use `EXPIRE` to set a timeout.
`EXPIRE` with zero or a negative number of seconds, and `PEXPIREAT` with a time
in the past, delete the key. `TTL` rounds up to whole seconds.

Errors are returned as RESP errors starting with `ERR`. Empty inline commands
are ignored, as in Redis. Without `-aof`, data is lost when the server stops.

## Options

| Flag            | Default          | Description                                                      |
|-----------------|------------------|------------------------------------------------------------------|
| `-addr`         | `127.0.0.1:9000` | TCP address to listen on                                         |
| `-aof`          | empty            | Append-only file path; empty disables persistence                |
| `-appendfsync`  | `everysec`       | When to fsync the append-only file: `always`, `everysec` or `no` |
| `-metrics-addr` | empty            | HTTP address for metrics and `pprof`; empty disables it          |
| `-tls-cert`     | empty            | TLS certificate file (PEM); enables TLS together with `-tls-key` |
| `-tls-key`      | empty            | TLS private key file (PEM)                                       |

By default the server only accepts connections from the local machine. Use
`-addr :9000` to listen on all interfaces.

## Docker

The image is built in two stages: a static binary compiled in the `golang`
image is copied onto a distroless base. The result is about 22 MB and runs as a
non-root user. By default the server listens on port 6379 and keeps the
append-only file in the `/data` volume:

```bash
docker build -t respkv .
docker run -d --name respkv -p 127.0.0.1:6379:6379 -v respkv-data:/data respkv
docker run --rm -it --network host redis:7 redis-cli
```

`docker stop` sends `SIGTERM`, so the server shuts down gracefully and flushes
the append-only file. Arguments after the image name replace the defaults, for
example to add metrics and TLS:

```bash
docker run -d -p 127.0.0.1:6379:6379 -p 127.0.0.1:9100:9100 \
  -v respkv-data:/data -v "$PWD/certs:/certs:ro" respkv \
  -addr :6379 -aof /data/appendonly.aof -metrics-addr :9100 \
  -tls-cert /certs/cert.pem -tls-key /certs/key.pem
```

The server runs as UID 65532, so the key file must be readable by that user,
for example with `chmod 644 key.pem` for a local test certificate. On systems
with SELinux, add `,z` to the volume options.

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

## TLS

Pass a certificate and its private key in PEM format to accept only TLS
connections. A self-signed certificate for local testing:

```bash
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 \
  -keyout key.pem -out cert.pem -subj /CN=localhost \
  -addext subjectAltName=IP:127.0.0.1,DNS:localhost
go run . -tls-cert cert.pem -tls-key key.pem
```

```bash
docker run --rm -it --network host -v "$PWD:/certs:ro" redis:7 \
  redis-cli -p 9000 --tls --cacert /certs/cert.pem
```

TLS 1.2 is the minimum version. Clients that do not speak TLS or do not trust
the certificate are disconnected. The metrics port stays plain HTTP.

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

## WebAssembly

The store and commands also compile to WebAssembly, for example for an
in-browser playground. The browser build has no network server, persistence,
TLS or metrics: it keeps one in-memory store per page and runs inline commands
against it, with the same replies and key expiration as the server.

```bash
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o respkv.wasm .
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" .
```

`wasm_exec.js` must come from the same Go version that built `respkv.wasm`. The
module is about 2.7 MB, or 0.8 MB with gzip.

Every `v*` tag pushed to GitHub attaches both files and their `SHA256SUMS` to
the release of that tag, creating the release if it does not exist yet. GitHub
serves release downloads without CORS headers, so download them when building
the site rather than fetching them from the browser:

```bash
curl -fsSLO https://github.com/jsob0/respkv/releases/latest/download/respkv.wasm
curl -fsSLO https://github.com/jsob0/respkv/releases/latest/download/wasm_exec.js
```

The module defines a global `respkv.exec(line)`. It takes one line typed as in
`redis-cli` and returns `null` for a blank line, or an object with the reply
formatted like `redis-cli` (`text`) and encoded as RESP (`resp`):

```js
import "./wasm_exec.js";

const go = new Go();
const { instance } = await WebAssembly.instantiateStreaming(
  fetch("/respkv.wasm"),
  go.importObject,
);
go.run(instance);

respkv.exec("SET greeting hello"); // { text: "OK", resp: "+OK\r\n" }
respkv.exec("GET greeting");       // { text: "\"hello\"", resp: "$5\r\nhello\r\n" }
respkv.exec("TTL greeting");       // { text: "(integer) -1", resp: ":-1\r\n" }
```

Do not `await` `go.run`: its promise resolves only when the Go program exits,
and the program keeps running to remove expired keys. `respkv` is defined as
soon as `go.run` returns. As with inline commands sent to the server, arguments
are split on whitespace and quotes are not supported.

## Performance

Measured with `redis-benchmark` from the `redis:7` image: 50 clients, 100,000
random keys, 64-byte values, with and without pipelining 16 commands. Requests
per second:

```bash
docker run --rm --network host redis:7 redis-benchmark -p 9000 \
  -t set,get -n 1000000 -c 50 -r 100000 -d 64 -P 16 -q
```

| Server                        | SET     | GET     | SET, pipelined | GET, pipelined |
|-------------------------------|---------|---------|----------------|----------------|
| respkv                        | 241,000 | 257,000 | 1,420,000      | 2,690,000      |
| respkv, AOF `everysec`        | 236,000 | 256,000 | 564,000        | 2,540,000      |
| Redis 7                       | 288,000 | 307,000 | 1,890,000      | 2,020,000      |
| Redis 7, AOF `everysec`       | 298,000 | 307,000 | 1,090,000      | 1,990,000      |

Machine: AMD Ryzen AI 7 PRO 350 (8 cores, 16 threads), Linux, Go 1.27.3,
power profile `balanced`, client and server on the same host over loopback,
append-only file on an SSD with btrfs. Redis ran in Docker with `--save ''`.

Without pipelining, both servers spend most of the time on the network round
trip and system calls. With pipelining, respkv reads run on all cores, so
pipelined `GET` is faster than in Redis, which executes commands on one thread.
Writes take the store lock and, with the append-only file, also encode the
record while holding it. A CPU profile under pipelined `SET` with the
append-only file showed the process using less than two cores. That makes
writes the slowest case.

Go benchmarks cover the parser, reply encoding, commands, the store under
parallel load, append-only file writes and the whole server over TCP:

```bash
go test -run '^$' -bench . -benchmem
```

## Design

All code is in one `main` package. Files that need a network or the file system
for the server (`main.go`, `server.go`, `metrics.go`, `tls.go`) carry the
`!js` build constraint, and `main_js.go` replaces `main.go` in the WebAssembly
build:

| File            | Responsibility                                                          |
|-----------------|-------------------------------------------------------------------------|
| `main.go`       | Flags, logger and signal handling                                       |
| `server.go`     | Listener, one goroutine per connection, timeouts and graceful shutdown  |
| `resp.go`       | RESP parser and reply encoding                                          |
| `commands.go`   | Command table and argument checks                                       |
| `store.go`      | Map behind a `sync.RWMutex`, key expiration and the AOF hook            |
| `aof.go`        | Append-only file writer, fsync policies and replay on startup           |
| `metrics.go`    | Prometheus metrics and `pprof` handlers                                 |
| `tls.go`        | TLS configuration                                                       |
| `playground.go` | Inline commands and `redis-cli` style replies for the WebAssembly build |
| `main_js.go`    | WebAssembly entry point exposing `respkv.exec` to JavaScript            |

The path of a command: the connection goroutine reads it with a
`bufio.Reader`, looks it up in the command table and runs it against the store.
Changes are applied and appended to the append-only file under the store lock.
The reply goes to a `bufio.Writer`, which is flushed once no more input is
buffered, after the append-only file has been handed to the operating system.

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
TLS tests generate a self-signed certificate at runtime and talk to the server
through `crypto/tls`.

Tests that do not need the server also run as WebAssembly under Node.js:

```bash
PATH="$(go env GOROOT)/lib/wasm:$PATH" GOOS=js GOARCH=wasm go test ./...
```

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
- [x] Metrics, profiling and benchmarks
- [x] Docker image and TLS
- [ ] Rewriting the append-only file to a compact form
- [ ] More commands, such as `INCR`, `EXISTS` and `MGET`
- [ ] Quoted arguments in inline commands

## License

[MIT](LICENSE)
