package store

import (
	"sync"
	"time"

	"anthropic-proxy/internal/types"
)

// ResponseStore is a TTL-based in-memory store for Responses API responses.
// Used to support previous_response_id lookups.
type ResponseStore struct {
	mu      sync.RWMutex
	entries map[string]*entry
	ttl     time.Duration
	maxSize int
}

type entry struct {
	response  *types.ResponsesResponse
	createdAt time.Time
}

// New creates a new ResponseStore with the given TTL and max size.
// It starts a background cleanup goroutine that runs every ttl/2.
func New(ttl time.Duration, maxSize int) *ResponseStore {
	if maxSize <= 0 {
		maxSize = 1000
	}
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}

	s := &ResponseStore{
		entries: make(map[string]*entry),
		ttl:     ttl,
		maxSize: maxSize,
	}

	// Background cleanup
	go s.cleanup(ttl / 2)

	return s
}

// Store saves a completed response. If the store is at capacity,
// it evicts the oldest entry.
func (s *ResponseStore) Store(id string, resp *types.ResponsesResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict oldest if at capacity
	if len(s.entries) >= s.maxSize {
		s.evictOldest()
	}

	s.entries[id] = &entry{
		response:  resp,
		createdAt: time.Now(),
	}
}

// Get retrieves a stored response by ID. Returns false if not found or expired.
func (s *ResponseStore) Get(id string) (*types.ResponsesResponse, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	e, ok := s.entries[id]
	if !ok {
		return nil, false
	}

	if time.Since(e.createdAt) > s.ttl {
		return nil, false
	}

	return e.response, true
}

// cleanup periodically removes expired entries.
func (s *ResponseStore) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for id, e := range s.entries {
			if now.Sub(e.createdAt) > s.ttl {
				delete(s.entries, id)
			}
		}
		s.mu.Unlock()
	}
}

// evictOldest removes the oldest entry. Must be called with s.mu held.
func (s *ResponseStore) evictOldest() {
	var oldestID string
	var oldestTime time.Time

	for id, e := range s.entries {
		if oldestID == "" || e.createdAt.Before(oldestTime) {
			oldestID = id
			oldestTime = e.createdAt
		}
	}

	if oldestID != "" {
		delete(s.entries, oldestID)
	}
}

// Len returns the current number of stored entries.
func (s *ResponseStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}
