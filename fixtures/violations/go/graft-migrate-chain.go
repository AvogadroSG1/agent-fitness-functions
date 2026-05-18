//go:build fixture

// Fixture adapted from graft/internal/migrate/migrate.go Chain.
package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type File struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Steps []Step `json:"steps"`
}

type Step struct {
	Type  string `json:"type"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

func Chain(root, name, from, to string) ([]File, error) {
	if err := validateMigrationName(name); err != nil {
		return nil, err
	}
	if from == to {
		return []File{}, nil
	}
	if err := rejectSymlink(filepath.Join(root, "migrations")); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "migrations", name)
	if err := rejectSymlink(dir); err != nil {
		return nil, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	edges := map[string][]File{}
	seenEdges := map[string]bool{}
	for _, entry := range files {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("migration file %q must not be a symlink", entry.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		var file File
		if err := json.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("parse migration %q: %w", entry.Name(), err)
		}
		if err := validateMigrationFilename(entry.Name(), file); err != nil {
			return nil, err
		}
		key := file.From + "\x00" + file.To
		if seenEdges[key] {
			return nil, fmt.Errorf("duplicate migration edge from %s to %s", file.From, file.To)
		}
		seenEdges[key] = true
		edges[file.From] = append(edges[file.From], file)
	}
	for fromVersion := range edges {
		sort.Slice(edges[fromVersion], func(i, j int) bool {
			left := edges[fromVersion][i]
			right := edges[fromVersion][j]
			if left.To == to {
				return true
			}
			if right.To == to {
				return false
			}
			return left.To < right.To
		})
	}
	type path struct {
		current string
		files   []File
		seen    map[string]bool
	}
	queue := []path{{current: from, files: []File{}, seen: map[string]bool{from: true}}}
	cycleVersion := ""
	for len(queue) > 0 {
		nextPath := queue[0]
		queue = queue[1:]
		for _, edge := range edges[nextPath.current] {
			if nextPath.seen[edge.To] {
				if cycleVersion == "" {
					cycleVersion = edge.To
				}
				continue
			}
			files := append(append([]File{}, nextPath.files...), edge)
			if edge.To == to {
				return files, nil
			}
			seen := map[string]bool{}
			for version, ok := range nextPath.seen {
				seen[version] = ok
			}
			seen[edge.To] = true
			queue = append(queue, path{current: edge.To, files: files, seen: seen})
		}
	}
	if cycleVersion != "" {
		return nil, fmt.Errorf("migration cycle detected at %s", cycleVersion)
	}
	return nil, fmt.Errorf("broken migration chain from %s to %s", from, to)
}

func validateMigrationName(string) error {
	return nil
}

func rejectSymlink(string) error {
	return nil
}

func validateMigrationFilename(string, File) error {
	return nil
}
