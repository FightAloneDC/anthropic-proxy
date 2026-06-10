package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"anthropic-proxy/internal/types"
)

type fileEntry struct {
	ID        string                   `json:"id"`
	CreatedAt time.Time                `json:"created_at"`
	Response  *types.ResponsesResponse `json:"response"`
}

type FileStore struct {
	mu       sync.RWMutex
	entries  map[string]*entry
	path     string
	ttl      time.Duration
	maxSize  int
	compactN int
	stop     chan struct{}
}

func NewFile(path string, ttl time.Duration, maxSize int) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("file store path is required")
	}
	if maxSize <= 0 {
		maxSize = 1000
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	fs := &FileStore{
		entries: make(map[string]*entry),
		path:    path,
		ttl:     ttl,
		maxSize: maxSize,
		stop:    make(chan struct{}),
	}
	if err := fs.load(); err != nil {
		return nil, err
	}
	go fs.cleanup(ttl / 2)
	return fs, nil
}

func NewStore(backend, path string, ttl time.Duration, maxSize int) (Store, error) {
	switch backend {
	case "", "memory":
		return NewMemory(ttl, maxSize), nil
	case "file":
		return NewFile(path, ttl, maxSize)
	case "redis", "redis-placeholder":
		return nil, errors.New("redis store backend is not implemented")
	default:
		return nil, errors.New("unsupported store backend: " + backend)
	}
}

func (fs *FileStore) Store(id string, resp *types.ResponsesResponse) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.storeLocked(id, resp, time.Now())
	_ = fs.appendLocked(fileEntry{ID: id, CreatedAt: fs.entries[id].createdAt, Response: resp})
}

func (fs *FileStore) Get(id string) (*types.ResponsesResponse, bool) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	e, ok := fs.entries[id]
	if !ok || time.Since(e.createdAt) > fs.ttl {
		return nil, false
	}
	return e.response, true
}

func (fs *FileStore) Len() int {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return len(fs.entries)
}

func (fs *FileStore) Stats() Stats {
	fs.mu.RLock()
	defer fs.mu.RUnlock()
	return Stats{
		Backend:    "file",
		Path:       fs.path,
		Entries:    len(fs.entries),
		MaxEntries: fs.maxSize,
		TTLSeconds: int(fs.ttl.Seconds()),
	}
}

func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	select {
	case <-fs.stop:
	default:
		close(fs.stop)
	}
	return fs.compactLocked()
}

func (fs *FileStore) load() error {
	// Clean up orphaned temp files from previous crashes
	tmpPath := fs.path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		_ = os.Remove(tmpPath)
	}

	file, err := os.Open(fs.path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(filepath.Dir(fs.path), 0755)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	now := time.Now()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record fileEntry
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil || record.ID == "" || record.Response == nil {
			continue
		}
		if now.Sub(record.CreatedAt) > fs.ttl {
			continue
		}
		if existing, ok := fs.entries[record.ID]; ok && !record.CreatedAt.After(existing.createdAt) {
			continue
		}
		fs.storeLoaded(record.ID, record.Response, record.CreatedAt)
	}
	return scanner.Err()
}

func (fs *FileStore) cleanup(interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fs.mu.Lock()
			now := time.Now()
			changed := false
			for id, e := range fs.entries {
				if now.Sub(e.createdAt) > fs.ttl {
					delete(fs.entries, id)
					changed = true
				}
			}
			if changed {
				_ = fs.compactLocked()
			}
			fs.mu.Unlock()
		case <-fs.stop:
			return
		}
	}
}

func (fs *FileStore) storeLocked(id string, resp *types.ResponsesResponse, createdAt time.Time) {
	if len(fs.entries) >= fs.maxSize {
		fs.evictOldestLocked()
		fs.compactN++
	}
	fs.entries[id] = &entry{response: resp, createdAt: createdAt}
	if fs.compactN >= fs.maxSize {
		_ = fs.compactLocked()
		fs.compactN = 0
	}
}

func (fs *FileStore) storeLoaded(id string, resp *types.ResponsesResponse, createdAt time.Time) {
	if len(fs.entries) >= fs.maxSize {
		fs.evictOldestLocked()
	}
	fs.entries[id] = &entry{response: resp, createdAt: createdAt}
}

func (fs *FileStore) evictOldestLocked() {
	var oldestID string
	var oldestTime time.Time
	for id, e := range fs.entries {
		if oldestID == "" || e.createdAt.Before(oldestTime) {
			oldestID = id
			oldestTime = e.createdAt
		}
	}
	if oldestID != "" {
		delete(fs.entries, oldestID)
	}
}

func (fs *FileStore) appendLocked(record fileEntry) error {
	if err := os.MkdirAll(filepath.Dir(fs.path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(fs.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(record); err != nil {
		return err
	}
	return file.Sync()
}

func (fs *FileStore) compactLocked() error {
	if err := os.MkdirAll(filepath.Dir(fs.path), 0755); err != nil {
		return err
	}
	tmp := fs.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(fs.entries))
	for id := range fs.entries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return fs.entries[ids[i]].createdAt.Before(fs.entries[ids[j]].createdAt)
	})
	encoder := json.NewEncoder(file)
	for _, id := range ids {
		e := fs.entries[id]
		if err := encoder.Encode(fileEntry{ID: id, CreatedAt: e.createdAt, Response: e.response}); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, fs.path); err != nil {
		// Fallback: copy + remove for cross-filesystem renames
		if copyErr := copyFile(tmp, fs.path); copyErr != nil {
			return copyErr
		}
		_ = os.Remove(tmp)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
