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
	evidence := Build(Graph{Nodes: []Node{{ID: "a", Name: "A", Kind: "class"}, {ID: "b", Name: "B", Kind: "class"}}, Edges: []Edge{{Source: "a", Destination: "b", Kind: "member", Evidence: []Evidence{{Location: Location{File: "a.cs", Line: 4}, ReferencedSymbol: "global::B"}}}}}).Relationships[0].RelationshipType.Connects.Protocol["evidence"]
	if evidence == nil {
		t.Fatal("dependency evidence must be retained in the CALM protocol")
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

func TestBuildPreservesMembersAndDistinguishesUnavailableMetadata(t *testing.T) {
	doc := Build(Graph{Nodes: []Node{
		{ID: "available", Name: "Available", Kind: "class", MemberExtractionAvailable: true, Members: []Member{{ID: "m1", Kind: "property", Name: "Quantity", DisplaySignature: "int Quantity", Accessibility: "public"}}},
		{ID: "unknown", Name: "Unknown", Kind: "class"},
	}})
	available := doc.Nodes[0].Metadata["agent-fitness-functions"].(map[string]any)
	unknown := doc.Nodes[1].Metadata["agent-fitness-functions"].(map[string]any)
	if _, ok := available["members"]; !ok {
		t.Fatal("available member extraction must retain an explicit members array")
	}
	if _, ok := unknown["members"]; ok {
		t.Fatal("unknown member extraction must not masquerade as an empty list")
	}
}

func TestBuildAccountsForMissingRelationshipEndpointsAndStableIDs(t *testing.T) {
	graph := Graph{Analysis: map[string]any{"completeness": "complete-within-scope"}, Nodes: []Node{{ID: "a", Name: "A", Kind: "class"}}, Edges: []Edge{
		{Source: "a", Destination: "missing", Kind: "member"},
		{Source: "a", Destination: "a", Kind: "construction"},
	}}
	doc := Build(graph)
	analysis := doc.Metadata["agent-fitness-functions"].(map[string]any)
	if analysis["relationships_emitted"] != 1 || analysis["relationships_omitted"] != 1 || analysis["completeness"] != "partial" {
		t.Fatalf("omission accounting = %#v", analysis)
	}
	first := doc.Relationships[0].UniqueID
	reordered := Build(Graph{Nodes: graph.Nodes, Edges: []Edge{graph.Edges[1], graph.Edges[0]}})
	if reordered.Relationships[0].UniqueID != first {
		t.Fatalf("relationship ID changed after unrelated ordering: %q vs %q", first, reordered.Relationships[0].UniqueID)
	}
}

func TestBuildRetainsExtractorOmissionReasons(t *testing.T) {
	doc := Build(Graph{Analysis: map[string]any{"relationships_excluded": float64(3), "omissions_by_reason": map[string]any{"external-definition": float64(3)}}, Nodes: []Node{{ID: "a", Name: "A", Kind: "class"}}})
	analysis := doc.Metadata["agent-fitness-functions"].(map[string]any)
	if analysis["relationships_omitted"] != 3 {
		t.Fatalf("excluded relationships were not retained: %#v", analysis)
	}
	reasons := analysis["omissions_by_reason"].(map[string]int)
	if reasons["external-definition"] != 3 {
		t.Fatalf("omission reasons were not retained: %#v", reasons)
	}
}

func TestValidateRejectsMalformedMemberCollection(t *testing.T) {
	_, err := Validate([]byte(`{"$schema":"x","nodes":[{"unique-id":"a","metadata":{"agent-fitness-functions":{"member_extraction_available":true,"members":{}}}}],"relationships":[]}`))
	if err == nil {
		t.Fatal("expected malformed member collection to be rejected")
	}
}
