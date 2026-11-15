package main

import (
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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

func writeTestFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "appendonly.aof")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	return path
}

func assertStoredValues(t *testing.T, s *Store, want map[string]string) {
	t.Helper()

	assertStoredKeys(t, s, slices.Collect(maps.Keys(want))...)
	for key, value := range want {
		if got, found := s.Get(key); got != value || !found {
			t.Errorf("Get(%q) = %q, %v, want %q, true", key, got, found, value)
		}
	}
}

func TestLoadAOF(t *testing.T) {
	records := encodeCommand("SET", "a", "1") +
		encodeCommand("SET", "b", "hello world") +
		encodeCommand("SET", "c", "3") +
		encodeCommand("DEL", "a") +
		encodeCommand("PEXPIREAT", "b", "1000100000") +
		encodeCommand("PEXPIREAT", "c", "999999000")
	cutInsideBulk := "*3\r\n$3\r\nSET\r\n$1\r\nd\r\n$5\r\nhel"
	cutHeader := "*3\r\n$3\r\nSET"

	tests := []struct {
		name          string
		content       string
		want          map[string]string
		wantCommands  int
		wantTruncated int64
		wantContent   string
	}{
		{name: "empty file", content: "", want: map[string]string{}},
		{name: "records", content: records, want: map[string]string{"b": "hello world"}, wantCommands: 6, wantContent: records},
		{
			name:          "cut inside a bulk",
			content:       records + cutInsideBulk,
			want:          map[string]string{"b": "hello world"},
			wantCommands:  6,
			wantTruncated: int64(len(cutInsideBulk)),
			wantContent:   records,
		},
		{
			name:          "cut after the array header",
			content:       records + "*3\r\n",
			want:          map[string]string{"b": "hello world"},
			wantCommands:  6,
			wantTruncated: 4,
			wantContent:   records,
		},
		{
			name:          "cut inside the array header",
			content:       encodeCommand("SET", "a", "1") + "*",
			want:          map[string]string{"a": "1"},
			wantCommands:  1,
			wantTruncated: 1,
			wantContent:   encodeCommand("SET", "a", "1"),
		},
		{
			name:          "only a cut record",
			content:       cutHeader,
			want:          map[string]string{},
			wantTruncated: int64(len(cutHeader)),
			wantContent:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestFile(t, tt.content)
			s, _ := newStoreWithClock()

			got, err := loadAOF(path, s)
			if err != nil {
				t.Fatalf("loadAOF() err = %v", err)
			}
			if want := (loadResult{commands: tt.wantCommands, truncatedBytes: tt.wantTruncated}); got != want {
				t.Errorf("loadAOF() = %+v, want %+v", got, want)
			}
			assertStoredValues(t, s, tt.want)
			assertFileContent(t, path, tt.wantContent)
		})
	}
}

func TestLoadAOFMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	s := NewStore()

	got, err := loadAOF(path, s)
	if err != nil || got != (loadResult{}) {
		t.Errorf("loadAOF() = %+v, %v, want %+v, nil", got, err, loadResult{})
	}
	assertStoredKeys(t, s)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat() err = %v, want %v", err, os.ErrNotExist)
	}
}

func TestLoadAOFCorrupt(t *testing.T) {
	valid := encodeCommand("SET", "a", "1")

	tests := []struct {
		name    string
		content string
	}{
		{name: "bad bulk length", content: valid + "*1\r\n$abc\r\n" + valid},
		{name: "bulk without CRLF", content: valid + "*1\r\n$4\r\nPINGxx" + valid},
		{name: "unknown command", content: valid + encodeCommand("FOO", "a")},
		{name: "wrong number of arguments", content: valid + encodeCommand("SET", "a")},
		{name: "invalid expiration time", content: valid + encodeCommand("PEXPIREAT", "a", "soon")},
		{name: "empty line", content: valid + "\r\n"},
		{name: "text", content: "hello\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestFile(t, tt.content)

			if _, err := loadAOF(path, NewStore()); !errors.Is(err, errAOFCorrupt) {
				t.Errorf("loadAOF() err = %v, want %v", err, errAOFCorrupt)
			}
			assertFileContent(t, path, tt.content)
		})
	}
}

func TestLoadAOFReportsCorruptOffset(t *testing.T) {
	valid := encodeCommand("SET", "a", "1")
	path := writeTestFile(t, valid+encodeCommand("FOO"))

	_, err := loadAOF(path, NewStore())
	if want := "at byte " + strconv.Itoa(len(valid)) + ":"; err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("loadAOF() err = %v, want it to contain %q", err, want)
	}
}

func TestAppendAfterTruncatedLoad(t *testing.T) {
	valid := encodeCommand("SET", "a", "1")
	path := writeTestFile(t, valid+"*3\r\n$3\r\nSET\r\n$1")

	if _, err := loadAOF(path, NewStore()); err != nil {
		t.Fatalf("loadAOF() err = %v", err)
	}
	a, err := OpenAOF(path, fsyncEverySec)
	if err != nil {
		t.Fatalf("OpenAOF() err = %v", err)
	}
	a.Append("SET", "b", "2")
	if err := a.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	s := NewStore()
	if _, err := loadAOF(path, s); err != nil {
		t.Fatalf("second loadAOF() err = %v", err)
	}
	assertStoredValues(t, s, map[string]string{"a": "1", "b": "2"})
}

func TestAOFRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appendonly.aof")
	a, err := OpenAOF(path, fsyncEverySec)
	if err != nil {
		t.Fatalf("OpenAOF() err = %v", err)
	}

	s, _ := newStoreWithClock()
	s.aof = a
	s.Set("greeting", "hello world")
	s.Set("lines", "a\r\nb")
	s.Set("empty", "")
	s.Set("temp", "v")
	s.Expire("temp", time.Minute)
	s.Set("gone", "v")
	s.Expire("gone", 0)
	s.Set("deleted", "v")
	s.Delete("deleted")
	if err := a.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}

	restored, advance := newStoreWithClock()
	if _, err := loadAOF(path, restored); err != nil {
		t.Fatalf("loadAOF() err = %v", err)
	}
	assertStoredValues(t, restored, map[string]string{
		"greeting": "hello world",
		"lines":    "a\r\nb",
		"empty":    "",
		"temp":     "v",
	})
	if remaining, _, _ := restored.TTL("temp"); remaining != time.Minute {
		t.Errorf("TTL(%q) remaining = %v, want %v", "temp", remaining, time.Minute)
	}

	advance(time.Minute)
	if _, found := restored.Get("temp"); found {
		t.Errorf("Get(%q) found = true after its expiration time", "temp")
	}
}
