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
