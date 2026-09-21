import { describe, expect, it } from 'vitest'
import { readBookmark } from './bookmark'

describe('bookmark', () => {
  it('rejects malformed or oversized state', () => {
    expect(readBookmark('#v1=not-json').expanded).toEqual([])
    expect(readBookmark(`#v1=${btoa(JSON.stringify({ v: 2, expanded: ['x'] }))}`).expanded).toEqual([])
  })
  it('normalizes focus hops and IDs', () => {
    const hash = `#v1=${btoa(JSON.stringify({ v: 1, expanded: ['project:A'], selected: 'a', focus: { direction: 'incoming', hops: 99 } }))}`
    expect(readBookmark(hash)).toEqual({ v: 1, expanded: ['project:A'], selected: 'a', focus: { direction: 'incoming', hops: 2 } })
  })
})
