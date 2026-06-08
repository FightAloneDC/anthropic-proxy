package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"anthropic-proxy/internal/types"
)

func TestFileStorePersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "responses.jsonl")
	first, err := NewFile(path, time.Hour, 10)
	if err != nil {
		t.Fatalf("NewFile first: %v", err)
	}
	first.Store("resp_1", &types.ResponsesResponse{ID: "resp_1", Model: "model", Status: "completed"})
	if err := first.Close(); err != nil {
		t.Fatalf("Close first: %v", err)
	}

	second, err := NewFile(path, time.Hour, 10)
	if err != nil {
		t.Fatalf("NewFile second: %v", err)
	}
	resp, ok := second.Get("resp_1")
	if !ok || resp.ID != "resp_1" || resp.Model != "model" {
		t.Fatalf("stored response = %#v ok=%v", resp, ok)
	}
}

func TestFileStoreSkipsExpiredAndCorruptRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "responses.jsonl")
	old := time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	content := `{"id":"old","created_at":"` + old + `","response":{"id":"old","model":"m","status":"completed"}}
not-json
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fs, err := NewFile(path, time.Hour, 10)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	if fs.Len() != 0 {
		t.Fatalf("len = %d", fs.Len())
	}
}

func TestFileStoreKeepsNewestDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "responses.jsonl")
	old := time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	newer := time.Now().Format(time.RFC3339Nano)
	content := `{"id":"resp_1","created_at":"` + old + `","response":{"id":"resp_1","model":"old","status":"completed"}}
{"id":"resp_1","created_at":"` + newer + `","response":{"id":"resp_1","model":"new","status":"completed"}}
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fs, err := NewFile(path, time.Hour, 10)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	resp, ok := fs.Get("resp_1")
	if !ok || resp.Model != "new" {
		t.Fatalf("response = %#v ok=%v", resp, ok)
	}
}

func TestFileStoreEnforcesMaxEntries(t *testing.T) {
	fs, err := NewFile(filepath.Join(t.TempDir(), "responses.jsonl"), time.Hour, 2)
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	fs.Store("resp_1", &types.ResponsesResponse{ID: "resp_1"})
	time.Sleep(time.Millisecond)
	fs.Store("resp_2", &types.ResponsesResponse{ID: "resp_2"})
	time.Sleep(time.Millisecond)
	fs.Store("resp_3", &types.ResponsesResponse{ID: "resp_3"})

	if fs.Len() != 2 {
		t.Fatalf("len = %d", fs.Len())
	}
	if _, ok := fs.Get("resp_1"); ok {
		t.Fatalf("oldest entry was not evicted")
	}
	if _, ok := fs.Get("resp_2"); !ok {
		t.Fatalf("resp_2 missing")
	}
	if _, ok := fs.Get("resp_3"); !ok {
		t.Fatalf("resp_3 missing")
	}
}

func TestNewStoreRejectsUnsupportedBackend(t *testing.T) {
	if _, err := NewStore("unknown", "", time.Hour, 10); err == nil {
		t.Fatalf("expected unsupported backend error")
	}
}
