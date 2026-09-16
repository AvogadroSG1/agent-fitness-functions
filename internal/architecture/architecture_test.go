package architecture

import (
	"encoding/json"
	"testing"
)

func TestBuildSortsGraphAndAggregatesLocations(t *testing.T) {
	doc := Build(Graph{
		Nodes: []Node{{ID: "b", Name: "B", Kind: "class"}, {ID: "a", Name: "A", Kind: "interface"}},
		Edges: []Edge{{Source: "b", Destination: "a", Kind: "member", Locations: []Location{{File: "z.cs", Line: 2}, {File: "a.cs", Line: 1}}}},
	})
	if doc.Nodes[0].UniqueID != "a" || doc.Relationships[0].RelationshipType.Connects.Source.Node != "b" {
		t.Fatalf("document was not deterministic: %+v", doc)
	}
	raw, _ := json.Marshal(doc)
	if string(raw) == "" || !contains(string(raw), `"source":{"node":"b"}`) {
		t.Fatalf("connects endpoints must use CALM node references: %s", raw)
	}
	if _, err := Validate(raw); err != nil {
		t.Fatal(err)
	}
}

func contains(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}

func TestValidateRejectsUnknownRelationship(t *testing.T) {
	_, err := Validate([]byte(`{"$schema":"x","nodes":[{"unique-id":"a"}],"relationships":[{"relationship-type":{"connects":{"source":"a","destination":"missing"}}}]}`))
	if err == nil {
		t.Fatal("expected unknown relationship to be rejected")
	}
}

func TestBuildAggregatesFactsWithoutInventingFitness(t *testing.T) {
	doc := Build(Graph{Language: "csharp", Nodes: []Node{{ID: "a", Name: "A", Kind: "class"}, {ID: "b", Name: "B", Kind: "interface"}}, Edges: []Edge{
		{Source: "a", Destination: "b", Kind: "implements", Locations: []Location{{File: "z.cs", Line: 3}}},
		{Source: "a", Destination: "b", Kind: "parameter", Locations: []Location{{File: "a.cs", Line: 2}}},
	}})
	if len(doc.Relationships) != 1 {
		t.Fatalf("got %d relationships, want one aggregated relationship", len(doc.Relationships))
	}
	if _, ok := doc.Nodes[0].Metadata["fitness"]; ok {
		t.Fatal("unmeasured fitness must not be serialized")
	}
	metadata := doc.Relationships[0].RelationshipType.Connects.Metadata
	if metadata == nil || metadata["dependency-kinds"] == nil || metadata["evidence"] == nil {
		t.Fatalf("dependency evidence missing from metadata: %#v", metadata)
	}
	first := doc.Relationships[0].UniqueID
	withUnrelated := Build(Graph{Language: "csharp", Nodes: []Node{{ID: "a", Name: "A", Kind: "class"}, {ID: "b", Name: "B", Kind: "interface"}, {ID: "z", Name: "Z", Kind: "enum"}}, Edges: []Edge{{Source: "a", Destination: "b", Kind: "implements"}, {Source: "a", Destination: "b", Kind: "parameter"}, {Source: "z", Destination: "z", Kind: "field"}}})
	if withUnrelated.Relationships[0].UniqueID != first {
		t.Fatal("unrelated graph changes renamed an existing relationship")
	}
}

func TestBuildAllowsIsolatedNodesWithEmptyRelationships(t *testing.T) {
	doc := Build(Graph{Language: "csharp", Nodes: []Node{{ID: "a", Name: "A", Kind: "record-class"}}})
	if doc.Relationships == nil || len(doc.Relationships) != 0 {
		t.Fatalf("isolated graph relationships = %#v, want []", doc.Relationships)
	}
}
