package main

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func newStoreWithClock() (s *Store, advance func(time.Duration)) {
	now := time.Unix(1_000_000, 0)

	s = NewStore()
	s.now = func() time.Time { return now }
	advance = func(d time.Duration) { now = now.Add(d) }
	return s, advance
}

func TestSetAndGet(t *testing.T) {
	s := NewStore()

	s.Set("key", "value")
	if got, found := s.Get("key"); got != "value" || !found {
		t.Errorf("Get() = %q, %v, want %q, true", got, found, "value")
	}
}

func TestGetMissingKey(t *testing.T) {
	s := NewStore()

	if got, found := s.Get("missing"); got != "" || found {
		t.Errorf("Get() = %q, %v, want %q, false", got, found, "")
	}
}

func TestEmptyValueIsDistinctFromMissingKey(t *testing.T) {
	s := NewStore()

	s.Set("key", "")
	if got, found := s.Get("key"); got != "" || !found {
		t.Errorf("Get() = %q, %v, want %q, true", got, found, "")
	}
}

func TestSetOverwritesValue(t *testing.T) {
	s := NewStore()

	s.Set("key", "old")
	s.Set("key", "new")
	if got, found := s.Get("key"); got != "new" || !found {
		t.Errorf("Get() = %q, %v, want %q, true", got, found, "new")
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name string
		keys []string
		want int
	}{
		{name: "existing key", keys: []string{"a"}, want: 1},
		{name: "several existing keys", keys: []string{"a", "b"}, want: 2},
		{name: "missing key", keys: []string{"missing"}, want: 0},
		{name: "existing and missing keys", keys: []string{"a", "missing"}, want: 1},
		{name: "repeated key", keys: []string{"a", "a"}, want: 1},
		{name: "no keys", keys: nil, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewStore()
			stored := []string{"a", "b"}
			for _, key := range stored {
				s.Set(key, "value")
			}

			if got := s.Delete(tt.keys...); got != tt.want {
				t.Errorf("Delete(%q) = %d, want %d", tt.keys, got, tt.want)
			}

			for _, key := range stored {
				wantFound := !slices.Contains(tt.keys, key)
				if _, found := s.Get(key); found != wantFound {
					t.Errorf("Get(%q) found = %v, want %v", key, found, wantFound)
				}
			}
		})
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	const (
		goroutines = 50
		iterations = 1000
	)

	s := NewStore()
	sharedKeys := []string{"a", "b", "c"}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			s.Set("unique-"+strconv.Itoa(i), strconv.Itoa(i))

			for j := range iterations {
				key := sharedKeys[j%len(sharedKeys)]

				s.Set(key, "value")
				if got, found := s.Get(key); found && got != "value" {
					t.Errorf("Get(%q) = %q, want %q", key, got, "value")
				}
				s.Expire(key, time.Minute)
				if remaining, hasExpiry, _ := s.TTL(key); hasExpiry && remaining > time.Minute {
					t.Errorf("TTL(%q) = %v, want at most %v", key, remaining, time.Minute)
				}
				s.Delete(key)
			}
		})
	}
	wg.Wait()

	for i := range goroutines {
		key, want := "unique-"+strconv.Itoa(i), strconv.Itoa(i)
		if got, found := s.Get(key); got != want || !found {
			t.Errorf("Get(%q) = %q, %v, want %q, true", key, got, found, want)
		}
	}
}

func TestKeyExpiration(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("key", "value")
	s.Expire("key", 10*time.Second)

	advance(10*time.Second - time.Nanosecond)
	if got, found := s.Get("key"); got != "value" || !found {
		t.Errorf("Get() = %q, %v, want %q, true", got, found, "value")
	}

	advance(time.Nanosecond)
	if got, found := s.Get("key"); got != "" || found {
		t.Errorf("Get() = %q, %v, want %q, false", got, found, "")
	}
}

func TestSetClearsExpiration(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("key", "old")
	s.Expire("key", 10*time.Second)
	s.Set("key", "new")

	advance(time.Hour)
	if got, found := s.Get("key"); got != "new" || !found {
		t.Errorf("Get() = %q, %v, want %q, true", got, found, "new")
	}
}

func TestDeleteIgnoresExpiredKey(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("key", "value")
	s.Expire("key", 10*time.Second)
	advance(10 * time.Second)

	if got := s.Delete("key"); got != 0 {
		t.Errorf("Delete() = %d, want 0", got)
	}
}

func TestExpire(t *testing.T) {
	tests := []struct {
		name      string
		key       string
		ttl       time.Duration
		want      bool
		wantFound bool
	}{
		{name: "positive ttl", key: "key", ttl: 10 * time.Second, want: true, wantFound: true},
		{name: "zero ttl deletes", key: "key", ttl: 0, want: true, wantFound: false},
		{name: "negative ttl deletes", key: "key", ttl: -time.Second, want: true, wantFound: false},
		{name: "missing key", key: "missing", ttl: 10 * time.Second, want: false, wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newStoreWithClock()
			s.Set("key", "value")

			if got := s.Expire(tt.key, tt.ttl); got != tt.want {
				t.Errorf("Expire(%q, %v) = %v, want %v", tt.key, tt.ttl, got, tt.want)
			}
			if _, found := s.Get(tt.key); found != tt.wantFound {
				t.Errorf("Get(%q) found = %v, want %v", tt.key, found, tt.wantFound)
			}
		})
	}
}

func TestExpireExpiredKey(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("key", "value")
	s.Expire("key", 10*time.Second)
	advance(10 * time.Second)

	if got := s.Expire("key", 10*time.Second); got {
		t.Errorf("Expire() = %v, want false", got)
	}
}

func TestExpireAt(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		at         time.Duration
		want       bool
		wantFound  bool
		wantStored []string
	}{
		{name: "future", key: "key", at: 10 * time.Second, want: true, wantFound: true, wantStored: []string{"key"}},
		{name: "now deletes", key: "key", at: 0, want: true, wantFound: false},
		{name: "past deletes", key: "key", at: -time.Second, want: true, wantFound: false},
		{name: "missing key", key: "missing", at: 10 * time.Second, want: false, wantFound: false, wantStored: []string{"key"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newStoreWithClock()
			s.Set("key", "value")
			at := s.now().Add(tt.at)

			if got := s.ExpireAt(tt.key, at); got != tt.want {
				t.Errorf("ExpireAt(%q, %v) = %v, want %v", tt.key, at, got, tt.want)
			}
			if _, found := s.Get(tt.key); found != tt.wantFound {
				t.Errorf("Get(%q) found = %v, want %v", tt.key, found, tt.wantFound)
			}
			assertStoredKeys(t, s, tt.wantStored...)
			if !tt.wantFound {
				return
			}
			if remaining, _, _ := s.TTL(tt.key); remaining != tt.at {
				t.Errorf("TTL(%q) remaining = %v, want %v", tt.key, remaining, tt.at)
			}
		})
	}
}

func TestExpireAtExpiredKey(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("key", "value")
	s.Expire("key", 10*time.Second)
	advance(10 * time.Second)

	if got := s.ExpireAt("key", s.now().Add(10*time.Second)); got {
		t.Errorf("ExpireAt() = %v, want false", got)
	}
}

func TestTTL(t *testing.T) {
	tests := []struct {
		name          string
		set           bool
		ttl           time.Duration
		wantRemaining time.Duration
		wantHasExpiry bool
		wantOK        bool
	}{
		{name: "missing key", set: false},
		{name: "no expiration", set: true, wantOK: true},
		{name: "with expiration", set: true, ttl: 10 * time.Second, wantRemaining: 7 * time.Second, wantHasExpiry: true, wantOK: true},
		{name: "expired", set: true, ttl: 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, advance := newStoreWithClock()
			if tt.set {
				s.Set("key", "value")
			}
			if tt.ttl > 0 {
				s.Expire("key", tt.ttl)
			}
			advance(3 * time.Second)

			remaining, hasExpiry, ok := s.TTL("key")
			if remaining != tt.wantRemaining || hasExpiry != tt.wantHasExpiry || ok != tt.wantOK {
				t.Errorf("TTL() = %v, %v, %v, want %v, %v, %v",
					remaining, hasExpiry, ok, tt.wantRemaining, tt.wantHasExpiry, tt.wantOK)
			}
		})
	}
}

func TestDeleteExpired(t *testing.T) {
	s, advance := newStoreWithClock()

	s.Set("alive", "value")
	s.Expire("alive", 30*time.Second)
	s.Set("persistent", "value")
	s.Set("expired", "value")
	s.Expire("expired", 10*time.Second)
	s.Set("expires-now", "value")
	s.Expire("expires-now", 20*time.Second)

	advance(20 * time.Second)

	if got := s.DeleteExpired(); got != 2 {
		t.Errorf("DeleteExpired() = %d, want 2", got)
	}
	assertStoredKeys(t, s, "alive", "persistent")
}

func TestRunExpiration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := NewStore()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			s.RunExpiration(ctx, time.Second)
			close(done)
		}()

		s.Set("first", "value")
		s.Expire("first", 2*time.Second)
		s.Set("second", "value")
		s.Expire("second", 5*time.Second)
		s.Set("persistent", "value")

		time.Sleep(3 * time.Second)
		synctest.Wait()
		assertStoredKeys(t, s, "persistent", "second")

		time.Sleep(3 * time.Second)
		synctest.Wait()
		assertStoredKeys(t, s, "persistent")

		cancel()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Error("RunExpiration did not return after cancel")
		}
	})
}

func assertStoredKeys(t *testing.T, s *Store, want ...string) {
	t.Helper()

	s.mu.RLock()
	got := slices.Sorted(maps.Keys(s.data))
	s.mu.RUnlock()

	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("stored keys = %q, want %q", got, want)
	}
}

type recordingAppender struct {
	records [][]string
}

func (r *recordingAppender) Append(args ...string) {
	r.records = append(r.records, slices.Clone(args))
}

func TestStoreAppendsWrites(t *testing.T) {
	s, advance := newStoreWithClock()
	rec := &recordingAppender{}
	s.aof = rec

	s.Set("a", "1")
	s.Set("b", "2")
	s.Set("c", "3")
	s.Get("a")
	s.TTL("a")
	s.Expire("a", 1500*time.Millisecond)
	s.Expire("missing", time.Second)
	s.ExpireAt("b", time.Unix(1_000_100, 0))
	s.Expire("c", 0)
	s.Delete("a", "missing", "b", "a")
	s.Delete("missing")
	s.Set("d", "4")
	s.Expire("d", time.Second)
	advance(time.Second)
	s.Delete("d")
	s.Expire("d", time.Second)
	s.DeleteExpired()

	want := [][]string{
		{"SET", "a", "1"},
		{"SET", "b", "2"},
		{"SET", "c", "3"},
		{"PEXPIREAT", "a", "1000001500"},
		{"PEXPIREAT", "b", "1000100000"},
		{"PEXPIREAT", "c", "1000000000"},
		{"DEL", "a", "b"},
		{"SET", "d", "4"},
		{"PEXPIREAT", "d", "1000001000"},
	}
	if !slices.EqualFunc(rec.records, want, slices.Equal) {
		t.Errorf("records = %q, want %q", rec.records, want)
	}
}

func TestStoreAppendsInApplyOrder(t *testing.T) {
	const goroutines, iterations = 10, 100

	s := NewStore()
	rec := &recordingAppender{}
	s.aof = rec

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range iterations {
				s.Set("key", strconv.Itoa(g*iterations+i))
			}
		})
	}
	wg.Wait()

	if got := len(rec.records); got != goroutines*iterations {
		t.Fatalf("records = %d, want %d", got, goroutines*iterations)
	}
	last := rec.records[len(rec.records)-1]
	if got, _ := s.Get("key"); last[2] != got {
		t.Errorf("last record value = %q, stored value = %q", last[2], got)
	}
}

func BenchmarkStoreParallel(b *testing.B) {
	const keyCount = 1000

	keys := make([]string, keyCount)
	for i := range keys {
		keys[i] = "key:" + strconv.Itoa(i)
	}

	mixes := []struct {
		name     string
		setEvery int
	}{
		{name: "reads only", setEvery: 0},
		{name: "10% writes", setEvery: 10},
		{name: "writes only", setEvery: 1},
	}

	for _, mix := range mixes {
		b.Run(mix.name, func(b *testing.B) {
			s := NewStore()
			for _, key := range keys {
				s.Set(key, "value")
			}
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := keys[i%keyCount]
					if mix.setEvery > 0 && i%mix.setEvery == 0 {
						s.Set(key, "value")
					} else {
						s.Get(key)
					}
					i++
				}
			})
		})
	}
}
