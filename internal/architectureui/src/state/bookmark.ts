import type { FocusDirection } from '../model/focus'

export type Bookmark = { v: 1; expanded: string[]; selected?: string; focus?: { direction: FocusDirection; hops: number } }
const maxLength = 1800

export function readBookmark(hash = location.hash): Bookmark {
  if (!hash.startsWith('#v1=')) return { v: 1, expanded: [] }
  try {
    const decoded = JSON.parse(atob(hash.slice(4))) as Partial<Bookmark>
    if (decoded.v !== 1 || !Array.isArray(decoded.expanded) || JSON.stringify(decoded).length > maxLength) return { v: 1, expanded: [] }
    const focus = decoded.focus && ['incoming', 'outgoing', 'both'].includes(decoded.focus.direction) ? { direction: decoded.focus.direction as FocusDirection, hops: Math.min(2, Math.max(1, Number(decoded.focus.hops) || 1)) } : undefined
    return { v: 1, expanded: decoded.expanded.filter(value => typeof value === 'string').slice(0, 100), selected: typeof decoded.selected === 'string' ? decoded.selected : undefined, focus }
  } catch { return { v: 1, expanded: [] } }
}

export function writeBookmark(bookmark: Bookmark) {
  const encoded = btoa(JSON.stringify(bookmark))
  if (encoded.length <= maxLength) history.replaceState(null, '', `${location.pathname}${location.search}#v1=${encoded}`)
}
