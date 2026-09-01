package server

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/analyzer"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/calm"
	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// contentScoringInput carries everything a content scorer needs: the proposed
// file, the repository's governance configuration, the pattern rule being
// enforced, and the CALM node the analyzed file belongs to.
type contentScoringInput struct {
	request  fitness.ValidationRequest
	config   Config
	rule     calm.FitnessRule
	calmNode string
}

// contentScorer scores one generalized fitness function directly against the
// proposed file content rather than against analyzer metrics. score returns the
// per-file violation count (recorded on AnalysisResult.RuleCounts so the
// generated CALM document carries it) and the violations to report.
type contentScorer struct {
	name     string
	operator string
	score    func(contentScoringInput) (int, []fitness.Violation)
}

// contentScorers is the registry of content-scored fitness functions. Each
// entry is skipped unless the governance pattern declares its rule with the
// expected operator and the repository has the function enabled.
var contentScorers = []contentScorer{
	{name: "deterministic-ordering", operator: "lte", score: deterministicOrderingViolations},
}

// scoreContentFunctions runs every applicable content scorer, records each
// scored count on result.RuleCounts, and returns the violations found. It must
// run before report.BuildArchitecture so the counts reach the CALM document.
func scoreContentFunctions(
	result *analyzer.AnalysisResult,
	request fitness.ValidationRequest,
	config Config,
	pattern calm.Pattern,
) []fitness.Violation {
	violations := make([]fitness.Violation, 0)
	counts := make(map[string]int, len(result.RuleCounts)+len(contentScorers))
	for name, count := range result.RuleCounts {
		counts[name] = count
	}
	for _, scorer := range contentScorers {
		rule, ok := pattern.FitnessFunctions[scorer.name]
		if !ok || rule.Operator != scorer.operator || !config.enabled(scorer.name) {
			continue
		}
		count, scored := scorer.score(contentScoringInput{
			request:  request,
			config:   config,
			rule:     rule,
			calmNode: result.CALMNode,
		})
		counts[scorer.name] = count
		violations = append(violations, scored...)
	}
	result.RuleCounts = counts
	return violations
}

// windowFunctionOrderBy matches the ORDER BY clause of a SQL window function,
// with or without a PARTITION BY, capturing the ordering expression.
var windowFunctionOrderBy = regexp.MustCompile(
	`(?is)OVER\s*\(\s*(?:PARTITION\s+BY\s+[^)]+?)?\s*ORDER\s+BY\s+([^)]+)\)`,
)

// maxReportedOrderByClause bounds how much of an offending ORDER BY clause is
// echoed back in a violation message.
const maxReportedOrderByClause = 80

// deterministicOrderingViolations counts window-function ORDER BY clauses in
// the proposed content that carry no configured tie-breaker token, and reports
// them as a single per-file violation.
func deterministicOrderingViolations(input contentScoringInput) (int, []fitness.Violation) {
	tokens := input.config.tieBreakerTokens()
	offenders := make([]string, 0)
	for _, match := range windowFunctionOrderBy.FindAllStringSubmatch(input.request.ProposedContent, -1) {
		clause := strings.Join(strings.Fields(match[1]), " ")
		if containsAnyToken(strings.ToLower(clause), tokens) {
			continue
		}
		offenders = append(offenders, truncateClause(clause))
	}
	if len(offenders) == 0 {
		return 0, nil
	}
	return len(offenders), []fitness.Violation{{
		FitnessFunction: "deterministic_ordering",
		CALMNode:        input.calmNode,
		File:            input.request.File,
		Value:           float64(len(offenders)),
		Limit:           input.rule.Threshold,
		Message: fmt.Sprintf(
			"File %q has %d window function ORDER BY clause(s) without a unique tie-breaker (limit %.0f): [%s]. "+
				"Append a unique column such as the primary key to each ORDER BY so the row order is deterministic.",
			input.request.File,
			len(offenders),
			input.rule.Threshold,
			strings.Join(offenders, "; "),
		),
	}}
}

// containsAnyToken reports whether clause contains any of the tokens as a
// substring. clause is expected to already be lowercased.
func containsAnyToken(clause string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(clause, strings.ToLower(token)) {
			return true
		}
	}
	return false
}

// truncateClause shortens an ORDER BY clause for display in a message.
func truncateClause(clause string) string {
	if len(clause) <= maxReportedOrderByClause {
		return clause
	}
	return clause[:maxReportedOrderByClause] + "..."
}
