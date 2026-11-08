package main

import (
	"slices"
	"strconv"
	"sync"
	"testing"
)

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
