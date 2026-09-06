//go:build darwin || linux

package historyservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/installer"
)

// ErrUnsafeEndpoint indicates a foreign or insecure filesystem endpoint.
var ErrUnsafeEndpoint = errors.New("history writer endpoint is unsafe")

// ErrOwned indicates another writer still owns the endpoint, even if it stopped listening.
var ErrOwned = errors.New("history writer ownership is held")

type socketIdentity struct {
	Service string
	Device  uint64
	Inode   uint64
}

type endpoint struct {
	path   string
	lock   *os.File
	socket os.FileInfo
}

// SocketPath resolves the user-local endpoint, with an explicit environment override.
func SocketPath(getenv func(string) string) string {
	if path := getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_SOCKET"); path != "" {
		return path
	}
	return filepath.Join(installer.StateRoot(getenv), "history", "writer.sock")
}

func inspectSocket(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSocket == 0 || !ownedByUser(info) {
		return nil, ErrUnsafeEndpoint
	}
	return info, nil
}

func ownedByUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func validatePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(path) > len(unix.RawSockaddrUnix{}.Path)-1 {
		return ErrUnsafeEndpoint
	}
	return nil
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	// Existing shared directories MUST remain untouched, including override parents.
	if !info.IsDir() || !ownedByUser(info) || info.Mode().Perm() != 0700 {
		return ErrUnsafeEndpoint
	}
	return nil
}

func acquireEndpoint(path string) (*endpoint, error) {
	if _, err := inspectSocket(path); err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := privateDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lock, err := openLock(path+".lock", true)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, errors.Join(ErrOwned, lock.Close())
	}
	ep := &endpoint{path: path, lock: lock}
	if err := validateOwnershipRecord(lock); err != nil {
		return nil, errors.Join(err, ep.close())
	}
	if err := ep.removeStale(); err != nil {
		return nil, errors.Join(err, ep.close())
	}
	return ep, nil
}

func openLock(path string, create bool) (*os.File, error) {
	flags := unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if create {
		flags |= unix.O_CREAT
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if !info.Mode().IsRegular() || !ownedByUser(info) || info.Mode().Perm() != 0600 {
		return nil, errors.Join(ErrUnsafeEndpoint, file.Close())
	}
	return file, nil
}

func identityOf(info os.FileInfo) socketIdentity {
	stat := info.Sys().(*syscall.Stat_t)
	return socketIdentity{Service: "agent-fitness-functions-history-writer", Device: uint64(stat.Dev), Inode: stat.Ino}
}

func (e *endpoint) removeStale() error {
	info, err := inspectSocket(e.path)
	if err != nil || info == nil {
		return err
	}
	var recorded socketIdentity
	if err := json.NewDecoder(e.lock).Decode(&recorded); err != nil {
		return ErrUnsafeEndpoint
	}
	if recorded != identityOf(info) {
		return ErrUnsafeEndpoint
	}
	connection, err := net.DialTimeout("unix", e.path, 100*time.Millisecond)
	if err == nil {
		return errors.Join(ErrUnsafeEndpoint, connection.Close())
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		return ErrUnsafeEndpoint
	}
	current, err := os.Lstat(e.path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return ErrUnsafeEndpoint
	}
	return os.Remove(e.path)
}

// A crash between bind and this record leaves an unproven socket. Startup MUST
// reject that endpoint instead of guessing ownership from its filename or UID.
func (e *endpoint) recordSocket() error {
	info, err := os.Lstat(e.path)
	if err != nil {
		return err
	}
	e.socket = info
	if err := e.lock.Truncate(0); err != nil {
		return err
	}
	if _, err := e.lock.Seek(0, 0); err != nil {
		return err
	}
	if err := json.NewEncoder(e.lock).Encode(identityOf(info)); err != nil {
		return err
	}
	return e.lock.Sync()
}

func (e *endpoint) close() error {
	return errors.Join(e.removeSocket(), e.lock.Close())
}

func (e *endpoint) removeSocket() error {
	if e.socket == nil {
		return nil
	}
	current, err := os.Lstat(e.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(e.socket, current) {
		return nil
	}
	return os.Remove(e.path)
}

// Released checks ownership without creating files or removing stale endpoints.
// A missing listener is insufficient: an uncancellable write may still hold the lock.
func Released(path string) (released bool, err error) {
	if err := validatePath(path); err != nil {
		return false, err
	}
	file, err := openLock(path+".lock", false)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return false, nil
		}
		return false, fmt.Errorf("check history ownership: %w", err)
	}
	return true, nil
}

func validateOwnershipRecord(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	} // A creator may crash before binding its first socket.
	if info.Size() > 512 {
		return ErrUnsafeEndpoint
	}
	var record socketIdentity
	if err := json.NewDecoder(io.NewSectionReader(file, 0, 512)).Decode(&record); err != nil {
		return ErrUnsafeEndpoint
	}
	if record.Service != "agent-fitness-functions-history-writer" {
		return ErrUnsafeEndpoint
	}
	return nil
}
