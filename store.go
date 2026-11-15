package main

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type appender interface {
	Append(args ...string)
}

type entry struct {
	value     string
	expiresAt time.Time
}

type Store struct {
	mu   sync.RWMutex
	data map[string]entry
	now  func() time.Time
	aof  appender
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
	s.appendLocked("SET", key, value)
}

func (s *Store) Delete(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	var deleted []string
	for _, key := range keys {
		if _, ok := s.getLocked(key, now); ok {
			delete(s.data, key)
			deleted = append(deleted, key)
		}
	}
	if len(deleted) > 0 {
		s.appendLocked(append([]string{"DEL"}, deleted...)...)
	}
	return len(deleted)
}

func (s *Store) Expire(key string, ttl time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	return s.expireAtLocked(key, now.Add(ttl), now)
}

func (s *Store) ExpireAt(key string, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.expireAtLocked(key, at, s.now())
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

func (s *Store) expireAtLocked(key string, at, now time.Time) bool {
	e, ok := s.getLocked(key, now)
	if !ok {
		return false
	}
	s.appendLocked("PEXPIREAT", key, strconv.FormatInt(at.UnixMilli(), 10))
	if !now.Before(at) {
		delete(s.data, key)
		return true
	}
	e.expiresAt = at
	s.data[key] = e
	return true
}

func (s *Store) appendLocked(args ...string) {
	if s.aof != nil {
		s.aof.Append(args...)
	}
}
