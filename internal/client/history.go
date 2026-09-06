package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/history"
)

const historyUsage = `usage: agent-fitness-functions client history <list|show|diff>
  list [--repo PATH] [--file PATH] [--session ID] [--worktree PATH]
       [--limit 50] [--before SEQUENCE] [--format json]
  show EVENT_ID [--repo PATH] [--format json]
  diff FROM_ID TO_ID [--repo PATH] [--format json]
--repo defaults to the current checkout; output defaults to readable text.
diff compares submitted proposals, including their recorded verdicts.
`

type historyReadOptions struct {
	command string
	repo    string
	format  string
	ids     []string
	query   history.Query
}

// RunHistory inspects clone-local history directly, independently of any writer
// or governance daemon. Reads MUST NOT initialize or repair history storage.
func RunHistory(args []string, stdout io.Writer) error {
	opts, err := parseHistoryOptions(args)
	if errors.Is(err, flag.ErrHelp) {
		_, err = io.WriteString(stdout, historyUsage)
		return err
	}
	if err != nil {
		return usageError{err: err}
	}
	ctx := context.Background()
	location, err := history.ResolveCheckout(ctx, opts.repo)
	if err != nil {
		return err
	}
	return inspectHistory(ctx, location, opts, stdout)
}

func parseHistoryOptions(args []string) (historyReadOptions, error) {
	if len(args) == 0 {
		return historyReadOptions{}, errors.New(historyUsage)
	}
	if args[0] == "--help" || args[0] == "-h" {
		return historyReadOptions{}, flag.ErrHelp
	}
	counts := map[string]int{"list": 0, "show": 1, "diff": 2}
	count, known := counts[args[0]]
	if !known {
		return historyReadOptions{}, fmt.Errorf("unknown history command %q", args[0])
	}
	opts := historyReadOptions{command: args[0]}
	flags := historyFlags(&opts)
	ids, err := parseHistoryFlags(flags, args[1:])
	if err != nil {
		return historyReadOptions{}, err
	}
	if len(ids) != count {
		return historyReadOptions{}, fmt.Errorf("history %s requires %d event IDs", opts.command, count)
	}
	opts.ids = ids
	return opts, validateHistoryOptions(opts)
}

func historyFlags(opts *historyReadOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("client history "+opts.command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.repo, "repo", ".", "Git checkout path")
	flags.StringVar(&opts.format, "format", "text", "output format: text or json")
	if opts.command == "list" {
		flags.StringVar(&opts.query.File, "file", "", "worktree-relative or absolute file")
		flags.StringVar(&opts.query.SessionID, "session", "", "recorded session ID")
		flags.StringVar(&opts.query.Worktree, "worktree", "", "recorded worktree path")
		flags.IntVar(&opts.query.Limit, "limit", 50, "page size (1-1000)")
		flags.Int64Var(&opts.query.Before, "before", 0, "exclusive sequence cursor")
	}
	return flags
}

// parseHistoryFlags allows flags before, between, or after the selected IDs.
// An explicit -- terminator retains the standard flag package's meaning.
func parseHistoryFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var ids []string
	for len(args) != 0 {
		if args[0] == "--" {
			return append(ids, args[1:]...), nil
		}
		if !strings.HasPrefix(args[0], "-") || args[0] == "-" {
			ids = append(ids, args[0])
			args = args[1:]
			continue
		}
		name, _, assigned := strings.Cut(strings.TrimLeft(args[0], "-"), "=")
		count := 1
		// Every registered history flag takes a value. Parse it together with
		// the flag, even when that value is itself -- or starts with a dash.
		if !assigned && flags.Lookup(name) != nil {
			count = min(2, len(args))
		}
		if err := flags.Parse(args[:count]); err != nil {
			return nil, err
		}
		args = args[count:]
	}
	return ids, nil
}

func validateHistoryOptions(opts historyReadOptions) error {
	if opts.format != "text" && opts.format != "json" {
		return fmt.Errorf("unsupported history format %q: use text or json", opts.format)
	}
	if opts.command == "list" && (opts.query.Limit < 1 || opts.query.Limit > 1000) {
		return errors.New("history limit must be between 1 and 1000")
	}
	if opts.query.Before < 0 {
		return errors.New("history before sequence must not be negative")
	}
	return nil
}

func inspectHistory(ctx context.Context, location history.Location, opts historyReadOptions, stdout io.Writer) error {
	switch opts.command {
	case "list":
		return listHistory(ctx, location, opts, stdout)
	case "show":
		record, err := history.Show(ctx, location.DatabasePath, opts.ids[0])
		if err != nil {
			return err
		}
		return outputHistory(stdout, opts.format, record, func() string { return historyRecordText(record, true) })
	default:
		return diffHistory(ctx, location.DatabasePath, opts, stdout)
	}
}

func listHistory(ctx context.Context, location history.Location, opts historyReadOptions, stdout io.Writer) error {
	query, err := normalizeHistoryQuery(location.Worktree, opts.query)
	if err != nil {
		return usageError{err: err}
	}
	page, err := history.List(ctx, location.DatabasePath, query)
	if err != nil {
		return err
	}
	return outputHistory(stdout, opts.format, page, func() string { return historyPageText(page) })
}

func normalizeHistoryQuery(worktree string, query history.Query) (history.Query, error) {
	var err error
	if query.File != "" {
		query.File, err = history.FileKey(worktree, query.File)
		if err != nil {
			return history.Query{}, err
		}
	}
	if query.Worktree != "" {
		query.Worktree, err = history.WorktreeKey(query.Worktree)
	}
	return query, err
}

func diffHistory(ctx context.Context, path string, opts historyReadOptions, stdout io.Writer) error {
	from, err := history.Show(ctx, path, opts.ids[0])
	if err != nil {
		return err
	}
	to, err := history.Show(ctx, path, opts.ids[1])
	if err != nil {
		return err
	}
	comparison, err := history.Diff(from, to)
	if errors.Is(err, history.ErrDifferentFiles) {
		return usageError{err: err}
	}
	if err != nil {
		return err
	}
	return outputHistory(stdout, opts.format, comparison, func() string {
		return "From proposal:\n" + historyRecordText(from, false) + "To proposal:\n" + historyRecordText(to, false) + comparison.UnifiedDiff
	})
}

func outputHistory(stdout io.Writer, format string, value any, readable func() string) error {
	if format == "json" {
		return json.NewEncoder(stdout).Encode(value)
	}
	_, err := io.WriteString(stdout, readable())
	return err
}

func historyPageText(page history.Page) string {
	if len(page.Records) == 0 {
		return "No validation history.\n"
	}
	var text strings.Builder
	for _, record := range page.Records {
		fmt.Fprintf(&text, "%d  %s  %s  %s  %q  worktree=%q  session=%s  dry_run=%t\n",
			record.Sequence, record.EventID, record.CompletedAt.Format(time.RFC3339Nano), record.Status,
			record.File, record.Worktree, historyIdentity(record.SessionID), record.DryRun)
	}
	if page.NextBefore != nil {
		fmt.Fprintf(&text, "Next page: --before %d\n", *page.NextBefore)
	}
	return text.String()
}

func historyRecordText(record history.Record, request bool) string {
	var text strings.Builder
	fmt.Fprintf(&text, "Event: %s\nSequence: %d\nCompleted: %s\nRecorded: %s\nRepository: %q\nFile: %q\nWorktree: %q\nBranch: %s\nHEAD: %s\nSource: %q\nTool: %s\nAction: %s\nSession: %s\nStatus: %s\nDry run: %t\n",
		record.EventID, record.Sequence, record.CompletedAt.Format(time.RFC3339Nano), record.RecordedAt.Format(time.RFC3339Nano),
		record.Repository, record.File, record.Worktree, historyIdentity(record.Branch), historyIdentity(record.HeadOID),
		record.Source, historyIdentity(record.Tool), historyIdentity(record.Action), historyIdentity(record.SessionID), record.Status, record.DryRun)
	if request {
		fmt.Fprintf(&text, "Request:\n%s\n", readableHistoryJSON(record.RequestJSON))
	}
	fmt.Fprintf(&text, "Result:\n%s\n", readableHistoryJSON(record.ResultJSON))
	return text.String()
}

func historyIdentity(value *string) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%q", *value)
}

func readableHistoryJSON(raw json.RawMessage) string {
	var text bytes.Buffer
	if err := json.Indent(&text, raw, "", "  "); err != nil {
		return string(raw)
	}
	return text.String()
}
