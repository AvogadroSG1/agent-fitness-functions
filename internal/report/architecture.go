// Package report builds CALM architecture documents from analyzer metrics.
package report

import (
	"regexp"
	"strings"

	"github.com/poconnor/calm-poc/internal/analyzer"
)

// ArchitectureDocument is the CALM architecture document submitted to calm validate.
type ArchitectureDocument struct {
	Schema        string         `json:"$schema"`
	Nodes         []Node         `json:"nodes"`
	Relationships []Relationship `json:"relationships"`
}

// Node is a CALM architecture node with bridge fitness metadata.
type Node struct {
	UniqueID    string   `json:"unique-id"`
	NodeType    string   `json:"node-type"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Metadata    Metadata `json:"metadata"`
}

// Metadata contains CALM metadata emitted by calm-bridge.
type Metadata struct {
	Fitness       Fitness                `json:"fitness"`
	ModuleMetrics *analyzer.ModuleMetric `json:"module_metrics,omitempty"`
	FileMetrics   *FileMetrics           `json:"file_metrics,omitempty"`
	ImportMetrics *ImportMetrics         `json:"import_metrics,omitempty"`
}

// Fitness contains concrete values for the governance fitness functions.
type Fitness struct {
	CyclomaticComplexity float64 `json:"cyclomatic-complexity"`
	InterfaceWidth       float64 `json:"interface-width"`
	ImplementationDepth  float64 `json:"implementation-depth"`
	LogicDensity         float64 `json:"logic-density"`
	DependencyDiscipline float64 `json:"dependency-discipline"`
}

// FileMetrics contains file-level values used by AI Slop rules.
type FileMetrics struct {
	TotalLines int     `json:"total_lines"`
	LogicLines int     `json:"logic_lines"`
	LDR        float64 `json:"ldr"`
}

// ImportMetrics contains dependency usage values used by DDC.
type ImportMetrics struct {
	TotalImports int     `json:"total_imports"`
	UsedImports  int     `json:"used_imports"`
	DDC          float64 `json:"ddc"`
}

// Relationship connects the synthetic actor to the analyzed node.
type Relationship struct {
	UniqueID         string           `json:"unique-id"`
	Description      string           `json:"description"`
	RelationshipType RelationshipType `json:"relationship-type"`
}

// RelationshipType contains the CALM relationship kind.
type RelationshipType struct {
	Interacts Interacts `json:"interacts"`
}

// Interacts is the CALM actor-to-node relationship payload.
type Interacts struct {
	Actor string   `json:"actor"`
	Nodes []string `json:"nodes"`
}

// BuildArchitecture converts analyzer metrics into a CALM architecture document.
func BuildArchitecture(result analyzer.AnalysisResult) ArchitectureDocument {
	result = analyzer.EnsureModuleMetric(result)
	nodeID := calmID(result.CALMNode)
	actorID := nodeID + "-actor"
	return ArchitectureDocument{
		Schema: "https://calm.finos.org/release/1.2/meta/calm.json",
		Nodes: []Node{
			{
				UniqueID:    actorID,
				NodeType:    "actor",
				Name:        result.CALMNode + " Actor",
				Description: "Synthetic actor used to keep the analyzed CALM node reachable.",
				Metadata: Metadata{Fitness: Fitness{
					CyclomaticComplexity: 1,
					InterfaceWidth:       1,
					ImplementationDepth:  1,
					LogicDensity:         1,
					DependencyDiscipline: 1,
				}},
			},
			{
				UniqueID:    nodeID,
				NodeType:    "service",
				Name:        result.CALMNode,
				Description: "Architecture fitness metrics for " + result.File + ".",
				Metadata: Metadata{Fitness: Fitness{
					CyclomaticComplexity: float64(maxCyclomaticComplexity(result.Functions)),
					InterfaceWidth:       float64(result.ModuleMetric.PublicMethods),
					ImplementationDepth:  result.ModuleMetric.AverageLOCPerPublicMethod,
					LogicDensity:         result.FileMetric.LDR,
					DependencyDiscipline: result.Imports.DDC,
				},
					ModuleMetrics: &result.ModuleMetric,
					FileMetrics: &FileMetrics{
						TotalLines: result.FileMetric.TotalLOC,
						LogicLines: result.FileMetric.LogicLOC,
						LDR:        result.FileMetric.LDR,
					},
					ImportMetrics: &ImportMetrics{
						TotalImports: result.Imports.Total,
						UsedImports:  result.Imports.Used,
						DDC:          result.Imports.DDC,
					},
				},
			},
		},
		Relationships: []Relationship{
			{
				UniqueID:    actorID + "-to-" + nodeID,
				Description: "Synthetic actor uses the analyzed node.",
				RelationshipType: RelationshipType{Interacts: Interacts{
					Actor: actorID,
					Nodes: []string{nodeID},
				}},
			},
		},
	}
}

func maxCyclomaticComplexity(functions []analyzer.FunctionMetric) int {
	maximum := 1
	for _, function := range functions {
		if function.CyclomaticComplexity > maximum {
			maximum = function.CyclomaticComplexity
		}
	}
	return maximum
}

var nonIDCharacters = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func calmID(value string) string {
	id := strings.Trim(nonIDCharacters.ReplaceAllString(value, "-"), "-")
	if id == "" {
		return "analyzed-node"
	}
	return id
}
