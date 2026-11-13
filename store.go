package main

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	value     string
	expiresAt time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]entry
	now  func() time.Time
}

func (e entry) expired(now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]entry),
		now:  time.Now,
	}
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.getLocked(key, s.now())
	return e.value, ok
}

func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = entry{value: value}
}

func (s *Store) Delete(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	deleted := 0
	for _, key := range keys {
		if _, ok := s.getLocked(key, now); ok {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}

func (s *Store) Expire(key string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	e, ok := s.getLocked(key, now)
	if !ok {
		return false
	}
	if ttl <= 0 {
		delete(s.data, key)
		return true
	}
	e.expiresAt = now.Add(ttl)
	s.data[key] = e
	return true
}

func (s *Store) TTL(key string) (remaining time.Duration, hasExpiry, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	e, ok := s.getLocked(key, now)
	if !ok {
		return 0, false, false
	}
	if e.expiresAt.IsZero() {
		return 0, false, true
	}
	return e.expiresAt.Sub(now), true, true
}

func (s *Store) DeleteExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	deleted := 0
	for key, e := range s.data {
		if e.expired(now) {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}

func (s *Store) RunExpiration(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.DeleteExpired()
		}
	}
}

func (s *Store) getLocked(key string, now time.Time) (entry, bool) {
	if e, ok := s.data[key]; ok && !e.expired(now) {
		return e, true
	}
	return entry{}, false
}
