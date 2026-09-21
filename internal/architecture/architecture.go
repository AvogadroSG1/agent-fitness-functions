// Package architecture contains the language-neutral observed architecture graph.
package architecture

import (
	"crypto/sha256"
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
	ID                        string         `json:"id"`
	Name                      string         `json:"name"`
	Kind                      string         `json:"kind"`
	Project                   string         `json:"project,omitempty"`
	QualifiedName             string         `json:"qualified_name,omitempty"`
	Namespace                 string         `json:"namespace,omitempty"`
	ProjectPath               string         `json:"project_path,omitempty"`
	Accessibility             string         `json:"accessibility,omitempty"`
	IsAbstract                bool           `json:"is_abstract,omitempty"`
	IsStatic                  bool           `json:"is_static,omitempty"`
	IsSealed                  bool           `json:"is_sealed,omitempty"`
	MemberExtractionAvailable bool           `json:"member_extraction_available,omitempty"`
	DeclarationLocations      []Location     `json:"declaration_locations,omitempty"`
	Members                   []Member       `json:"members,omitempty"`
	Metadata                  map[string]any `json:"metadata,omitempty"`
}
type Member struct {
	ID                       string      `json:"id"`
	Kind                     string      `json:"kind"`
	Name                     string      `json:"name"`
	DisplaySignature         string      `json:"display_signature"`
	Accessibility            string      `json:"accessibility"`
	Type                     string      `json:"type,omitempty"`
	IsStatic                 bool        `json:"is_static,omitempty"`
	IsAbstract               bool        `json:"is_abstract,omitempty"`
	IsReadOnly               bool        `json:"is_read_only,omitempty"`
	PropertyGetAccessibility string      `json:"property_get_accessibility,omitempty"`
	PropertySetAccessibility string      `json:"property_set_accessibility,omitempty"`
	Parameters               []Parameter `json:"parameters,omitempty"`
	Locations                []Location  `json:"locations,omitempty"`
}
type Parameter struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	RefKind    string `json:"ref_kind,omitempty"`
	IsOptional bool   `json:"is_optional,omitempty"`
}
type Edge struct {
	Source      string     `json:"source"`
	Destination string     `json:"destination"`
	Kind        string     `json:"kind"`
	Locations   []Location `json:"locations,omitempty"`
	Evidence    []Evidence `json:"evidence,omitempty"`
}
type Evidence struct {
	Location            Location `json:"location"`
	OriginatingMemberID string   `json:"originating_member_id,omitempty"`
	ReferencedSymbol    string   `json:"referenced_symbol,omitempty"`
}
type Graph struct {
	Nodes    []Node         `json:"nodes"`
	Edges    []Edge         `json:"edges"`
	Analysis map[string]any `json:"analysis,omitempty"`
}

type Document struct {
	Schema        string         `json:"$schema"`
	Nodes         []CALMNode     `json:"nodes"`
	Relationships []Relationship `json:"relationships"`
	Metadata      map[string]any `json:"metadata,omitempty"`
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
	nodes, edges := sortedGraph(g)
	d := Document{Schema: schema, Metadata: map[string]any{"agent-fitness-functions": analysisMetadata(g.Analysis)}}
	nodeIDs := appendCALMNodes(&d, nodes)
	appendRelationships(&d, edges, nodeIDs)
	accountRelationships(&d, edges)
	return d
}

func sortedGraph(g Graph) ([]Node, []Edge) {
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
	return nodes, edges
}

func analysisMetadata(source map[string]any) map[string]any {
	analysis := make(map[string]any, len(source)+4)
	for key, value := range source {
		analysis[key] = value
	}
	if _, ok := analysis["completeness"]; !ok {
		analysis["completeness"] = "unknown"
	}
	analysis["relationships_emitted"] = 0
	analysis["relationships_omitted"] = 0
	return analysis
}

func appendCALMNodes(d *Document, nodes []Node) map[string]bool {
	ids := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		ids[n.ID] = true
		d.Nodes = append(d.Nodes, calmNode(n))
	}
	return ids
}

func calmNode(n Node) CALMNode {
	extension := map[string]any{"qualified_name": n.QualifiedName, "namespace": n.Namespace, "project_path": n.ProjectPath, "accessibility": n.Accessibility, "kind": n.Kind, "is_abstract": n.IsAbstract, "is_static": n.IsStatic, "is_sealed": n.IsSealed, "member_extraction_available": n.MemberExtractionAvailable, "declaration_locations": n.DeclarationLocations}
	if n.MemberExtractionAvailable {
		extension["members"] = n.Members
	}
	metadata := map[string]any{"kind": n.Kind, "project": n.Project, "agent-fitness-functions": extension}
	for key, value := range n.Metadata {
		metadata[key] = value
	}
	return CALMNode{UniqueID: n.ID, NodeType: nodeType(n.Kind), Name: n.Name, Description: "Observed C# " + n.Kind + " " + n.Name, Metadata: metadata}
}

func appendRelationships(d *Document, edges []Edge, nodeIDs map[string]bool) {
	for _, edge := range edges {
		if relationship, ok := calmRelationship(edge, nodeIDs); ok {
			d.Relationships = append(d.Relationships, relationship)
		}
	}
}

func calmRelationship(edge Edge, nodeIDs map[string]bool) (Relationship, bool) {
	if !nodeIDs[edge.Source] || !nodeIDs[edge.Destination] {
		return Relationship{}, false
	}
	return Relationship{UniqueID: stableRelationshipID(edge), Description: "Observed " + edge.Kind + " dependency", RelationshipType: Connects{Connects: ConnectsBody{Source: NodeReference{Node: edge.Source}, Destination: NodeReference{Node: edge.Destination}, Protocol: map[string]any{"dependency-kind": edge.Kind, "locations": sortedLocations(edge.Locations), "evidence": sortedEvidence(edge.Evidence)}}}}, true
}

func sortedLocations(locations []Location) []Location {
	result := append([]Location(nil), locations...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].File != result[j].File {
			return result[i].File < result[j].File
		}
		if result[i].Line != result[j].Line {
			return result[i].Line < result[j].Line
		}
		return result[i].Column < result[j].Column
	})
	return result
}

func sortedEvidence(evidence []Evidence) []Evidence {
	result := append([]Evidence(nil), evidence...)
	sort.Slice(result, func(i, j int) bool {
		if result[i].Location.File != result[j].Location.File {
			return result[i].Location.File < result[j].Location.File
		}
		if result[i].Location.Line != result[j].Location.Line {
			return result[i].Location.Line < result[j].Location.Line
		}
		return result[i].Location.Column < result[j].Location.Column
	})
	return result
}

func accountRelationships(d *Document, edges []Edge) {
	extension, ok := d.Metadata["agent-fitness-functions"].(map[string]any)
	if !ok {
		return
	}
	emitted := len(d.Relationships)
	omitted := len(edges) - emitted
	excluded := excludedRelationshipCount(extension)
	extension["relationships_emitted"] = emitted
	extension["relationships_omitted"] = omitted + excluded
	if omitted > 0 || excluded > 0 {
		extension["omissions_by_reason"] = omissionReasons(extension, omitted)
		if completeness, ok := extension["completeness"].(string); ok && completeness == "complete-within-scope" {
			extension["completeness"] = "partial"
		}
	}
}

func excludedRelationshipCount(extension map[string]any) int {
	if value, ok := extension["relationships_excluded"].(float64); ok {
		return int(value)
	}
	if value, ok := extension["relationships_excluded"].(int); ok {
		return value
	}
	return 0
}

func omissionReasons(extension map[string]any, omitted int) map[string]int {
	reasons := map[string]int{}
	if existing, ok := extension["omissions_by_reason"].(map[string]any); ok {
		for key, value := range existing {
			if count, ok := value.(float64); ok {
				reasons[key] = int(count)
			}
		}
	}
	if existing, ok := extension["omissions_by_reason"].(map[string]int); ok {
		for key, count := range existing {
			reasons[key] = count
		}
	}
	reasons["missing_endpoint"] += omitted
	return reasons
}
func stableRelationshipID(e Edge) string {
	digest := sha256.Sum256([]byte(e.Source + "\x00" + e.Destination + "\x00" + e.Kind))
	return "relationship-" + fmt.Sprintf("%x", digest[:8])
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
	if err := validateDocumentShape(d); err != nil {
		return d, err
	}
	ids, err := validateNodes(d.Nodes)
	if err != nil {
		return d, err
	}
	if err := validateRelationships(d.Relationships, ids); err != nil {
		return d, err
	}
	return d, nil
}

func validateDocumentShape(d Document) error {
	if d.Schema == "" || len(d.Nodes) == 0 {
		return errors.New("architecture document requires $schema and at least one node")
	}
	return nil
}

func validateNodes(nodes []CALMNode) (map[string]bool, error) {
	ids := map[string]bool{}
	for _, n := range nodes {
		if n.UniqueID == "" || ids[n.UniqueID] {
			return nil, errors.New("architecture document contains invalid or duplicate node IDs")
		}
		ids[n.UniqueID] = true
		if err := validateNodeMembers(n); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func validateNodeMembers(n CALMNode) error {
	extension, ok := n.Metadata["agent-fitness-functions"].(map[string]any)
	if !ok {
		return nil
	}
	if !memberExtractionAvailable(extension) {
		return nil
	}
	list, err := memberList(extension)
	if err != nil {
		return err
	}
	return validateMemberIDs(list)
}

func memberExtractionAvailable(extension map[string]any) bool {
	available, present := extension["member_extraction_available"].(bool)
	return present && available
}

func memberList(extension map[string]any) ([]any, error) {
	members, present := extension["members"]
	if !present {
		return nil, errors.New("architecture node claims member extraction but omits members")
	}
	list, ok := members.([]any)
	if !ok {
		return nil, errors.New("architecture node members must be an array")
	}
	return list, nil
}

func validateMemberIDs(list []any) error {
	memberIDs := map[string]bool{}
	for _, rawMember := range list {
		member, ok := rawMember.(map[string]any)
		if !ok || member["id"] == nil || member["id"] == "" {
			return errors.New("architecture node contains a member without a stable ID")
		}
		memberID := fmt.Sprint(member["id"])
		if memberIDs[memberID] {
			return errors.New("architecture node contains duplicate member IDs")
		}
		memberIDs[memberID] = true
	}
	return nil
}

func validateRelationships(relationships []Relationship, ids map[string]bool) error {
	relationshipIDs := map[string]bool{}
	for _, r := range relationships {
		if r.UniqueID == "" || relationshipIDs[r.UniqueID] {
			return errors.New("architecture document contains invalid or duplicate relationship IDs")
		}
		relationshipIDs[r.UniqueID] = true
		if r.RelationshipType.Connects.Source.Node == "" || r.RelationshipType.Connects.Destination.Node == "" || !ids[r.RelationshipType.Connects.Source.Node] || !ids[r.RelationshipType.Connects.Destination.Node] {
			return errors.New("architecture relationship references an unknown node")
		}
	}
	return nil
}
func StableID(project, metadata string) string {
	s := strings.TrimPrefix(metadata, "global::")
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "+", "-")
	return strings.Trim(strings.ToLower(project+"-"+s), "-")
}
