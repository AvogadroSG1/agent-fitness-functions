package sarif

import (
	"fmt"
	"path/filepath"

	"github.com/poconnor/calm-poc/internal/bridge"
)

const schema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

type log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []run  `json:"runs"`
}

type run struct {
	Tool    tool     `json:"tool"`
	Results []result `json:"results"`
}

type tool struct {
	Driver driver `json:"driver"`
}

type driver struct {
	Name           string `json:"name"`
	InformationURI string `json:"informationUri"`
	Rules          []rule `json:"rules,omitempty"`
}

type rule struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type result struct {
	RuleID    string     `json:"ruleId"`
	Level     string     `json:"level"`
	Message   message    `json:"message"`
	Locations []location `json:"locations,omitempty"`
}

type message struct {
	Text string `json:"text"`
}

type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
}

type artifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

// Convert transforms a bridge.CheckResponse into a SARIF 2.1.0 document.
// repoRoot is used to produce repo-relative URIs for file locations.
func Convert(resp bridge.CheckResponse, repoRoot string) any {
	rules := uniqueRules(resp.Violations)
	results := make([]result, 0, len(resp.Violations))
	for _, v := range resp.Violations {
		results = append(results, toResult(v, resp.Status, repoRoot))
	}
	return log{
		Schema:  schema,
		Version: "2.1.0",
		Runs: []run{
			{
				Tool: tool{
					Driver: driver{
						Name:           "calm-bridge",
						InformationURI: "https://calm.finos.org",
						Rules:          rules,
					},
				},
				Results: results,
			},
		},
	}
}

func toResult(v bridge.Violation, status bridge.CheckStatus, repoRoot string) result {
	text := v.Message
	if v.Function != "" {
		text = fmt.Sprintf("%s (function: %s)", text, v.Function)
	}
	if v.Value != 0 || v.Limit != 0 {
		text = fmt.Sprintf("%s [value: %.2f, limit: %.2f]", text, v.Value, v.Limit)
	}
	r := result{
		RuleID:  v.FitnessFunction,
		Level:   levelFor(status),
		Message: message{Text: text},
	}
	if v.File != "" {
		r.Locations = []location{
			{PhysicalLocation: physicalLocation{
				ArtifactLocation: artifactLocation{
					URI:       relURI(v.File, repoRoot),
					URIBaseID: "%SRCROOT%",
				},
			}},
		}
	}
	return r
}

func levelFor(status bridge.CheckStatus) string {
	if status == bridge.StatusBlock {
		return "error"
	}
	return "warning"
}

// relURI returns a forward-slash path relative to repoRoot, or the original
// path when relativisation fails.
func relURI(file, repoRoot string) string {
	if repoRoot == "" {
		return filepath.ToSlash(file)
	}
	rel, err := filepath.Rel(repoRoot, file)
	if err != nil {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}

func uniqueRules(violations []bridge.Violation) []rule {
	seen := make(map[string]struct{}, len(violations))
	var rules []rule
	for _, v := range violations {
		if _, ok := seen[v.FitnessFunction]; ok {
			continue
		}
		seen[v.FitnessFunction] = struct{}{}
		rules = append(rules, rule{ID: v.FitnessFunction, Name: v.FitnessFunction})
	}
	return rules
}
