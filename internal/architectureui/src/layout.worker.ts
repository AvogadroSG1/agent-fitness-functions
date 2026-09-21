import ELK from 'elkjs/lib/elk.bundled.js'
import { edgeInfo, extension, isProject } from './model/project'

const elk = new ELK()
self.onmessage = async (event: MessageEvent<{ request: number; elements: any[]; relationships: any[]; detail: string; direction: 'RIGHT' | 'DOWN' }>) => {
  const { request, elements, relationships, detail, direction } = event.data
  try {
    const graph = await elk.layout({ id: 'architecture', layoutOptions: { 'elk.algorithm': 'layered', 'elk.direction': direction, 'elk.layered.spacing.nodeNodeBetweenLayers': '80', 'elk.spacing.nodeNode': '40' }, children: elements.map(element => ({ id: element['unique-id'], width: isProject(element) ? 220 : 280, height: isProject(element) ? 64 : detail === 'detailed' ? Math.max(120, 78 + ((extension(element).members ?? []) as any[]).slice(0, 16).length * 22) : 90 })), edges: relationships.flatMap(relationship => { const edge = edgeInfo(relationship); return elements.some(element => element['unique-id'] === edge.source.node) && elements.some(element => element['unique-id'] === edge.destination.node) ? [{ id: relationship['unique-id'], sources: [edge.source.node], targets: [edge.destination.node] }] : [] }) })
    const positions = (graph.children ?? []).map(child => ({ id: child.id, x: child.x ?? 0, y: child.y ?? 0 }))
    self.postMessage({ request, positions })
  } catch (error) {
    self.postMessage({ request, error: error instanceof Error ? error.message : String(error) })
  }
}
