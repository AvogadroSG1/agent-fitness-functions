package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

const (
	defaultLimit = 50
	maxLimit     = 1000
)

// ErrNotFound indicates that a history database or event does not exist.
var ErrNotFound = errors.New("history record not found")

// Record adds database ordering and recording time to a preserved event.
type Record struct {
	Event
	Sequence   int64     `json:"sequence"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Query selects a newest-first page of validation history.
type Query struct {
	File      string
	SessionID string
	Worktree  string
	Limit     int
	Before    int64
}

// Page contains one bounded history page and its optional older-page cursor.
type Page struct {
	Records    []Record `json:"records"`
	NextBefore *int64   `json:"next_before"`
}

// List reads a newest-first metadata page without returning the raw payloads.
func List(ctx context.Context, path string, query Query) (Page, error) {
	limit, err := queryLimit(query.Limit)
	if err != nil {
		return Page{}, err
	}
	exists, err := inspectReadTarget(path)
	if err != nil {
		return Page{}, err
	}
	if !exists {
		return Page{Records: []Record{}}, nil
	}
	db, err := openReadDatabase(ctx, path)
	if err != nil {
		return Page{}, err
	}
	page, readErr := listRecords(ctx, db, path, query, limit)
	closeErr := db.Close()
	if readErr != nil {
		return Page{}, fmt.Errorf("list history: %w", errors.Join(readErr, closeErr))
	}
	if closeErr != nil {
		return Page{}, fmt.Errorf("close history database: %w", closeErr)
	}
	return page, nil
}

// Show returns one preserved event including its original request and result.
func Show(ctx context.Context, path, eventID string) (Record, error) {
	exists, err := inspectReadTarget(path)
	if err != nil {
		return Record{}, err
	}
	if !exists {
		return Record{}, ErrNotFound
	}
	db, err := openReadDatabase(ctx, path)
	if err != nil {
		return Record{}, err
	}
	record, readErr := showRecord(ctx, db, path, eventID)
	closeErr := db.Close()
	if readErr != nil {
		return Record{}, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return Record{}, fmt.Errorf("close history database: %w", closeErr)
	}
	return record, nil
}

func queryLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultLimit, nil
	}
	if limit < 0 || limit > maxLimit {
		return 0, fmt.Errorf("history limit must be between 1 and %d", maxLimit)
	}
	return limit, nil
}

func inspectReadTarget(path string) (bool, error) {
	if err := validateFixedPath(path); err != nil {
		return false, err
	}
	directory := filepath.Dir(path)
	if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := requireDirectory(directory); err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("history database is not a regular file")
	}
	return true, nil
}

func openReadDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db := openDatabase(databaseSource(path, "ro"))
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 0"); err != nil {
		return nil, closeSetupError(db, fmt.Errorf("configure history reader: %w", err))
	}
	version, err := databaseVersion(ctx, db)
	if err != nil {
		return nil, closeSetupError(db, fmt.Errorf("read history schema version: %w", err))
	}
	if version != schemaVersion {
		return nil, closeSetupError(db, fmt.Errorf("unsupported history schema version %d", version))
	}
	return db, nil
}

func listRecords(ctx context.Context, db *sql.DB, path string, query Query, limit int) (Page, error) {
	statement, args := listStatement(query, limit+1)
	rows, err := db.QueryContext(ctx, statement, args...)
	if err != nil {
		return Page{}, err
	}
	records := make([]Record, 0, limit+1)
	for rows.Next() {
		record, err := scanRecord(rows, path, false)
		if err != nil {
			return Page{}, errors.Join(err, rows.Close())
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return Page{}, errors.Join(err, rows.Close())
	}
	if err := rows.Close(); err != nil {
		return Page{}, err
	}
	return pageFromRecords(records, limit), nil
}

func listStatement(query Query, limit int) (string, []any) {
	const columns = `sequence, event_id, completed_at, recorded_at, repository, worktree,
branch, head_oid, file, source, tool, action, session_id, status, dry_run`
	statement := "SELECT " + columns + " FROM validation_history"
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 5)
	addFilter := func(column, value string) {
		if value != "" {
			conditions = append(conditions, column+" = ?")
			args = append(args, value)
		}
	}
	addFilter("file", query.File)
	addFilter("session_id", query.SessionID)
	addFilter("worktree", query.Worktree)
	if query.Before > 0 {
		conditions = append(conditions, "sequence < ?")
		args = append(args, query.Before)
	}
	if len(conditions) != 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY sequence DESC LIMIT ?"
	return statement, append(args, limit)
}

func pageFromRecords(records []Record, limit int) Page {
	page := Page{Records: records}
	if len(records) <= limit {
		return page
	}
	page.Records = records[:limit]
	cursor := page.Records[len(page.Records)-1].Sequence
	page.NextBefore = &cursor
	return page
}

func showRecord(ctx context.Context, db *sql.DB, path, eventID string) (Record, error) {
	const statement = `SELECT sequence, event_id, completed_at, recorded_at, repository, worktree,
branch, head_oid, file, source, tool, action, session_id, status, dry_run, request_json, result_json
FROM validation_history WHERE event_id = ?`
	record, err := scanRecord(db.QueryRowContext(ctx, statement, eventID), path, true)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("show history event: %w", err)
	}
	if err := record.Event.Validate(); err != nil {
		return Record{}, fmt.Errorf("validate stored history event: %w", err)
	}
	return record, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(row rowScanner, path string, payloads bool) (Record, error) {
	var record Record
	var completedAt, recordedAt string
	var branch, headOID, tool, action, sessionID sql.NullString
	var dryRun int
	destinations := []any{
		&record.Sequence, &record.EventID, &completedAt, &recordedAt, &record.Repository, &record.Worktree,
		&branch, &headOID, &record.File, &record.Source, &tool, &action, &sessionID, &record.Status, &dryRun,
	}
	if payloads {
		destinations = append(destinations, &record.RequestJSON, &record.ResultJSON)
	}
	if err := row.Scan(destinations...); err != nil {
		return Record{}, err
	}
	if err := hydrateRecord(&record, path, completedAt, recordedAt, dryRun); err != nil {
		return Record{}, err
	}
	record.Branch = optionalString(branch)
	record.HeadOID = optionalString(headOID)
	record.Tool = optionalString(tool)
	record.Action = optionalString(action)
	record.SessionID = optionalString(sessionID)
	return record, nil
}

func hydrateRecord(record *Record, path, completedAt, recordedAt string, dryRun int) error {
	var err error
	record.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAt)
	if err != nil {
		return fmt.Errorf("parse history completion time: %w", err)
	}
	record.RecordedAt, err = time.Parse(time.RFC3339Nano, recordedAt)
	if err != nil {
		return fmt.Errorf("parse history recording time: %w", err)
	}
	record.RecordedAt = record.RecordedAt.UTC()
	record.CommonGitDir = filepath.Dir(filepath.Dir(path))
	if dryRun != 0 && dryRun != 1 {
		return errors.New("stored history dry-run value is invalid")
	}
	record.DryRun = dryRun == 1
	switch record.Status {
	case fitness.StatusPass, fitness.StatusAdvisory, fitness.StatusBlock:
		return nil
	default:
		return errors.New("stored history status is invalid")
	}
}

func optionalString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
