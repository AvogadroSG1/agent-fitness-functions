//go:build darwin || linux

package historyservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestForeignEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s")
	const content = "another application's file"
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Run(ctx, Config{SocketPath: path}); !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("writer startup = %v, want unsafe endpoint rejection", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("foreign endpoint changed: %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("foreign endpoint permissions changed: %v, %v", info, err)
	}
}
