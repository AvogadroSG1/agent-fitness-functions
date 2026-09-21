import type { DependencyRecord, Location, NormalizedGraph, TypeRecord } from './contracts'

const extension = (node: any) => node?.metadata?.['agent-fitness-functions'] ?? {}
const locations = (value: unknown): Location[] => Array.isArray(value) ? value.filter(item => item && typeof item.file === 'string').map(item => ({ file: String(item.file), line: Number(item.line) || 0, column: Number(item.column) || 0 })) : []

/** Adapts both enriched documents and older project-reference documents without inventing members. */
export function normalize(raw: any): NormalizedGraph {
  const nodes = Array.isArray(raw?.nodes) ? raw.nodes : []
  const types: TypeRecord[] = nodes.map((node: any): TypeRecord => {
    const meta = extension(node)
    const available = meta.member_extraction_available === true ? 'available' : meta.member_extraction_available === false ? 'unavailable' : 'unknown'
    return { id: String(node['unique-id'] ?? ''), name: String(node.name ?? ''), qualifiedName: String(meta.qualified_name ?? ''), namespace: String(meta.namespace ?? ''), project: String(meta.project ?? node.metadata?.project ?? 'Unassigned'), projectPath: typeof meta.project_path === 'string' ? meta.project_path : undefined, kind: String(meta.kind ?? node['node-type'] ?? 'unknown'), accessibility: meta.accessibility, modifiers: [meta.is_abstract && 'abstract', meta.is_static && 'static', meta.is_sealed && 'sealed'].filter(Boolean) as string[], memberAvailability: available, members: (available === 'available' && Array.isArray(meta.members) ? meta.members : []) as TypeRecord['members'], locations: locations(meta.declaration_locations), raw: node }
  })
  const relationships = Array.isArray(raw?.relationships) ? raw.relationships : []
  const dependencies = relationships.map((relationship: any): DependencyRecord => {
    const connects = relationship['relationship-type']?.connects ?? {}
    const protocol = connects.protocol ?? {}
    return { id: String(relationship['unique-id'] ?? ''), source: String(connects.source?.node ?? ''), destination: String(connects.destination?.node ?? ''), kind: String(protocol['dependency-kind'] ?? 'dependency'), locations: locations(protocol.locations), evidence: Array.isArray(protocol.evidence) ? protocol.evidence : [], raw: relationship }
  })
  const projectMap = new Map<string, { id: string; name: string; path?: string; typeIds: string[] }>()
  types.forEach(type => { const existing = projectMap.get(type.project) ?? { id: `project:${type.project}`, name: type.project, path: type.projectPath, typeIds: [] }; existing.typeIds.push(type.id); projectMap.set(type.project, existing) })
  const metadata = raw?.metadata?.['agent-fitness-functions'] ?? {}
  return { types, dependencies, projects: [...projectMap.values()].sort((a, b) => a.name.localeCompare(b.name)), analysis: { extensionVersion: metadata.extension_version, identityVersion: metadata.identity_version, extractionMode: String(metadata.extraction_mode ?? 'project-reference'), analyzerVersion: metadata.analyzer_version, completeness: metadata.completeness === 'partial' || metadata.completeness === 'complete-within-scope' ? metadata.completeness : 'unknown', omissions: Number(metadata.relationships_omitted ?? 0), diagnostics: Array.isArray(metadata.diagnostics) ? metadata.diagnostics : [], raw: metadata }, raw }
}
