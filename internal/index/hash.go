// Package index handles JSONL parsing and session indexing.
package index

import (
	"crypto/md5" //nolint:gosec // not for security, just change detection
	"fmt"
	"os"
)

// FileHash computes a quick hash based on file size and mtime.
// Not a content hash — used only to detect if a file has changed since last index.
func FileHash(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	key := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
	sum := md5.Sum([]byte(key)) //nolint:gosec
	return fmt.Sprintf("%x", sum), nil
}
