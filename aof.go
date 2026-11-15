package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

type fsyncPolicy int

const (
	fsyncEverySec fsyncPolicy = iota
	fsyncAlways
	fsyncNo
)

var fsyncPolicyNames = map[fsyncPolicy]string{
	fsyncEverySec: "everysec",
	fsyncAlways:   "always",
	fsyncNo:       "no",
}

func (p fsyncPolicy) String() string {
	return fsyncPolicyNames[p]
}

func (p *fsyncPolicy) Set(name string) error {
	for policy, n := range fsyncPolicyNames {
		if n == name {
			*p = policy
			return nil
		}
	}
	return fmt.Errorf("unknown fsync policy %q, want always, everysec or no", name)
}

var (
	errAOFClosed  = errors.New("append-only file is closed")
	errAOFCorrupt = errors.New("append-only file is corrupt")
)

type AOF struct {
	mu     sync.Mutex
	file   *os.File
	w      *bufio.Writer
	policy fsyncPolicy
	err    error
}

func OpenAOF(path string, policy fsyncPolicy) (*AOF, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open append-only file: %w", err)
	}
	return &AOF{file: file, w: bufio.NewWriter(file), policy: policy}, nil
}

func (a *AOF) Append(args ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return
	}
	if _, err := a.w.Write(bulkArray(args).encode()); err != nil {
		a.err = err
		return
	}
	if a.policy == fsyncAlways {
		a.flushLocked(true)
	}
}

func (a *AOF) Flush() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.flushLocked(false)
}

func (a *AOF) Sync() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.flushLocked(a.policy != fsyncNo)
}

func (a *AOF) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if errors.Is(a.err, errAOFClosed) {
		return a.err
	}
	err := a.flushLocked(true)
	if closeErr := a.file.Close(); err == nil {
		err = closeErr
	}
	a.err = errAOFClosed
	return err
}

func (a *AOF) flushLocked(fsync bool) error {
	if a.err != nil {
		return a.err
	}
	if err := a.w.Flush(); err != nil {
		a.err = err
		return err
	}
	if fsync {
		if err := a.file.Sync(); err != nil {
			a.err = err
			return err
		}
	}
	return nil
}

type loadResult struct {
	commands       int
	truncatedBytes int64
}

func loadAOF(path string, store *Store) (loadResult, error) {
	var result loadResult

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("open append-only file: %w", err)
	}
	defer file.Close()

	counter := &countingReader{r: file}
	r := newCommandReader(counter)
	for {
		offset := counter.n - int64(r.Buffered())

		args, err := readCommand(r)
		switch {
		case errors.Is(err, io.EOF):
			return result, nil
		case errors.Is(err, io.ErrUnexpectedEOF):
			result.truncatedBytes = counter.n - offset
			if err := truncateFile(file, offset); err != nil {
				return result, err
			}
			return result, nil
		case errors.Is(err, errProtocol):
			return result, fmt.Errorf("%w at byte %d: %w", errAOFCorrupt, offset, err)
		case err != nil:
			return result, fmt.Errorf("read append-only file: %w", err)
		}

		if failed, ok := execute(store, args).(errorReply); ok {
			return result, fmt.Errorf("%w at byte %d: %s", errAOFCorrupt, offset, string(failed))
		}
		result.commands++
	}
}

func truncateFile(file *os.File, size int64) error {
	if err := file.Truncate(size); err != nil {
		return fmt.Errorf("truncate append-only file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync append-only file: %w", err)
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}
