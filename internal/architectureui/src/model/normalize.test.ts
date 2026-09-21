import { describe, expect, it } from 'vitest'
import { normalize } from './normalize'

describe('normalize', () => {
  it('keeps legacy member metadata unknown instead of inventing an empty list', () => {
    const graph = normalize({ nodes: [{ 'unique-id': 'a', name: 'A', 'node-type': 'service', metadata: { project: 'P' } }], relationships: [] })
    expect(graph.types[0].memberAvailability).toBe('unknown')
    expect(graph.types[0].members).toEqual([])
  })
  it('normalizes enriched type and evidence metadata', () => {
    const graph = normalize({ metadata: { 'agent-fitness-functions': { completeness: 'partial', extraction_mode: 'semantic', relationships_omitted: 2 } }, nodes: [{ 'unique-id': 'a', name: 'A', metadata: { 'agent-fitness-functions': { project: 'P', project_path: 'src/P/P.csproj', member_extraction_available: true, members: [{ id: 'm', kind: 'method', name: 'Run', display_signature: 'Run()', accessibility: 'public' }] } } }], relationships: [{ 'unique-id': 'r', 'relationship-type': { connects: { source: { node: 'a' }, destination: { node: 'a' }, protocol: { 'dependency-kind': 'member', evidence: [{ location: { file: 'P.cs', line: 3, column: 4 } }] } } } }] })
    expect(graph.analysis.extractionMode).toBe('semantic')
    expect(graph.analysis.omissions).toBe(2)
    expect(graph.types[0].memberAvailability).toBe('available')
    expect(graph.dependencies[0].evidence[0].location.file).toBe('P.cs')
  })
})
