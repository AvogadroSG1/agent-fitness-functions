// Package history preserves completed validation attempts within a Git clone.
package history

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Event contains the client-observed verdict and its original JSON payloads.
// Optional identity fields remain nil when the caller does not supply them.
type Event struct {
	EventID      string          `json:"event_id"`
	CompletedAt  time.Time       `json:"completed_at"`
	CommonGitDir string          `json:"common_git_dir"`
	Repository   string          `json:"repository"`
	Worktree     string          `json:"worktree"`
	Branch       *string         `json:"branch"`
	HeadOID      *string         `json:"head_oid"`
	File         string          `json:"file"`
	Source       string          `json:"source"`
	Tool         *string         `json:"tool"`
	Action       *string         `json:"action"`
	SessionID    *string         `json:"session_id"`
	Status       fitness.Status  `json:"status"`
	DryRun       bool            `json:"dry_run"`
	RequestJSON  json.RawMessage `json:"request_json,omitempty"`
	ResultJSON   json.RawMessage `json:"result_json,omitempty"`
}

// Validate rejects incomplete metadata and attempts without a genuine verdict.
// It MUST NOT reserialize payloads: unknown JSON values and source stay intact.
func (e Event) Validate() error {
	if err := e.validateIdentity(); err != nil {
		return err
	}
	if err := e.validateContext(); err != nil {
		return err
	}
	if err := e.validateRequest(); err != nil {
		return err
	}
	return e.validateResult()
}

func (e Event) validateIdentity() error {
	id, err := hex.DecodeString(e.EventID)
	if err != nil || len(id) != 16 {
		return errors.New("event ID must encode 16 bytes as hex")
	}
	if e.CompletedAt.IsZero() {
		return errors.New("event completion time is missing")
	}
	if _, err := e.CompletedAt.MarshalJSON(); err != nil {
		return fmt.Errorf("invalid event completion time: %w", err)
	}
	return nil
}

func (e Event) validateContext() error {
	if !filepath.IsAbs(e.CommonGitDir) || !filepath.IsAbs(e.Worktree) {
		return errors.New("event Git directories must be absolute")
	}
	if e.Repository == "" || e.Source == "" {
		return errors.New("event repository and source are required")
	}
	if !filepath.IsLocal(e.File) || e.File == "." || filepath.ToSlash(filepath.Clean(e.File)) != e.File {
		return errors.New("event file must be normalized and worktree-relative")
	}
	return nil
}

func (e Event) validateRequest() error {
	// Pointers distinguish absent/null fields from legitimate empty source and
	// false dry-run values. Decoding is only for checks; raw JSON is retained.
	var request *struct {
		Repo            string  `json:"repo"`
		File            string  `json:"file"`
		ProposedContent *string `json:"proposed_content"`
		Language        string  `json:"language"`
		DryRun          *bool   `json:"dry_run"`
	}
	if err := json.Unmarshal(e.RequestJSON, &request); err != nil {
		return fmt.Errorf("invalid history request JSON: %w", err)
	}
	if request == nil || request.ProposedContent == nil || request.DryRun == nil {
		return errors.New("history request is missing required values")
	}
	if request.Repo != e.Repository || request.File == "" {
		return errors.New("history request has inconsistent or missing context")
	}
	if *request.DryRun != e.DryRun {
		return errors.New("history request dry-run does not match event")
	}
	return nil
}

func (e Event) validateResult() error {
	var result *fitness.ValidationResult
	if err := json.Unmarshal(e.ResultJSON, &result); err != nil {
		return fmt.Errorf("invalid history result JSON: %w", err)
	}
	if result == nil || result.Warming {
		return errors.New("history result is not a completed verdict")
	}
	switch result.Status {
	case fitness.StatusPass, fitness.StatusAdvisory, fitness.StatusBlock:
	default:
		return errors.New("history result has no genuine status")
	}
	if result.Status != e.Status {
		return errors.New("history result status does not match event")
	}
	return nil
}
