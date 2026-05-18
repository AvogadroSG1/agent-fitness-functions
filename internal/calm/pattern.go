package calm

import (
	"encoding/json"
	"fmt"
	"os"
)

// Pattern is the governance pattern used by the bridge for CALM validation.
type Pattern struct {
	Schema           string                 `json:"$schema"`
	ID               string                 `json:"$id"`
	Title            string                 `json:"title"`
	Description      string                 `json:"description"`
	FitnessFunctions map[string]FitnessRule `json:"-"`
}

// FitnessRule defines one architecture fitness threshold in governance.json.
type FitnessRule struct {
	Description string  `json:"description"`
	Threshold   float64 `json:"threshold"`
	Operator    string  `json:"operator"`
	Unit        string  `json:"unit"`
}

type patternSchema struct {
	Schema      string `json:"$schema"`
	ID          string `json:"$id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Properties  struct {
		Nodes struct {
			Items struct {
				Properties struct {
					Metadata struct {
						Properties struct {
							Fitness struct {
								Properties map[string]fitnessProperty `json:"properties"`
							} `json:"fitness"`
						} `json:"properties"`
					} `json:"metadata"`
				} `json:"properties"`
			} `json:"items"`
		} `json:"nodes"`
	} `json:"properties"`
}

type fitnessProperty struct {
	Description string   `json:"description"`
	Maximum     *float64 `json:"maximum"`
	Minimum     *float64 `json:"minimum"`
}

// LoadPattern reads and validates a CALM governance pattern file.
func LoadPattern(path string) (Pattern, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Pattern{}, fmt.Errorf("reading pattern %s: %w", path, err)
	}
	var pattern Pattern
	var schema patternSchema
	if err := json.Unmarshal(content, &schema); err != nil {
		return Pattern{}, fmt.Errorf("parsing pattern %s: %w", path, err)
	}
	pattern.Schema = schema.Schema
	pattern.ID = schema.ID
	pattern.Title = schema.Title
	pattern.Description = schema.Description
	pattern.FitnessFunctions = fitnessFunctions(schema.Properties.Nodes.Items.Properties.Metadata.Properties.Fitness.Properties)
	if len(pattern.FitnessFunctions) == 0 {
		return Pattern{}, fmt.Errorf("pattern %s must define fitness-functions", path)
	}
	return pattern, nil
}

func fitnessFunctions(properties map[string]fitnessProperty) map[string]FitnessRule {
	functions := make(map[string]FitnessRule, len(properties))
	for name, property := range properties {
		switch {
		case property.Maximum != nil:
			functions[name] = FitnessRule{
				Description: property.Description,
				Threshold:   *property.Maximum,
				Operator:    "lte",
				Unit:        unitForFitnessFunction(name),
			}
		case property.Minimum != nil:
			functions[name] = FitnessRule{
				Description: property.Description,
				Threshold:   *property.Minimum,
				Operator:    "gte",
				Unit:        unitForFitnessFunction(name),
			}
		}
	}
	return functions
}

func unitForFitnessFunction(name string) string {
	switch name {
	case "cyclomatic-complexity":
		return "function"
	case "interface-width", "implementation-depth":
		return "module"
	default:
		return "file"
	}
}
