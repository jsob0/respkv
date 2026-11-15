package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sync"
)

type fsyncPolicy int

const (
	fsyncAlways fsyncPolicy = iota
	fsyncEverySec
	fsyncNo
)

var errAOFClosed = errors.New("append-only file is closed")

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
