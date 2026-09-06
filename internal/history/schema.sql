CREATE TABLE validation_history (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    completed_at TEXT NOT NULL,
    recorded_at TEXT NOT NULL,
    repository TEXT NOT NULL,
    worktree TEXT NOT NULL,
    branch TEXT,
    head_oid TEXT,
    file TEXT NOT NULL,
    source TEXT NOT NULL,
    tool TEXT,
    action TEXT,
    session_id TEXT,
    status TEXT NOT NULL CHECK (status IN ('pass', 'advisory', 'block')),
    dry_run INTEGER NOT NULL CHECK (dry_run IN (0, 1)),
    request_json BLOB NOT NULL,
    result_json BLOB NOT NULL
);
CREATE INDEX validation_history_file_sequence
    ON validation_history (file, sequence);
PRAGMA user_version = 1;
