package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindNearestCsproj walks from the directory of csFilePath up to root looking for a .csproj file.
// Returns the absolute path of the first .csproj found, or "" if none exists within root.
func FindNearestCsproj(csFilePath, root string) (string, error) {
	root = filepath.Clean(root)
	dir := filepath.Clean(filepath.Dir(csFilePath))
	for {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return "", fmt.Errorf("reading dir %s: %w", dir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".csproj") {
				return filepath.Join(dir, entry.Name()), nil
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", nil
}
