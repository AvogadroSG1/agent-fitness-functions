// Package architecture contains the language-neutral observed architecture graph.
package architecture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}
type Node struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Project       string     `json:"project,omitempty"`
	CanonicalName string     `json:"canonical_name,omitempty"`
	Locations     []Location `json:"locations,omitempty"`
}
type Edge struct {
	Source      string     `json:"source"`
	Destination string     `json:"destination"`
	Kind        string     `json:"kind"`
	Locations   []Location `json:"locations,omitempty"`
}
type Analysis struct {
	Completeness     string       `json:"completeness"`
	AnalyzedProjects []string     `json:"analyzed_projects"`
	SkippedProjects  []string     `json:"skipped_projects"`
	TargetFrameworks []string     `json:"target_frameworks"`
	Diagnostics      []Diagnostic `json:"diagnostics"`
}
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Project  string `json:"project,omitempty"`
}
type Graph struct {
	SchemaVersion string   `json:"schema_version"`
	Language      string   `json:"language"`
	Nodes         []Node   `json:"nodes"`
	Edges         []Edge   `json:"edges"`
	Analysis      Analysis `json:"analysis"`
}

type Document struct {
	Schema        string         `json:"$schema"`
	Nodes         []CALMNode     `json:"nodes"`
	Relationships []Relationship `json:"relationships"`
}
type CALMNode struct {
	UniqueID    string         `json:"unique-id"`
	NodeType    string         `json:"node-type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}
type Relationship struct {
	UniqueID         string           `json:"unique-id"`
	Description      string           `json:"description,omitempty"`
	RelationshipType RelationshipType `json:"relationship-type"`
}
type RelationshipType struct {
	Connects   ConnectsBody    `json:"connects"`
	ComposedOf *ComposedOfBody `json:"composed-of,omitempty"`
}

// Connects is retained as a compatibility alias for callers of the first implementation.
type Connects struct {
	Connects ConnectsBody `json:"connects"`
}
type ConnectsBody struct {
	Source      NodeReference  `json:"source"`
	Destination NodeReference  `json:"destination"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}
type ComposedOfBody struct {
	Container NodeReference `json:"container"`
	Component NodeReference `json:"component"`
}
type NodeReference struct {
	Node string `json:"node"`
}

const SchemaURL = "https://calm.finos.org/release/1.2/meta/calm.json"
const schemaVersion = "1"

var supportedKinds = map[string]bool{"inherits": true, "implements": true, "field": true, "property": true, "parameter": true, "return": true, "constructs": true, "calls": true, "construction": true, "inheritance": true, "member": true}

func Build(g Graph) Document {
	nodes := append([]Node(nil), g.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	d := Document{Schema: SchemaURL, Nodes: []CALMNode{}, Relationships: []Relationship{}}
	ids := map[string]bool{}
	for _, n := range nodes {
		ids[n.ID] = true
		d.Nodes = append(d.Nodes, CALMNode{UniqueID: n.ID, NodeType: "code-element", Name: n.Name, Description: "Observed code element " + n.Name, Metadata: map[string]any{"agent-fitness-functions": map[string]any{"language": g.Language, "symbol-kind": n.Kind, "project": n.Project, "canonical-name": n.CanonicalName, "locations": sortedLocations(n.Locations), "c4-level": "code"}}})
	}
	type group struct {
		source, dest string
		kinds        map[string][]Location
	}
	groups := map[string]*group{}
	for _, e := range g.Edges {
		if !ids[e.Source] || !ids[e.Destination] {
			continue
		}
		key := e.Source + "\x00" + e.Destination
		x := groups[key]
		if x == nil {
			x = &group{source: e.Source, dest: e.Destination, kinds: map[string][]Location{}}
			groups[key] = x
		}
		x.kinds[e.Kind] = append(x.kinds[e.Kind], e.Locations...)
	}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		x := groups[k]
		kinds := make([]string, 0, len(x.kinds))
		for kind := range x.kinds {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		evidence := []any{}
		for _, kind := range kinds {
			evidence = append(evidence, map[string]any{"kind": kind, "locations": sortedLocations(x.kinds[kind])})
		}
		d.Relationships = append(d.Relationships, Relationship{UniqueID: "relationship-" + stableDigest(x.source, x.dest, strings.Join(kinds, ",")), Description: "Observed dependency", RelationshipType: RelationshipType{Connects: ConnectsBody{Source: NodeReference{Node: x.source}, Destination: NodeReference{Node: x.dest}, Metadata: map[string]any{"dependency-kinds": kinds, "evidence": evidence}}}})
	}
	return d
}
func sortedLocations(in []Location) []Location {
	out := append([]Location(nil), in...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Column < out[j].Column
	})
	uniq := out[:0]
	for _, x := range out {
		if len(uniq) == 0 || x != uniq[len(uniq)-1] {
			uniq = append(uniq, x)
		}
	}
	return uniq
}
func stableDigest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}
func StableID(project, metadata string) string { return "csharp-" + stableDigest(project, metadata) }

func Validate(raw []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("malformed architecture document: %w", err)
	}
	if d.Schema != SchemaURL {
		return d, errors.New("architecture document must use CALM 1.2 schema")
	}
	if len(d.Nodes) == 0 {
		return d, errors.New("architecture document requires at least one node")
	}
	ids := map[string]bool{}
	rels := map[string]bool{}
	for _, n := range d.Nodes {
		if n.UniqueID == "" || n.Name == "" || n.NodeType == "" || ids[n.UniqueID] {
			return d, errors.New("architecture document contains invalid or duplicate node IDs")
		}
		ids[n.UniqueID] = true
	}
	for _, r := range d.Relationships {
		rt := r.RelationshipType
		if r.UniqueID == "" || rels[r.UniqueID] {
			return d, errors.New("architecture document contains invalid or duplicate relationship IDs")
		}
		rels[r.UniqueID] = true
		if rt.Connects.Source.Node == "" && rt.ComposedOf == nil {
			return d, errors.New("architecture relationship requires a supported relationship type")
		}
		if rt.ComposedOf == nil && (rt.Connects.Source.Node == "" || rt.Connects.Destination.Node == "" || !ids[rt.Connects.Source.Node] || !ids[rt.Connects.Destination.Node]) {
			return d, errors.New("architecture relationship references an unknown node")
		}
		if rt.ComposedOf != nil && (!ids[rt.ComposedOf.Container.Node] || !ids[rt.ComposedOf.Component.Node]) {
			return d, errors.New("architecture composed-of references an unknown node")
		}
	}
	return d, nil
}
func ValidateGraph(g Graph) error {
	if g.SchemaVersion != "" && g.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported graph schema version %q", g.SchemaVersion)
	}
	seen := map[string]Node{}
	for _, n := range g.Nodes {
		if n.ID == "" || n.Name == "" || n.Kind == "" {
			return errors.New("graph node has an empty required field")
		}
		if old, ok := seen[n.ID]; ok && old.Name != n.Name {
			return fmt.Errorf("conflicting node identity %q", n.ID)
		}
		seen[n.ID] = n
	}
	for _, e := range g.Edges {
		if !supportedKinds[e.Kind] {
			return fmt.Errorf("unsupported dependency kind %q", e.Kind)
		}
		if _, ok := seen[e.Source]; !ok {
			return fmt.Errorf("edge source %q is not declared", e.Source)
		}
		if _, ok := seen[e.Destination]; !ok {
			return fmt.Errorf("edge destination %q is not declared", e.Destination)
		}
	}
	if len(g.Nodes) == 0 {
		return errors.New("empty architecture graph is not a supported baseline")
	}
	return nil
}
