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
// enforced, the CALM node the analyzed file belongs to, and the analyzer's
// findings for that file. A scorer reads the content or the findings depending
// on whether its detection is textual or syntax-tree based.
type contentScoringInput struct {
	request  fitness.ValidationRequest
	config   Config
	rule     calm.FitnessRule
	calmNode string
	findings []analyzer.Finding
}

// contentScorer scores one generalized fitness function from the proposed file
// content or from the analyzer's findings for it, rather than from analyzer
// metrics. score returns the per-file violation count (recorded on
// AnalysisResult.RuleCounts so the generated CALM document carries it) and the
// violations to report.
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
	{name: "layer-sovereignty", operator: "lte", score: layerSovereigntyViolations},
	{name: "temporal-purity", operator: "lte", score: temporalPurityViolations},
	{name: "sql-composition-safety", operator: "lte", score: sqlCompositionSafetyViolations},
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
	// No analyzer populates RuleCounts today; copying rather than writing in
	// place is deliberate, so a future analyzer-owned map is never mutated.
	counts := make(map[string]int, len(result.RuleCounts)+len(contentScorers))
	for name, count := range result.RuleCounts {
		counts[name] = count
	}
	for _, scorer := range contentScorers {
		rule, ok := pattern.FitnessFunctions[scorer.name]
		if !ok || rule.Operator != scorer.operator || !config.Enabled(scorer.name) {
			continue
		}
		count, scored := scorer.score(contentScoringInput{
			request:  request,
			config:   config,
			rule:     rule,
			calmNode: result.CALMNode,
			findings: result.Findings,
		})
		counts[scorer.name] = count
		violations = append(violations, scored...)
	}
	result.RuleCounts = counts
	return violations
}

// windowFunctionOpen matches the start of a SQL window function's OVER clause.
// Go's regexp is RE2 and cannot balance parentheses, so the window body is
// delimited by walking characters from the opening paren instead.
var windowFunctionOpen = regexp.MustCompile(`(?i)OVER\s*\(`)

// orderByKeyword matches the ORDER BY keyword and its trailing whitespace.
var orderByKeyword = regexp.MustCompile(`(?is)ORDER\s+BY\s+`)

// maxReportedOrderByClause bounds how much of an offending ORDER BY clause is
// echoed back in a violation message.
const maxReportedOrderByClause = 80

// deterministicOrderingViolations counts window-function ORDER BY clauses in
// the proposed content that carry no configured tie-breaker token, and reports
// them as a single per-file violation.
func deterministicOrderingViolations(input contentScoringInput) (int, []fitness.Violation) {
	tokens := input.config.TieBreakerTokens()
	offenders := make([]string, 0)
	for _, clause := range windowOrderByClauses(input.request.ProposedContent) {
		if containsTieBreaker(clause, tokens) {
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

// layerSovereigntyViolations counts forbidden-pattern references in the
// proposed content across every configured layer whose path globs match the
// request's file, and reports them as a single per-file violation. A file
// that matches no layer, or matches layers with no forbidden hits,
// contributes nothing.
func layerSovereigntyViolations(input contentScoringInput) (int, []fitness.Violation) {
	layerNames := make([]string, 0)
	matches := make([]string, 0)
	for _, layer := range input.config.LayerRules() {
		if !layer.MatchesPath(input.request.File) {
			continue
		}
		found := layer.ForbiddenMatches(input.request.ProposedContent)
		if len(found) == 0 {
			continue
		}
		layerNames = append(layerNames, layer.Name)
		matches = append(matches, found...)
	}
	if len(matches) == 0 {
		return 0, nil
	}
	return len(matches), []fitness.Violation{{
		FitnessFunction: "layer_sovereignty",
		CALMNode:        input.calmNode,
		File:            input.request.File,
		Value:           float64(len(matches)),
		Limit:           input.rule.Threshold,
		Message: fmt.Sprintf(
			"File %q belongs to layer(s) %s, which must not reference %d forbidden pattern(s) (limit %.0f): [%s]. "+
				"Route access through the layer's sanctioned interface instead of referencing these patterns directly.",
			input.request.File,
			strings.Join(layerNames, ", "),
			len(matches),
			input.rule.Threshold,
			strings.Join(matches, "; "),
		),
	}}
}

// maxReportedFindings bounds how many finding locations a violation message
// names before it summarizes the rest.
const maxReportedFindings = 3

// temporalPurityViolations counts the temporal-purity findings the language
// analyzer detected in the file's syntax tree — timestamps constructed without
// an explicit time zone — and reports them as a single per-file violation. A
// language whose analyzer implements no temporal detections contributes no
// findings and therefore no violation.
func temporalPurityViolations(input contentScoringInput) (int, []fitness.Violation) {
	found := findingsForRule(input.findings, "temporal-purity")
	if len(found) == 0 {
		return 0, nil
	}
	return len(found), []fitness.Violation{{
		FitnessFunction: "temporal_purity",
		CALMNode:        input.calmNode,
		File:            input.request.File,
		Value:           float64(len(found)),
		Limit:           input.rule.Threshold,
		Message: fmt.Sprintf(
			"File %q constructs %d naive timestamp(s) (limit %.0f): [%s]. "+
				"Construct timestamps with an explicit time zone, such as datetime.now(timezone.utc), "+
				"so the recorded instant is unambiguous.",
			input.request.File,
			len(found),
			input.rule.Threshold,
			strings.Join(describeFindings(found), "; "),
		),
	}}
}

// sqlCompositionSafetyViolations counts the sql-composition-safety findings
// the language analyzer detected in the file's syntax tree — SQL statements
// composed via f-string interpolation, %-formatting, or .format() and passed
// to execute()/executemany() — and reports them as a single per-file
// violation. A language whose analyzer implements no SQL-composition
// detections contributes no findings and therefore no violation.
func sqlCompositionSafetyViolations(input contentScoringInput) (int, []fitness.Violation) {
	found := findingsForRule(input.findings, "sql-composition-safety")
	if len(found) == 0 {
		return 0, nil
	}
	return len(found), []fitness.Violation{{
		FitnessFunction: "sql_composition_safety",
		CALMNode:        input.calmNode,
		File:            input.request.File,
		Value:           float64(len(found)),
		Limit:           input.rule.Threshold,
		Message: fmt.Sprintf(
			"File %q composes %d SQL statement(s) via string interpolation (limit %.0f): [%s]. "+
				"Use parameterized queries or a SQL composition API instead of building statements with "+
				"f-strings, %%-formatting, or .format().",
			input.request.File,
			len(found),
			input.rule.Threshold,
			strings.Join(describeFindings(found), "; "),
		),
	}}
}

// findingsForRule returns the analyzer findings attributed to one kebab-case
// fitness function.
func findingsForRule(findings []analyzer.Finding, rule string) []analyzer.Finding {
	matched := make([]analyzer.Finding, 0, len(findings))
	for _, finding := range findings {
		if finding.Rule == rule {
			matched = append(matched, finding)
		}
	}
	return matched
}

// describeFindings names the first maxReportedFindings locations as
// "<kind> at line <n>", summarizing any remainder as a count.
func describeFindings(findings []analyzer.Finding) []string {
	described := make([]string, 0, maxReportedFindings+1)
	for _, finding := range findings {
		if len(described) == maxReportedFindings {
			return append(described, fmt.Sprintf("and %d more", len(findings)-maxReportedFindings))
		}
		described = append(described, fmt.Sprintf("%s at line %d", finding.Kind, finding.Line))
	}
	return described
}

// windowOrderByClauses returns the ordering expression of every SQL window
// function in content, whitespace-normalized. A window's ORDER BY runs to the
// window's own closing paren, so a nested call such as COALESCE(a, b) does not
// truncate the clause and hide a tie-breaker that follows it.
func windowOrderByClauses(content string) []string {
	clauses := make([]string, 0)
	for _, match := range windowFunctionOpen.FindAllStringIndex(content, -1) {
		body, ok := balancedParenBody(content, match[1]-1)
		if !ok {
			continue
		}
		clause, ok := trailingOrderByClause(body)
		if !ok {
			continue
		}
		clauses = append(clauses, strings.Join(strings.Fields(clause), " "))
	}
	return clauses
}

// balancedParenBody returns the text between the paren at open and its
// matching close paren. It reports false when the parens are unbalanced.
func balancedParenBody(content string, open int) (string, bool) {
	depth := 0
	for i := open; i < len(content); i++ {
		switch content[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return content[open+1 : i], true
			}
		}
	}
	return "", false
}

// trailingOrderByClause returns everything after the last ORDER BY that occurs
// at paren depth zero in body, which is the window's own ordering expression
// rather than one belonging to a nested subexpression.
func trailingOrderByClause(body string) (string, bool) {
	clause := ""
	found := false
	for _, match := range orderByKeyword.FindAllStringIndex(body, -1) {
		if parenDepthAt(body, match[0]) != 0 {
			continue
		}
		clause = body[match[1]:]
		found = true
	}
	return clause, found
}

// parenDepthAt returns the paren nesting depth of text immediately before index.
func parenDepthAt(text string, index int) int {
	depth := 0
	for i := 0; i < index; i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	return depth
}

// identifierRun matches one lowercased SQL identifier.
var identifierRun = regexp.MustCompile(`[a-z0-9_]+`)

// containsTieBreaker reports whether any identifier in clause satisfies one of
// the configured tie-breaker tokens. Matching is per identifier, never a bare
// substring of the clause, so "paid_amount" is not a tie-breaker for "id".
func containsTieBreaker(clause string, tokens []string) bool {
	for _, identifier := range identifierRun.FindAllString(strings.ToLower(clause), -1) {
		for _, token := range tokens {
			if identifierMatchesToken(identifier, strings.ToLower(token)) {
				return true
			}
		}
	}
	return false
}

// identifierMatchesToken reports whether one identifier satisfies one token:
// an exact match, or a suffix match on a "_" boundary. "employee_id" satisfies
// both "id" and "_id"; "paid" and "grid_position" satisfy neither.
func identifierMatchesToken(identifier, token string) bool {
	switch {
	case token == "" || !strings.HasSuffix(identifier, token):
		return false
	case identifier == token || strings.HasPrefix(token, "_"):
		return true
	default:
		return identifier[len(identifier)-len(token)-1] == '_'
	}
}

// truncateClause shortens an ORDER BY clause for display in a message,
// counting runes so multi-byte characters are never split.
func truncateClause(clause string) string {
	runes := []rune(clause)
	if len(runes) <= maxReportedOrderByClause {
		return clause
	}
	return string(runes[:maxReportedOrderByClause]) + "..."
}
