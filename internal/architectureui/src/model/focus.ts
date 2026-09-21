import type { ProjectDocument } from './project'
import { edgeInfo } from './project'

export type FocusDirection = 'incoming' | 'outgoing' | 'both'

export function focusTypes(document: ProjectDocument, roots: string[], direction: FocusDirection, hops = 1) {
  const adjacency = new Map<string, { incoming: string[]; outgoing: string[] }>()
  document.nodes.forEach(node => adjacency.set(node['unique-id'], { incoming: [], outgoing: [] }))
  document.relationships.forEach(relationship => {
    const edge = edgeInfo(relationship)
    adjacency.get(edge.source.node)?.outgoing.push(edge.destination.node)
    adjacency.get(edge.destination.node)?.incoming.push(edge.source.node)
  })
  const result = new Set(roots), frontier = [...roots]
  for (let depth = 0; depth < Math.max(0, Math.min(hops, 2)); depth++) {
    const next: string[] = []
    frontier.forEach(id => {
      const links = adjacency.get(id)
      const neighbors = direction === 'incoming' ? links?.incoming ?? [] : direction === 'outgoing' ? links?.outgoing ?? [] : [...(links?.incoming ?? []), ...(links?.outgoing ?? [])]
      neighbors.forEach(neighbor => { if (!result.has(neighbor)) { result.add(neighbor); next.push(neighbor) } })
    })
    frontier.splice(0, frontier.length, ...next)
  }
  return result
}
