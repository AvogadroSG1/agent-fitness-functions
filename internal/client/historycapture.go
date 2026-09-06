package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyipc"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/historyservice"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/osevent"
)

type historyOptions struct {
	worktree, source, tool, action, sessionID string
}

func (o *historyOptions) bind(flags *flag.FlagSet) {
	for _, option := range []struct {
		name, env string
		value     *string
	}{
		{"worktree", "WORKTREE", &o.worktree},
		{"source", "SOURCE", &o.source},
		{"tool", "TOOL", &o.tool},
		{"action", "ACTION", &o.action},
		{"session-id", "SESSION_ID", &o.sessionID},
	} {
		flags.StringVar(option.value, "history-"+option.name, os.Getenv("AGENT_FITNESS_FUNCTIONS_HISTORY_"+option.env), "history invocation "+option.name)
	}
}

// historyCapture owns only downstream observability; its dependencies never
// participate in validation output or daemon startup.
type historyCapture struct {
	options historyOptions
	random  io.Reader
	resolve func(context.Context, string, string) (history.Location, error)
	publish func(string, history.Event) error
	log     func(osevent.Diagnostic) error
}

func newHistoryCapture(options historyOptions) historyCapture {
	return historyCapture{options: options, random: rand.Reader, resolve: history.Resolve, publish: historyipc.Publish, log: osevent.LogValidation}
}

func (c historyCapture) complete(ctx context.Context, opts validateOptions, request, result []byte, completed time.Time) {
	status, kind := historyVerdict(result)
	if kind != "" {
		c.diagnostic(opts.repo, "", "response", kind)
		return
	}
	event, err := c.event(ctx, opts, completed)
	if err != nil {
		c.diagnostic(opts.repo, event.EventID, "metadata", "metadata")
		return
	}
	c.options.worktree = event.Worktree
	event.Status, event.RequestJSON, event.ResultJSON = status, request, result
	if err := event.Validate(); err != nil {
		c.diagnostic(opts.repo, event.EventID, "metadata", "invalid_event")
		return
	}
	if err := c.publish(historyservice.SocketPath(os.Getenv), event); err != nil {
		c.diagnostic(opts.repo, event.EventID, "publish", "delivery")
	}
}

func historyVerdict(body []byte) (fitness.Status, string) {
	var result *fitness.ValidationResult
	if err := json.Unmarshal(body, &result); err != nil || result == nil {
		return "", "invalid_response"
	}
	if result.Warming {
		return "", "warming"
	}
	switch result.Status {
	case fitness.StatusPass, fitness.StatusAdvisory, fitness.StatusBlock:
		return result.Status, ""
	default:
		return "", "invalid_response"
	}
}

func (c historyCapture) event(ctx context.Context, opts validateOptions, completed time.Time) (history.Event, error) {
	var id [16]byte
	if _, err := io.ReadFull(c.random, id[:]); err != nil {
		return history.Event{}, err
	}
	event := history.Event{EventID: hex.EncodeToString(id[:]), CompletedAt: completed.UTC()}
	checkout := c.options.worktree
	if checkout == "" {
		checkout = resolveRepoRoot(opts.repo, opts.file)
	}
	location, err := c.resolve(ctx, checkout, opts.file)
	if err != nil {
		return event, err
	}
	event.CommonGitDir, event.Worktree, event.File = location.CommonGitDir, location.Worktree, location.File
	event.Branch, event.HeadOID = location.Branch, location.HeadOID
	event.Repository, event.DryRun = opts.repo, opts.dryRun
	event.Source = c.options.source
	if event.Source == "" {
		event.Source = "manual"
	}
	event.Tool, event.Action, event.SessionID = historyOptional(c.options.tool), historyOptional(c.options.action), historyOptional(c.options.sessionID)
	return event, nil
}

func historyOptional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func (c historyCapture) diagnostic(repo, id, phase, kind string) {
	// Logging failure MUST itself be dropped; it MUST NOT recurse or affect validation.
	_ = c.log(osevent.Diagnostic{Name: "validation_history_dropped", Phase: phase, ErrorKind: kind, EventID: id,
		Repository: repo, Worktree: c.options.worktree, Tool: c.options.tool, SessionID: c.options.sessionID})
}

func (o historyOptions) diagnostic(repo, id, phase, kind string) {
	newHistoryCapture(o).diagnostic(repo, id, phase, kind)
}

func historyFailureKind(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var status httpStatusError
	if errors.As(err, &status) {
		return "http_status"
	}
	return "transport"
}
