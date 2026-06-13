package fitness

import (
	"encoding/json"
	"testing"
)

func TestValidationWireContractUsesSharedVocabulary(t *testing.T) {
	request := ValidationRequest{
		Repo:            "repo-one",
		File:            "internal/parser/parser.go",
		ProposedContent: "package parser",
		Language:        "go",
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal(ValidationRequest) error = %v, want nil", err)
	}
	wantRequest := `{"repo":"repo-one","file":"internal/parser/parser.go","proposed_content":"package parser","language":"go"}`
	if string(requestBody) != wantRequest {
		t.Fatalf("json.Marshal(ValidationRequest) = %s, want %s", requestBody, wantRequest)
	}

	result := ValidationResult{
		Status:  StatusBlock,
		Warming: true,
		Violations: []Violation{{
			FitnessFunction: "cyclomatic_complexity",
			CALMNode:        "go-module",
			File:            "internal/parser/parser.go",
			Function:        "Parse",
			Value:           10,
			Limit:           9,
			Message:         "too complex",
		}},
	}
	resultBody, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(ValidationResult) error = %v, want nil", err)
	}
	wantResult := `{"status":"block","warming":true,"violations":[{"fitness_function":"cyclomatic_complexity","calm_node":"go-module","file":"internal/parser/parser.go","function":"Parse","value":10,"limit":9,"message":"too complex"}]}`
	if string(resultBody) != wantResult {
		t.Fatalf("json.Marshal(ValidationResult) = %s, want %s", resultBody, wantResult)
	}
}
