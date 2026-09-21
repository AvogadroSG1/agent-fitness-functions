export type ProjectElement = { 'unique-id': string; name: string; 'node-type': string; metadata?: Record<string, any> }
export type ProjectRelationship = { 'unique-id': string; description?: string; 'relationship-type': { connects: { source: { node: string }; destination: { node: string }; protocol?: Record<string, any> } } }
export type ProjectDocument = { nodes: ProjectElement[]; relationships: ProjectRelationship[] }

export const extension = (element?: unknown) => (element as ProjectElement | undefined)?.metadata?.['agent-fitness-functions'] ?? {}
export const projectName = (element: unknown) => { const candidate = element as ProjectElement | undefined; return extension(candidate).project || candidate?.metadata?.project || 'Unassigned' }
export const projectId = (name: string) => `project:${name}`
export const projectKey = (element: unknown) => { const candidate = element as ProjectElement | undefined; return extension(candidate).project_path || candidate?.metadata?.project_path || projectName(candidate) }
export const edgeInfo = (relationship: ProjectRelationship) => relationship['relationship-type']?.connects ?? { source: { node: '' }, destination: { node: '' } }

export const isProject = (element: unknown) => { const candidate = element as ProjectElement | undefined; return extension(candidate).project_overview === true || candidate?.['node-type'] === 'project' }

/** Project-first projection. It preserves the source document and only changes visible representatives. */
export function projectGraph(document: ProjectDocument, expanded: Set<string>, selected?: string) {
  const sourceProjectNodes = document.nodes.filter(isProject)
  const typeNodes = document.nodes.filter(element => !isProject(element))
  const expandedFor = (element: ProjectElement) => expanded.has(projectId(projectKey(element))) || expanded.has(projectId(projectName(element)))
  const projects = [...new Map(document.nodes.map(element => [projectKey(element), { key: projectKey(element), name: projectName(element) }])).values()].sort((a, b) => a.key.localeCompare(b.key))
  const representativeByProject = new Map(sourceProjectNodes.map(element => [projectKey(element), element['unique-id']]))
  const representative = (key: string) => representativeByProject.get(key) ?? projectId(key)
  const internalCounts = new Map<string, number>()
  document.relationships.forEach(relationship => {
    const edge = edgeInfo(relationship)
    const source = document.nodes.find(element => element['unique-id'] === edge.source.node)
    const destination = document.nodes.find(element => element['unique-id'] === edge.destination.node)
    if (source && destination && projectKey(source) === projectKey(destination)) internalCounts.set(projectKey(source), (internalCounts.get(projectKey(source)) ?? 0) + 1)
  })
  const projectNodes = projects.map(({ key, name }) => {
    const existing = sourceProjectNodes.find(element => projectKey(element) === key)
    if (existing) return { ...existing, metadata: { ...(existing.metadata ?? {}), 'agent-fitness-functions': { ...extension(existing), project: name, project_path: key, project_overview: true, 'internal-dependency-count': internalCounts.get(key) ?? 0 } } }
    return { 'unique-id': projectId(key), name, 'node-type': 'project', metadata: { 'agent-fitness-functions': { project: name, project_path: key, project_overview: true, 'internal-dependency-count': internalCounts.get(key) ?? 0 } } } as ProjectElement
  })
  const types = typeNodes.filter(element => expandedFor(element) || element['unique-id'] === selected)
  const elements = [...projectNodes, ...types]
  const endpoint = (id: string) => {
    const source = document.nodes.find(element => element['unique-id'] === id)
    if (!source) return projectId('Unknown')
    if (isProject(source)) return representative(projectKey(source))
    return expandedFor(source) ? id : representative(projectKey(source))
  }
  const aggregated = new Map<string, ProjectRelationship>()
  document.relationships.forEach(relationship => {
    const edge = edgeInfo(relationship)
    const source = endpoint(edge.source.node), destination = endpoint(edge.destination.node)
    if (source === destination) return
    const kind = edge.protocol?.['dependency-kind'] ?? 'dependency'
    const key = `${source}:${destination}:${kind}`
    const existing = aggregated.get(key)
    if (!existing) {
      aggregated.set(key, { ...relationship, 'unique-id': `aggregate:${key}`, description: `Aggregated ${kind} dependency`, 'relationship-type': { connects: { ...edge, source: { node: source }, destination: { node: destination }, protocol: { ...edge.protocol, 'dependency-count': 1, 'relationship-count': 1, 'relationship-ids': [relationship['unique-id']] } } } })
      return
    }
    const current = edgeInfo(existing)
    current.protocol = { ...current.protocol, 'dependency-count': Number(current.protocol?.['dependency-count'] ?? 1) + 1, 'relationship-count': Number(current.protocol?.['relationship-count'] ?? 1) + 1, 'relationship-ids': [...(current.protocol?.['relationship-ids'] ?? []), relationship['unique-id']], evidence: [...((current.protocol?.evidence as unknown[]) ?? []), ...(((edge.protocol?.evidence as unknown[]) ?? []))] }
  })
  return { elements, relationships: [...aggregated.values()] }
}
