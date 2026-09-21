import { describe, expect, it } from 'vitest'
import { focusTypes } from './focus'
import { edgeInfo } from './project'

const node = (id: string) => ({ 'unique-id': id, name: id, 'node-type': 'service' })
const edge = (source: string, destination: string) => ({ 'unique-id': `${source}-${destination}`, 'relationship-type': { connects: { source: { node: source }, destination: { node: destination } } } })

describe('focusTypes', () => {
  const document = { nodes: [node('a'), node('b'), node('c'), node('d')], relationships: [edge('a', 'b'), edge('c', 'b'), edge('b', 'd')] }
  it('traverses predecessors and successors independently', () => {
    expect(focusTypes(document, ['b'], 'incoming')).toEqual(new Set(['b', 'a', 'c']))
    expect(focusTypes(document, ['b'], 'outgoing')).toEqual(new Set(['b', 'd']))
  })
  it('supports both directions and the bounded second hop', () => {
    expect(focusTypes(document, ['b'], 'both', 2)).toEqual(new Set(['b', 'a', 'c', 'd']))
  })
})
