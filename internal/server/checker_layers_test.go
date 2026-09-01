package server

import (
	"strings"
	"testing"

	"github.com/AvogadroSG1/agent-fitness-functions/internal/fitness"
)

// Red tests for calm-poc-fua (S4): layer-sovereignty is scored in the checker
// by matching the request's file path against configured layer globs and the
// proposed content against that layer's forbidden regexes. The violation
// count is the number of forbidden patterns that matched, reported against
// the pattern's lte-0 rule, with layer name and matched patterns in the
// message.

const bronzeLayerConfig = `{
	"enforcement-mode": "block",
	"fitness-functions": {` + contentScoredOnlyFunctions + `,
		"layer-sovereignty": true
	},
	"fitness-function-settings": {
		"layer-sovereignty": {
			"layers": [
				{
					"name": "bronze",
					"paths": ["src/bronze/**"],
					"forbidden-patterns": ["\\bsilver\\.", "\\bgold\\."]
				}
			]
		}
	}
}`

func TestHandlerCheckBlocksLayerSovereigntyViolation(t *testing.T) {
	repo := "repo-layers"
	server := newContentScoringServer(t, repo, bronzeLayerConfig)

	source := "package bronze\n\nconst query = `SELECT * FROM silver.orders JOIN gold.dim_person USING (person_key)`\n"
	body := postCheck(t, server.URL, repo, "src/bronze/orders.go", source)
	if body.Status != fitness.StatusBlock {
		t.Fatalf("status = %q, want block for bronze file referencing silver/gold", body.Status)
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %+v, want one layer-sovereignty violation", body.Violations)
	}
	violation := body.Violations[0]
	if violation.FitnessFunction != "layer_sovereignty" || violation.Value != 2 || violation.Limit != 0 {
		t.Fatalf("violation = %+v, want layer_sovereignty value 2 limit 0", violation)
	}
	for _, fragment := range []string{"bronze", `\bsilver\.`} {
		if !strings.Contains(violation.Message, fragment) {
			t.Fatalf("message = %q, want layer name and matched pattern %q", violation.Message, fragment)
		}
	}
}

func TestHandlerCheckPassesFileOutsideConfiguredLayers(t *testing.T) {
	repo := "repo-layers-outside"
	server := newContentScoringServer(t, repo, bronzeLayerConfig)

	source := "package silver\n\nconst query = `SELECT * FROM silver.orders`\n"
	body := postCheck(t, server.URL, repo, "src/silver/orders.go", source)
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for file outside bronze layer", body.Status, body.Violations)
	}
}

func TestHandlerCheckPassesBronzeFileWithoutForbiddenReferences(t *testing.T) {
	repo := "repo-layers-clean"
	server := newContentScoringServer(t, repo, bronzeLayerConfig)

	source := "package bronze\n\nconst query = `SELECT * FROM raw_bamboo.employees`\n"
	body := postCheck(t, server.URL, repo, "src/bronze/employees.go", source)
	if body.Status != fitness.StatusPass {
		t.Fatalf("status = %q (violations %+v), want pass for clean bronze file", body.Status, body.Violations)
	}
}
