package main

import (
	"slices"
	"strconv"
	"sync"
	"testing"
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
				// Another goroutine may have deleted the key by now.
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

func TestTTL(t *testing.T) {
	tests := []struct {
		name string
		set  bool
		// Zero means Expire is not called.
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
