package history

import (
	"context"
	"database/sql"
	"database/sql/driver"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
)

const (
	schemaVersion = 1
)

//go:embed schema.sql
var schemaSQL string

// Insert preserves one completed validation attempt in its clone's database.
// A repeated event ID is an idempotent success.
func Insert(ctx context.Context, event Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate history event: %w", err)
	}
	path, err := insertPath(ctx, event)
	if err != nil {
		return err
	}
	if err := checkExistingSchema(ctx, path); err != nil {
		return err
	}
	if err := prepareWriteTarget(path); err != nil {
		return fmt.Errorf("prepare history database: %w", err)
	}
	db, err := openWriteDatabase(ctx, path)
	if err != nil {
		return err
	}
	writeErr := insertEvent(ctx, db, event, time.Now().UTC())
	closeErr := db.Close()
	if writeErr != nil {
		return fmt.Errorf("insert history event: %w", errors.Join(writeErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close history database: %w", closeErr)
	}
	return nil
}

func checkExistingSchema(ctx context.Context, path string) error {
	exists, err := inspectReadTarget(path)
	if err != nil {
		return fmt.Errorf("inspect history database: %w", err)
	}
	if !exists {
		return nil
	}
	db := openDatabase(databaseSource(path, "ro"))
	version, versionErr := databaseVersion(ctx, db)
	closeErr := db.Close()
	if versionErr != nil {
		return fmt.Errorf("read existing history schema version: %w", errors.Join(versionErr, closeErr))
	}
	if closeErr != nil {
		return fmt.Errorf("close existing history database: %w", closeErr)
	}
	if version != 0 && version != schemaVersion {
		return fmt.Errorf("unsupported history schema version %d", version)
	}
	return nil
}

func insertPath(ctx context.Context, event Event) (string, error) {
	location, err := Resolve(ctx, event.Worktree, event.File)
	if err != nil {
		return "", fmt.Errorf("resolve history storage: %w", err)
	}
	common, err := matchingRealDirectory(event.CommonGitDir, location.CommonGitDir)
	if err != nil {
		return "", fmt.Errorf("validate history common Git directory: %w", err)
	}
	if _, err := matchingRealDirectory(event.Worktree, location.Worktree); err != nil {
		return "", fmt.Errorf("validate history worktree: %w", err)
	}
	return filepath.Join(common, "agent-fitness-functions", "history.sqlite3"), nil
}

func matchingRealDirectory(got, want string) (string, error) {
	gotReal, err := filepath.EvalSymlinks(got)
	if err != nil {
		return "", err
	}
	wantReal, err := filepath.EvalSymlinks(want)
	if err != nil {
		return "", err
	}
	if gotReal != wantReal {
		return "", errors.New("event location does not match its Git checkout")
	}
	info, err := os.Lstat(gotReal)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("history root is not a real directory")
	}
	return gotReal, nil
}

func prepareWriteTarget(path string) error {
	if err := validateFixedPath(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := requireDirectory(directory); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	return prepareDatabaseFile(path)
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("history storage path is not a real directory")
	}
	return nil
}

func prepareDatabaseFile(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			return createErr
		}
		return file.Close()
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("history database is not a regular file")
	}
	return os.Chmod(path, 0o600)
}

func validateFixedPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("history database path must be absolute and normalized")
	}
	if filepath.Base(path) != "history.sqlite3" || filepath.Base(filepath.Dir(path)) != "agent-fitness-functions" {
		return errors.New("history database path is outside the fixed storage location")
	}
	return nil
}

func openWriteDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db := openDatabase(databaseSource(path, "rw"))
	version, err := databaseVersion(ctx, db)
	if err != nil {
		return nil, closeSetupError(db, fmt.Errorf("read history schema version: %w", err))
	}
	if version != 0 && version != schemaVersion {
		return nil, closeSetupError(db, fmt.Errorf("unsupported history schema version %d", version))
	}
	if err := configureWriter(ctx, db); err != nil {
		return nil, closeSetupError(db, fmt.Errorf("configure history database: %w", err))
	}
	if err := ensureSchema(ctx, db, version); err != nil {
		return nil, closeSetupError(db, fmt.Errorf("initialize history database: %w", err))
	}
	return db, nil
}

func closeSetupError(db *sql.DB, err error) error {
	return errors.Join(err, db.Close())
}

func databaseSource(path, mode string) string {
	return (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=" + mode}).String()
}

func openDatabase(dataSource string) *sql.DB {
	db := sql.OpenDB(driverConnector{driver: &sqlite.Driver{}, dataSource: dataSource})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db
}

type driverConnector struct {
	driver     *sqlite.Driver
	dataSource string
}

func (c driverConnector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dataSource)
}

func (c driverConnector) Driver() driver.Driver {
	return c.driver
}

func configureWriter(ctx context.Context, db *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA busy_timeout = 0",
		"PRAGMA synchronous = FULL",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return err
	}
	if strings.ToLower(journalMode) != "wal" {
		return fmt.Errorf("history database refused WAL mode: %s", journalMode)
	}
	return nil
}

func ensureSchema(ctx context.Context, db *sql.DB, version int) error {
	if version == schemaVersion {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, schemaSQL); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func databaseVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func insertEvent(ctx context.Context, db *sql.DB, event Event, recordedAt time.Time) error {
	const statement = `INSERT INTO validation_history (
event_id, completed_at, recorded_at, repository, worktree, branch, head_oid,
file, source, tool, action, session_id, status, dry_run, request_json, result_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(event_id) DO NOTHING`
	_, err := db.ExecContext(ctx, statement,
		event.EventID, event.CompletedAt.UTC().Format(time.RFC3339Nano), recordedAt.Format(time.RFC3339Nano),
		event.Repository, event.Worktree, event.Branch, event.HeadOID, event.File, event.Source,
		event.Tool, event.Action, event.SessionID, event.Status, event.DryRun, []byte(event.RequestJSON), []byte(event.ResultJSON),
	)
	return err
}
