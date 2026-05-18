package analyzer

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

// BaselineReport summarizes analyzer results for one repository.
type BaselineReport struct {
	Repository string           `json:"repository"`
	Language   string           `json:"language"`
	Generated  time.Time        `json:"generated"`
	Summary    BaselineSummary  `json:"summary"`
	Results    []AnalysisResult `json:"results"`
}

// BaselineSummary contains distributions used for threshold calibration.
type BaselineSummary struct {
	FileCount                 int     `json:"file_count"`
	FunctionCount             int     `json:"function_count"`
	P90CyclomaticComplexity   int     `json:"p90_cyclomatic_complexity"`
	P90PublicMethods          int     `json:"p90_public_methods"`
	P10AverageLOCPerPublicAPI float64 `json:"p10_avg_loc_per_public_method"`
	P10LogicDensityRatio      float64 `json:"p10_logic_density_ratio"`
	P10DependencyDiscipline   float64 `json:"p10_dependency_discipline"`
}

// WriteBaselineReport writes a JSON baseline report for threshold calibration.
func WriteBaselineReport(path, repository, language string, results []AnalysisResult) error {
	report := BaselineReport{
		Repository: repository,
		Language:   language,
		Generated:  time.Now().UTC(),
		Summary:    summarize(results),
		Results:    results,
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func summarize(results []AnalysisResult) BaselineSummary {
	cc := make([]int, 0)
	publicMethods := make([]int, 0, len(results))
	avgLOCPerPublic := make([]float64, 0, len(results))
	ldr := make([]float64, 0, len(results))
	ddc := make([]float64, 0, len(results))
	for _, result := range results {
		publicMethods = append(publicMethods, result.FileMetric.PublicMethods)
		if result.FileMetric.PublicMethods > 0 {
			avgLOCPerPublic = append(avgLOCPerPublic, float64(result.FileMetric.LogicLOC)/float64(result.FileMetric.PublicMethods))
		}
		if result.FileMetric.TotalLOC > 0 {
			ldr = append(ldr, result.FileMetric.LDR)
		}
		if result.Imports.Total > 0 {
			ddc = append(ddc, result.Imports.DDC)
		}
		for _, fn := range result.Functions {
			cc = append(cc, fn.CyclomaticComplexity)
		}
	}
	return BaselineSummary{
		FileCount:                 len(results),
		FunctionCount:             len(cc),
		P90CyclomaticComplexity:   percentileInt(cc, 0.90),
		P90PublicMethods:          percentileInt(publicMethods, 0.90),
		P10AverageLOCPerPublicAPI: percentileFloat(avgLOCPerPublic, 0.10),
		P10LogicDensityRatio:      percentileFloat(ldr, 0.10),
		P10DependencyDiscipline:   percentileFloat(ddc, 0.10),
	}
}

func percentileInt(values []int, percentile float64) int {
	if len(values) == 0 {
		return 0
	}
	sort.Ints(values)
	index := percentileIndex(len(values), percentile)
	return values[index]
}

func percentileFloat(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	index := percentileIndex(len(values), percentile)
	return values[index]
}

func percentileIndex(length int, percentile float64) int {
	index := int(percentile * float64(length-1))
	if index < 0 {
		return 0
	}
	if index >= length {
		return length - 1
	}
	return index
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}
