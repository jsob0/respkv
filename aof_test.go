package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
)

func openTestAOF(t *testing.T, policy fsyncPolicy) (*AOF, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "appendonly.aof")
	a, err := OpenAOF(path, policy)
	if err != nil {
		t.Fatalf("OpenAOF() err = %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a, path
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(got) != want {
		t.Errorf("file content = %q, want %q", got, want)
	}
}

func TestAOFAppend(t *testing.T) {
	a, path := openTestAOF(t, fsyncEverySec)

	a.Append("SET", "k", "hello world")
	a.Append("DEL", "k")
	if err := a.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	assertFileContent(t, path, encodeCommand("SET", "k", "hello world")+encodeCommand("DEL", "k"))
}

func TestAOFAlwaysWritesEachRecord(t *testing.T) {
	a, path := openTestAOF(t, fsyncAlways)

	a.Append("SET", "k", "v")

	assertFileContent(t, path, encodeCommand("SET", "k", "v"))
}

func TestAOFBuffersUntilSync(t *testing.T) {
	policies := []struct {
		name   string
		policy fsyncPolicy
	}{
		{name: "everysec", policy: fsyncEverySec},
		{name: "no", policy: fsyncNo},
	}

	for _, p := range policies {
		t.Run(p.name, func(t *testing.T) {
			a, path := openTestAOF(t, p.policy)

			a.Append("SET", "k", "v")
			assertFileContent(t, path, "")

			if err := a.Sync(); err != nil {
				t.Fatalf("Sync() err = %v", err)
			}
			assertFileContent(t, path, encodeCommand("SET", "k", "v"))
		})
	}
}

func TestAOFReopenAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")

	for _, value := range []string{"v1", "v2"} {
		a, err := OpenAOF(path, fsyncEverySec)
		if err != nil {
			t.Fatalf("OpenAOF() err = %v", err)
		}
		a.Append("SET", "k", value)
		if err := a.Close(); err != nil {
			t.Fatalf("Close() err = %v", err)
		}
	}

	assertFileContent(t, path, encodeCommand("SET", "k", "v1")+encodeCommand("SET", "k", "v2"))
}

func TestOpenAOFMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "appendonly.aof")

	if _, err := OpenAOF(path, fsyncEverySec); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("OpenAOF() err = %v, want %v", err, os.ErrNotExist)
	}
}

func TestAOFConcurrentAppendsDoNotInterleave(t *testing.T) {
	const goroutines, appends = 10, 100

	a, path := openTestAOF(t, fsyncNo)

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range appends {
				a.Append("SET", "key:"+strconv.Itoa(g), strconv.Itoa(i))
			}
		})
	}
	wg.Wait()
	if err := a.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer file.Close()

	next := make(map[string]int)
	r := newCommandReader(file)
	for {
		args, err := readCommand(r)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("readCommand() err = %v", err)
		}
		key := args[1]
		want := []string{"SET", key, strconv.Itoa(next[key])}
		if !slices.Equal(args, want) {
			t.Fatalf("record = %q, want %q", args, want)
		}
		next[key]++
	}

	for g := range goroutines {
		if got := next["key:"+strconv.Itoa(g)]; got != appends {
			t.Errorf("records for key:%d = %d, want %d", g, got, appends)
		}
	}
}

func TestAOFWriteErrorIsKept(t *testing.T) {
	a, _ := openTestAOF(t, fsyncAlways)
	a.file.Close()

	a.Append("SET", "k", "v")

	if err := a.Sync(); err == nil {
		t.Error("Sync() err = nil, want the write error")
	}
}

func TestAOFSyncErrorIsKept(t *testing.T) {
	a, _ := openTestAOF(t, fsyncEverySec)
	a.file.Close()

	if err := a.Sync(); err == nil {
		t.Fatal("Sync() err = nil, want an error")
	}

	healthy, err := os.Create(filepath.Join(t.TempDir(), "healthy"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { healthy.Close() })
	a.file = healthy

	if err := a.Sync(); err == nil {
		t.Error("Sync() after a failed fsync err = nil, want the first error")
	}
	if err := a.Close(); err == nil {
		t.Error("Close() after a failed fsync err = nil, want the first error")
	}
}

func TestAOFClose(t *testing.T) {
	a, path := openTestAOF(t, fsyncNo)

	a.Append("SET", "k", "v")
	if err := a.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}
	assertFileContent(t, path, encodeCommand("SET", "k", "v"))

	a.Append("SET", "k", "w")
	if err := a.Sync(); !errors.Is(err, errAOFClosed) {
		t.Errorf("Sync() after Close() err = %v, want %v", err, errAOFClosed)
	}
	if err := a.Close(); !errors.Is(err, errAOFClosed) {
		t.Errorf("second Close() err = %v, want %v", err, errAOFClosed)
	}
	assertFileContent(t, path, encodeCommand("SET", "k", "v"))
}
