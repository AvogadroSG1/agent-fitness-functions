// Package fitness defines the validation wire contract shared by client and server roles.
package fitness

// Status is the closed set of validation outcomes returned by /check.
type Status string

const (
	// StatusPass indicates that no active fitness function blocked the change.
	StatusPass Status = "pass"
	// StatusBlock indicates that an active fitness function rejected the change.
	StatusBlock Status = "block"
	// StatusAdvisory indicates that an active fitness function reported guidance without blocking.
	StatusAdvisory Status = "advisory"
)

// ValidationRequest is the JSON body accepted by POST /check.
type ValidationRequest struct {
	Repo            string `json:"repo"`
	File            string `json:"file"`
	ProposedContent string `json:"proposed_content"`
	Language        string `json:"language"`
}

// ValidationResult is the JSON response returned by POST /check.
type ValidationResult struct {
	Status     Status      `json:"status"`
	Warming    bool        `json:"warming,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
}

// Violation describes one architectural fitness function failure.
type Violation struct {
	FitnessFunction string  `json:"fitness_function"`
	CALMNode        string  `json:"calm_node"`
	File            string  `json:"file,omitempty"`
	Function        string  `json:"function,omitempty"`
	Value           float64 `json:"value"`
	Limit           float64 `json:"limit"`
	Message         string  `json:"message"`
}
