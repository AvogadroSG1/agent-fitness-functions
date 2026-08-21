package fitness

import (
	"encoding/json"
	"os"
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

func TestPersistedValidationResultRetainsCALMNodeWireContract(t *testing.T) {
	content, err := os.ReadFile("testdata/persisted-calm-node-validation-result.json")
	if err != nil {
		t.Fatalf("os.ReadFile(persisted validation result) error = %v, want nil", err)
	}

	var result ValidationResult
	if err := json.Unmarshal(content, &result); err != nil {
		t.Fatalf("json.Unmarshal(ValidationResult) error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(ValidationResult) error = %v, want nil", err)
	}

	var payload struct {
		Violations []map[string]json.RawMessage `json:"violations"`
	}
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal(serialized ValidationResult) error = %v, want nil", err)
	}
	if len(payload.Violations) != 1 {
		t.Fatalf("serialized violations = %d, want 1", len(payload.Violations))
	}
	violation := payload.Violations[0]
	if _, ok := violation["stack_node"]; ok {
		t.Errorf("serialized violation contains stack_node, want only calm_node: %s", encoded)
	}
	calmNode, ok := violation["calm_node"]
	if !ok || string(calmNode) != `"go-module"` {
		t.Errorf("serialized violation calm_node = %s, %v, want %q", calmNode, ok, "go-module")
	}
}
