package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const runtimeNameAttempts = 16

var runtimeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type managedRuntime struct {
	version string
	ca      []byte
}

type runtimeOperations struct {
	lstat    func(string) (os.FileInfo, error)
	open     func(string) (*os.File, error)
	openFile func(string, int, os.FileMode) (*os.File, error)
	rename   func(string, string) error
	remove   func(string) error
	sync     func(*os.File) error
	nextID   func() (string, error)
}

var defaultRuntimeOperations = runtimeOperations{
	lstat:    os.Lstat,
	open:     os.Open,
	openFile: os.OpenFile,
	rename:   os.Rename,
	remove:   os.Remove,
	sync:     func(file *os.File) error { return file.Sync() },
	nextID:   runtimeRandomID,
}

func publishManagedRuntime(dir string, material managedRuntime, operations runtimeOperations) (resultErr error) {
	directory, observed, err := openRuntimeDirectory(dir, operations)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	for _, artifact := range []struct {
		name string
		data []byte
	}{
		{name: "pinned-dev-cert-version", data: []byte(material.version + "\n")},
		{name: "health-ca.crt", data: material.ca},
	} {
		if err := replaceRuntimeFile(dir, artifact.name, artifact.data, directory, observed, operations); err != nil {
			return fmt.Errorf("publish managed runtime %s: %w", artifact.name, err)
		}
	}
	return nil
}

func openRuntimeDirectory(path string, operations runtimeOperations) (*os.File, os.FileInfo, error) {
	observed, err := operations.lstat(path)
	if err != nil || !observed.IsDir() || observed.Mode().Perm() != 0o755 || observed.Mode()&os.ModeSymlink != 0 {
		return nil, nil, errors.New("runtime directory must be a non-symlink directory with mode 0755")
	}
	directory, err := operations.open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open runtime directory: %w", err)
	}
	pinned, err := directory.Stat()
	if err != nil || !os.SameFile(observed, pinned) {
		_ = directory.Close()
		return nil, nil, errors.New("runtime directory changed during open")
	}
	return directory, observed, nil
}

func replaceRuntimeFile(dir, name string, data []byte, directory *os.File, directoryInfo os.FileInfo, operations runtimeOperations) error {
	destination := filepath.Join(dir, name)
	if info, err := operations.lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return errors.New("runtime destination has unsafe type or mode")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runtime destination: %w", err)
	}

	for range runtimeNameAttempts {
		id, err := operations.nextID()
		if err != nil {
			return err
		}
		if !runtimeIDPattern.MatchString(id) {
			return errors.New("invalid runtime temporary name")
		}
		temp := filepath.Join(dir, "."+name+"."+id+".tmp")
		file, err := operations.openFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("create exclusive runtime temp: %w", err)
		}
		if err := finishRuntimeFile(file, temp, destination, data, directory, directoryInfo, operations); err != nil {
			return err
		}
		return nil
	}
	return errors.New("runtime temporary name collisions exhausted")
}

func finishRuntimeFile(file *os.File, temp, destination string, data []byte, directory *os.File, directoryInfo os.FileInfo, operations runtimeOperations) (resultErr error) {
	removeTemp := true
	var tempInfo os.FileInfo
	defer func() {
		if removeTemp {
			resultErr = errors.Join(resultErr, removeRuntimeTemp(file, temp, tempInfo, operations))
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod exclusive runtime temp: %w", err)
	}
	observed, err := operations.lstat(temp)
	if err != nil || !observed.Mode().IsRegular() || observed.Mode().Perm() != 0o600 {
		return errors.New("runtime temp has unsafe type or mode")
	}
	tempInfo = observed
	pinned, err := file.Stat()
	if err != nil || !os.SameFile(observed, pinned) {
		return errors.New("runtime temp changed during open")
	}
	if err := writeRuntimeBytes(file, data); err != nil {
		return fmt.Errorf("write runtime temp: %w", err)
	}
	if err := operations.sync(file); err != nil {
		return fmt.Errorf("sync runtime temp: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		return fmt.Errorf("chmod runtime temp: %w", err)
	}
	if err := operations.sync(file); err != nil {
		return fmt.Errorf("sync chmodded runtime temp: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close runtime temp: %w", err)
	}
	if info, err := operations.lstat(temp); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 || !os.SameFile(observed, info) {
		return errors.New("runtime temp changed before rename")
	}
	if info, err := operations.lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return errors.New("runtime destination changed before rename")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("reinspect runtime destination: %w", err)
	}
	if currentDirectory, err := directory.Stat(); err != nil || !os.SameFile(directoryInfo, currentDirectory) {
		return errors.New("runtime directory changed before rename")
	}
	if err := operations.rename(temp, destination); err != nil {
		return fmt.Errorf("rename runtime temp: %w", err)
	}
	removeTemp = false
	if err := operations.sync(directory); err != nil {
		return fmt.Errorf("sync runtime directory: %w", err)
	}
	return nil
}

func writeRuntimeBytes(file *os.File, data []byte) error {
	for len(data) > 0 {
		written, err := file.Write(data)
		if err != nil {
			return err
		}
		if written == 0 {
			return errors.New("short runtime write")
		}
		data = data[written:]
	}
	return nil
}

func removeRuntimeTemp(file *os.File, path string, want os.FileInfo, operations runtimeOperations) error {
	_ = file.Close()
	info, err := operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || want == nil || !info.Mode().IsRegular() || !os.SameFile(want, info) {
		return err
	}
	return operations.remove(path)
}

func runtimeRandomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", errors.New("generate runtime temporary name")
	}
	return hex.EncodeToString(value), nil
}
