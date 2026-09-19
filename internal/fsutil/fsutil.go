// Package fsutil holds small filesystem helpers used by the entrypoint.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureWritable checks that a directory exists and is writable, creating it
// first if needed.
func EnsureWritable(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", path, err)
	}
	probe := filepath.Join(path, ".write_test")
	f, err := os.Create(probe)
	if err != nil {
		return fmt.Errorf("directory %s is not writable: %w", path, err)
	}
	f.Close()
	os.Remove(probe)
	return nil
}
