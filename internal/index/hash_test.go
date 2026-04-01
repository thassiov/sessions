package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileHash(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash1, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash() error: %v", err)
	}
	if hash1 == "" {
		t.Error("FileHash() returned empty string")
	}

	// Same file, same hash.
	hash2, err := FileHash(path)
	if err != nil {
		t.Fatalf("FileHash() error: %v", err)
	}
	if hash1 != hash2 {
		t.Errorf("FileHash() not deterministic: %q != %q", hash1, hash2)
	}
}

func TestFileHashChangesOnWrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash1, _ := FileHash(path)

	// Modify the file (changes size and mtime).
	if err := os.WriteFile(path, []byte("hello world, this is longer now"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash2, _ := FileHash(path)

	if hash1 == hash2 {
		t.Error("FileHash() should change when file is modified")
	}
}

func TestFileHashNonexistent(t *testing.T) {
	t.Parallel()

	_, err := FileHash("/nonexistent/file.jsonl")
	if err == nil {
		t.Error("FileHash() should return error for nonexistent file")
	}
}
