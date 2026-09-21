import { describe, expect, it } from 'vitest'
import { projectGraph, projectId } from './project'

const node = (id: string, project: string) => ({ 'unique-id': id, name: id, 'node-type': 'service', metadata: { project, 'agent-fitness-functions': { project, kind: 'class' } } })
const relationship = (id: string, source: string, destination: string, kind = 'member') => ({ 'unique-id': id, 'relationship-type': { connects: { source: { node: source }, destination: { node: destination }, protocol: { 'dependency-kind': kind } } } })

describe('projectGraph', () => {
  it('starts collapsed and aggregates parallel evidence by project pair and kind', () => {
    const document = { nodes: [node('a', 'A'), node('b', 'B')], relationships: [relationship('1', 'a', 'b'), relationship('2', 'a', 'b'), relationship('3', 'a', 'b', 'construction')] }
    const graph = projectGraph(document, new Set())
    expect(graph.elements.map(element => element['unique-id'])).toEqual([projectId('A'), projectId('B')])
    expect(graph.relationships).toHaveLength(2)
    expect(graph.relationships.find(edge => edge['unique-id'].endsWith(':member'))?.['relationship-type'].connects.protocol?.['dependency-count']).toBe(2)
  })

  it('expands one project while preserving the other as a representative', () => {
    const document = { nodes: [node('a', 'A'), node('b', 'B')], relationships: [relationship('1', 'a', 'b')] }
    const graph = projectGraph(document, new Set([projectId('A')]))
    expect(graph.elements.map(element => element['unique-id'])).toEqual(['project:A', 'project:B', 'a'])
    expect(graph.relationships[0]['relationship-type'].connects.source.node).toBe('a')
    expect(graph.relationships[0]['relationship-type'].connects.destination.node).toBe('project:B')
  })

  it('counts internal dependencies without drawing a collapsed project loop', () => {
    const document = { nodes: [node('a', 'A'), node('b', 'A')], relationships: [relationship('1', 'a', 'b')] }
    const graph = projectGraph(document, new Set())
    expect(graph.relationships).toHaveLength(0)
    expect(graph.elements[0].metadata?.['agent-fitness-functions']?.['internal-dependency-count']).toBe(1)
  })

  it('keeps the underlying relationship IDs reversible after aggregation', () => {
    const document = { nodes: [node('a', 'A'), node('b', 'B')], relationships: [relationship('1', 'a', 'b'), relationship('2', 'a', 'b')] }
    const edge = projectGraph(document, new Set()).relationships[0]
    expect(edge['relationship-type'].connects.protocol?.['relationship-ids']).toEqual(['1', '2'])
  })

  it('keeps explicit project-reference nodes as project representatives', () => {
    const app = { 'unique-id': 'project-app', name: 'App', 'node-type': 'project', metadata: { 'agent-fitness-functions': { project: 'App', project_overview: true } } }
    const lib = { 'unique-id': 'project-lib', name: 'Lib', 'node-type': 'project', metadata: { 'agent-fitness-functions': { project: 'Lib', project_overview: true } } }
    const graph = projectGraph({ nodes: [app, lib], relationships: [relationship('r', 'project-app', 'project-lib', 'project-reference')] }, new Set())
    expect(graph.elements.map(element => element['unique-id'])).toEqual(['project-app', 'project-lib'])
    expect(graph.relationships[0]['relationship-type'].connects.source.node).toBe('project-app')
    expect(graph.relationships[0]['relationship-type'].connects.destination.node).toBe('project-lib')
  })
})
