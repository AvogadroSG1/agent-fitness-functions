package analyzer

import (
	"encoding/json"
	"os"
	"sort"
	"time"
)

// BaselineReport summarizes analyzer results for one repository.
type BaselineReport struct {
	Repository    string                `json:"repository"`
	Language      string                `json:"language"`
	Generated     time.Time             `json:"generated"`
	Summary       BaselineSummary       `json:"summary"`
	Distributions BaselineDistributions `json:"distributions"`
	Results       []AnalysisResult      `json:"results"`
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

// BaselineDistributions contains raw sorted metric values used to set thresholds.
type BaselineDistributions struct {
	CyclomaticComplexity      []int     `json:"cyclomatic_complexity"`
	PublicMethods             []int     `json:"public_methods"`
	AverageLOCPerPublicMethod []float64 `json:"avg_loc_per_public_method"`
	LogicDensityRatio         []float64 `json:"logic_density_ratio"`
	DependencyDiscipline      []float64 `json:"dependency_discipline"`
}

// WriteBaselineReport writes a JSON baseline report for threshold calibration.
func WriteBaselineReport(path, repository, language string, results []AnalysisResult) error {
	distributions := distributions(results)
	report := BaselineReport{
		Repository:    repository,
		Language:      language,
		Generated:     time.Now().UTC(),
		Summary:       summarize(distributions, len(results)),
		Distributions: distributions,
		Results:       results,
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(path, content, 0o644)
}

func distributions(results []AnalysisResult) BaselineDistributions {
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
	sort.Ints(cc)
	sort.Ints(publicMethods)
	sort.Float64s(avgLOCPerPublic)
	sort.Float64s(ldr)
	sort.Float64s(ddc)
	return BaselineDistributions{
		CyclomaticComplexity:      cc,
		PublicMethods:             publicMethods,
		AverageLOCPerPublicMethod: avgLOCPerPublic,
		LogicDensityRatio:         ldr,
		DependencyDiscipline:      ddc,
	}
}

func summarize(distributions BaselineDistributions, fileCount int) BaselineSummary {
	return BaselineSummary{
		FileCount:                 fileCount,
		FunctionCount:             len(distributions.CyclomaticComplexity),
		P90CyclomaticComplexity:   percentileInt(distributions.CyclomaticComplexity, 0.90),
		P90PublicMethods:          percentileInt(distributions.PublicMethods, 0.90),
		P10AverageLOCPerPublicAPI: percentileFloat(distributions.AverageLOCPerPublicMethod, 0.10),
		P10LogicDensityRatio:      percentileFloat(distributions.LogicDensityRatio, 0.10),
		P10DependencyDiscipline:   percentileFloat(distributions.DependencyDiscipline, 0.10),
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
