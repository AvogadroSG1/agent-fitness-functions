// Package architecture contains the language-neutral observed architecture graph.
package architecture

import (
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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Project string `json:"project,omitempty"`
}
type Edge struct {
	Source      string     `json:"source"`
	Destination string     `json:"destination"`
	Kind        string     `json:"kind"`
	Locations   []Location `json:"locations,omitempty"`
}
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
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
	UniqueID         string   `json:"unique-id"`
	Description      string   `json:"description,omitempty"`
	RelationshipType Connects `json:"relationship-type"`
}
type Connects struct {
	Connects ConnectsBody `json:"connects"`
}
type ConnectsBody struct {
	Source      NodeReference  `json:"source"`
	Destination NodeReference  `json:"destination"`
	Protocol    map[string]any `json:"protocol,omitempty"`
}
type NodeReference struct {
	Node string `json:"node"`
}

const schema = "https://calm.finos.org/release/1.2/meta/calm.json"

func Build(g Graph) Document {
	nodes := append([]Node(nil), g.Nodes...)
	edges := append([]Edge(nil), g.Edges...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Destination != edges[j].Destination {
			return edges[i].Destination < edges[j].Destination
		}
		return edges[i].Kind < edges[j].Kind
	})
	d := Document{Schema: schema}
	nodeIDs := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodeIDs[n.ID] = true
		d.Nodes = append(d.Nodes, CALMNode{UniqueID: n.ID, NodeType: nodeType(n.Kind), Name: n.Name, Description: "Observed C# " + n.Kind + " " + n.Name, Metadata: map[string]any{"kind": n.Kind, "project": n.Project, "fitness": map[string]any{"cyclomatic-complexity": 1, "interface-width": 1, "implementation-depth": 1, "logic-density": 1, "dependency-discipline": 1}}})
	}
	for i, e := range edges {
		if !nodeIDs[e.Source] || !nodeIDs[e.Destination] {
			continue
		}
		locs := append([]Location(nil), e.Locations...)
		sort.Slice(locs, func(i, j int) bool {
			if locs[i].File != locs[j].File {
				return locs[i].File < locs[j].File
			}
			if locs[i].Line != locs[j].Line {
				return locs[i].Line < locs[j].Line
			}
			return locs[i].Column < locs[j].Column
		})
		d.Relationships = append(d.Relationships, Relationship{UniqueID: fmt.Sprintf("relationship-%04d", i+1), Description: "Observed " + e.Kind + " dependency", RelationshipType: Connects{Connects: ConnectsBody{Source: NodeReference{Node: e.Source}, Destination: NodeReference{Node: e.Destination}, Protocol: map[string]any{"dependency-kind": e.Kind, "locations": locs}}}})
	}
	return d
}
func nodeType(k string) string {
	if k == "interface" {
		return "service"
	}
	return "service"
}
func Validate(raw []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	if d.Schema == "" || len(d.Nodes) == 0 {
		return d, errors.New("architecture document requires $schema and at least one node")
	}
	ids := map[string]bool{}
	for _, n := range d.Nodes {
		if n.UniqueID == "" || ids[n.UniqueID] {
			return d, errors.New("architecture document contains invalid or duplicate node IDs")
		}
		ids[n.UniqueID] = true
	}
	for _, r := range d.Relationships {
		if r.RelationshipType.Connects.Source.Node == "" || r.RelationshipType.Connects.Destination.Node == "" || !ids[r.RelationshipType.Connects.Source.Node] || !ids[r.RelationshipType.Connects.Destination.Node] {
			return d, errors.New("architecture relationship references an unknown node")
		}
	}
	return d, nil
}
func StableID(project, metadata string) string {
	s := strings.TrimPrefix(metadata, "global::")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "+", "-")
	return strings.Trim(strings.ToLower(project+"-"+s), "-")
}
