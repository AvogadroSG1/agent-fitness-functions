// Package analyzer defines source-code metric analyzers used by calm-bridge.
package analyzer

// AnalysisResult contains normalized metrics from a language-specific analyzer.
type AnalysisResult struct {
	CALMNode   string           `json:"calm_node"`
	Language   string           `json:"language"`
	File       string           `json:"file"`
	Functions  []FunctionMetric `json:"functions"`
	FileMetric FileMetric       `json:"file_metrics"`
	Imports    ImportMetric     `json:"import_metrics"`
}

// FunctionMetric describes function-level complexity and size metrics.
type FunctionMetric struct {
	Name                 string `json:"name"`
	CyclomaticComplexity int    `json:"cyclomatic_complexity"`
	IsPublic             bool   `json:"is_public"`
	LOC                  int    `json:"loc"`
}

// FileMetric describes file-level source metrics.
type FileMetric struct {
	TotalLOC      int     `json:"total_loc"`
	LogicLOC      int     `json:"logic_loc"`
	PublicMethods int     `json:"public_methods"`
	LDR           float64 `json:"ldr"`
}

// ImportMetric describes dependency usage metrics.
type ImportMetric struct {
	Total  int      `json:"total"`
	Used   int      `json:"used"`
	Unused []string `json:"unused,omitempty"`
	DDC    float64  `json:"ddc"`
}
