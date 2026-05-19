// Package analyzer defines source-code metric analyzers used by calm-bridge.
package analyzer

// AnalysisResult contains normalized metrics from a language-specific analyzer.
type AnalysisResult struct {
	CALMNode     string           `json:"calm_node"`
	Language     string           `json:"language"`
	File         string           `json:"file"`
	Functions    []FunctionMetric `json:"functions"`
	ModuleMetric ModuleMetric     `json:"module_metrics"`
	FileMetric   FileMetric       `json:"file_metrics"`
	Imports      ImportMetric     `json:"import_metrics"`
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

// ModuleMetric describes module-level source metrics.
type ModuleMetric struct {
	PublicMethods             int     `json:"public_method_count"`
	TotalLOC                  int     `json:"total_loc"`
	PrivateLOC                int     `json:"private_loc"`
	AverageLOCPerPublicMethod float64 `json:"avg_loc_per_public_method"`
}

// ImportMetric describes dependency usage metrics.
type ImportMetric struct {
	Total  int      `json:"total"`
	Used   int      `json:"used"`
	Unused []string `json:"unused,omitempty"`
	DDC    float64  `json:"ddc"`
}

// BuildModuleMetric derives module metrics from analyzer output.
func BuildModuleMetric(fileMetric FileMetric, functions []FunctionMetric) ModuleMetric {
	publicLOC := 0
	for _, function := range functions {
		if function.IsPublic {
			publicLOC += function.LOC
		}
	}
	privateLOC := fileMetric.TotalLOC - publicLOC
	if privateLOC < 0 {
		privateLOC = 0
	}
	return ModuleMetric{
		PublicMethods:             fileMetric.PublicMethods,
		TotalLOC:                  fileMetric.TotalLOC,
		PrivateLOC:                privateLOC,
		AverageLOCPerPublicMethod: AverageLOCPerPublicMethod(fileMetric),
	}
}

// AverageLOCPerPublicMethod returns the implementation-depth metric.
func AverageLOCPerPublicMethod(metric FileMetric) float64 {
	if metric.PublicMethods == 0 {
		return 1
	}
	return float64(metric.LogicLOC) / float64(metric.PublicMethods)
}

// EnsureModuleMetric fills module metrics for older analyzer payloads.
func EnsureModuleMetric(result AnalysisResult) AnalysisResult {
	if result.ModuleMetric.PublicMethods == 0 &&
		result.ModuleMetric.TotalLOC == 0 &&
		result.ModuleMetric.PrivateLOC == 0 &&
		result.ModuleMetric.AverageLOCPerPublicMethod == 0 {
		result.ModuleMetric = BuildModuleMetric(result.FileMetric, result.Functions)
	}
	return result
}
