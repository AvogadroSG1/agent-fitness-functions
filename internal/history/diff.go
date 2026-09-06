package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// ErrDifferentFiles indicates that explicitly selected proposals concern unlike files.
var ErrDifferentFiles = errors.New("history diff requires the same normalized file")

// Comparison preserves both selected contexts and verdicts alongside their source diff.
type Comparison struct {
	From        Record `json:"from"`
	To          Record `json:"to"`
	UnifiedDiff string `json:"unified_diff"`
}

// Diff compares submitted proposals. It MUST NOT imply either proposal was applied.
func Diff(from, to Record) (Comparison, error) {
	if from.File != to.File {
		return Comparison{}, ErrDifferentFiles
	}
	a, err := submittedLines(from)
	if err != nil {
		return Comparison{}, err
	}
	b, err := submittedLines(to)
	if err != nil {
		return Comparison{}, err
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: a, B: b, Context: 3,
		FromFile: strconv.Quote("proposal/" + from.EventID + "/" + from.File),
		ToFile:   strconv.Quote("proposal/" + to.EventID + "/" + to.File),
	})
	if err != nil {
		return Comparison{}, fmt.Errorf("diff history proposals: %w", err)
	}
	return Comparison{From: from, To: to, UnifiedDiff: diff}, nil
}

func submittedLines(record Record) ([]string, error) {
	var request struct {
		Content *string `json:"proposed_content"`
	}
	if err := json.Unmarshal(record.RequestJSON, &request); err != nil {
		return nil, fmt.Errorf("decode proposal %s: %w", record.EventID, err)
	}
	if request.Content == nil {
		return nil, fmt.Errorf("proposal %s has no submitted source", record.EventID)
	}
	if *request.Content == "" {
		return nil, nil
	}
	lines := strings.SplitAfter(*request.Content, "\n")
	last := len(lines) - 1
	if lines[last] == "" {
		return lines[:last], nil
	}
	// The marker belongs to this one source-line token, so difflib's hunk
	// counts and three-line context still count only actual source lines.
	lines[last] += "\n\\ No newline at end of file\n"
	return lines, nil
}
