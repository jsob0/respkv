package main

import "sync"

type Store struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewStore() *Store {
	return &Store{data: make(map[string]string)}
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	value, ok := s.data[key]
	return value, ok
}

func (s *Store) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value
}

func (s *Store) Delete(keys ...string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	deleted := 0
	for _, key := range keys {
		if _, ok := s.data[key]; ok {
			delete(s.data, key)
			deleted++
		}
	}
	return deleted
}
