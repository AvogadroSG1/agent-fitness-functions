import React, { useEffect, useMemo, useRef, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { ReactFlow, Background, Controls, Handle, MiniMap, Position, applyNodeChanges, type Edge, type Node, type ReactFlowInstance } from '@xyflow/react'
import ELK from 'elkjs/lib/elk.bundled.js'
import '@xyflow/react/dist/style.css'
import './styles.css'
import './uml.css'
import { edgeInfo, extension, isProject, projectGraph, projectId, projectKey, projectName } from './model/project'

type Member = { kind: string }
type Location = { file: string; line: number; column: number }
type Element = { 'unique-id': string; name: string; 'node-type': string; metadata?: Record<string, any> }
type Relationship = { 'unique-id': string; 'relationship-type': { connects: { source: { node: string }; destination: { node: string }; protocol?: Record<string, any> } } }
type Document = { nodes: Element[]; relationships: Relationship[]; metadata?: Record<string, any> }
type Summary = { repo: string; status: string; node_count: number; relationship_count: number }

const elk = new ELK()
const fetchJSON = async (url: string) => { const response = await fetch(url); if (!response.ok) throw new Error(`${response.status} ${response.statusText}`); return response.json() }
const fetchDocument = async (url: string) => { const response = await fetch(url); if (!response.ok) throw new Error(`${response.status} ${response.statusText}`); return JSON.parse(await response.text()) as Document }

function TypeNode({ data }: { data: { element: Element; selected: boolean } }) {
  const meta = extension(data.element), project = isProject(data.element), members = (meta.members ?? []) as Member[]
  const attributes = members.filter(member => member.kind === 'property' || member.kind === 'field')
  const operations = members.filter(member => member.kind === 'constructor' || member.kind === 'method')
  return <div className={`type-node ${project ? 'project-node' : 'uml-node'} ${data.selected ? 'selected' : ''}`}><Handle type="target" position={Position.Left} /><div className="uml-header"><strong>{data.element.name}</strong><small>{project ? `project${meta['internal-dependency-count'] ? ` · ${meta['internal-dependency-count']} internal dependencies` : ''}` : `${meta.kind || data.element['node-type']}${meta.namespace ? ` · ${meta.namespace}` : ''}`}</small></div>{!project && <><div className="uml-rule" /><div className="uml-section-title">{attributes.length ? `Attributes · ${attributes.length}` : 'Attributes · unknown'}</div><div className="uml-rule" /><div className="uml-section-title">{operations.length ? `Operations · ${operations.length}` : 'Operations · unknown'}</div></>}<Handle type="source" position={Position.Right} /></div>
}

const nodeTypes = { typeCard: TypeNode }

async function layout(elements: Element[], relationships: Relationship[], selected?: string) {
  const graph = await elk.layout({ id: 'architecture', layoutOptions: { 'elk.algorithm': 'layered', 'elk.direction': 'RIGHT', 'elk.layered.spacing.nodeNodeBetweenLayers': '80', 'elk.spacing.nodeNode': '40' }, children: elements.map(element => ({ id: element['unique-id'], width: isProject(element) ? 240 : 300, height: isProject(element) ? 76 : 104 })), edges: relationships.map(relationship => { const edge = edgeInfo(relationship); return { id: relationship['unique-id'], sources: [edge.source.node], targets: [edge.destination.node] } }) })
  const positions = new Map((graph.children ?? []).map(child => [child.id, { x: child.x ?? 0, y: child.y ?? 0 }]))
  return elements.map(element => ({ id: element['unique-id'], type: 'typeCard', position: positions.get(element['unique-id']) ?? { x: 0, y: 0 }, data: { element, selected: element['unique-id'] === selected }, width: isProject(element) ? 240 : 300, height: isProject(element) ? 76 : 104 })) as Node[]
}

function Inspector({ element, relationships }: { element?: Element; relationships: Relationship[] }) {
  if (!element) return <aside className="inspector empty"><h2>Type summary</h2><p>Select a type in the graph or search results.</p></aside>
  const meta = extension(element), locations = (meta.declaration_locations ?? []) as Location[]
  return <aside className="inspector"><p className="eyebrow">TYPE SUMMARY</p><h2>{element.name}</h2><p className="qualified">{meta.qualified_name || element['unique-id']}</p><div className="chips"><span>{meta.kind || element['node-type']}</span><span>{meta.accessibility || 'unknown'}</span>{projectName(element) && <span>{projectName(element)}</span>}</div><section><h3>Members <small>{meta.member_extraction_available === false ? 'unavailable' : (meta.members ?? []).length}</small></h3><p className="muted">{meta.member_extraction_available === false ? 'Member information unavailable.' : `${(meta.members ?? []).length} declared members`}</p></section><section><h3>Declaration locations</h3>{locations.map(location => <p className="location" key={`${location.file}:${location.line}`}>{location.file}:{location.line}:{location.column}</p>)}</section><section><h3>Relationships <small>{relationships.length}</small></h3>{relationships.map(relationship => { const edge = edgeInfo(relationship); return <div className="relationship" key={relationship['unique-id']}><b>{edge.protocol?.['dependency-kind'] ?? 'dependency'}</b><span>{edge.source.node === element['unique-id'] ? `→ ${edge.destination.node}` : `← ${edge.source.node}`}</span></div> })}</section></aside>
}

function App() {
  const [catalog, setCatalog] = useState<Summary[]>([]), [repo, setRepo] = useState(''), [document, setDocument] = useState<Document>(), [query, setQuery] = useState(''), [repoQuery, setRepoQuery] = useState(''), [selected, setSelected] = useState<string>(), [expanded, setExpanded] = useState<Set<string>>(new Set()), [nodes, setNodes] = useState<Node[]>([]), [loading, setLoading] = useState(''), [error, setError] = useState('')
  const flowRef = useRef<ReactFlowInstance>(), positions = useRef(new Map<string, { x: number; y: number }>()), layoutJob = useRef(0)
  const available = useMemo(() => catalog.filter(item => item.status === 'available' && item.repo.toLowerCase().includes(repoQuery.toLowerCase())), [catalog, repoQuery])
  const filtered = useMemo(() => document?.nodes.filter(element => `${element.name} ${element['unique-id']} ${extension(element).qualified_name ?? ''} ${projectName(element)}`.toLowerCase().includes(query.toLowerCase())) ?? [], [document, query])
  const selectedElement = document?.nodes.find(element => element['unique-id'] === selected)
  const selectedRelationships = document?.relationships.filter(relationship => { const edge = edgeInfo(relationship); return edge.source.node === selected || edge.destination.node === selected }) ?? []
  const model = document ? projectGraph(document, expanded, selected) : { elements: [], relationships: [] }
  const refreshLayout = async (doc: Document, open: Set<string>, selection?: string) => { const job = ++layoutJob.current; const graph = projectGraph(doc, open, selection); const laidOut = await layout(graph.elements, graph.relationships, selection); if (job !== layoutJob.current) return; const stable = laidOut.map(node => { const previous = positions.current.get(node.id); return previous ? { ...node, position: previous } : node }); stable.forEach(node => positions.current.set(node.id, node.position)); setNodes(stable) }
  const loadRepo = async (name: string) => { setLoading(`Loading ${name}…`); setError(''); try { const doc = await fetchDocument(`/architecture?repo=${encodeURIComponent(name)}`); setRepo(name); setDocument(doc); setSelected(undefined); setExpanded(new Set()); positions.current.clear(); await refreshLayout(doc, new Set()); setLoading(`${doc.nodes.length} types · ${doc.relationships.length} relationships`) } catch (reason: any) { setError(reason.message); setLoading('') } }
  useEffect(() => { fetchJSON('/architectures').then(response => { const entries = response.architectures as Summary[]; setCatalog(entries); const requested = new URLSearchParams(location.search).get('repo'); const chosen = entries.find(entry => entry.status === 'available' && entry.repo === requested) ?? entries.find(entry => entry.status === 'available'); if (chosen) loadRepo(chosen.repo); else setLoading('No accepted architectures') }).catch(reason => setError(reason.message)) }, [])
  useEffect(() => { if (document) refreshLayout(document, expanded, selected) }, [selected, expanded])
  const toggleProject = (id: string) => { const next = new Set(expanded); next.has(id) ? next.delete(id) : next.add(id); setExpanded(next) }
  const reveal = (element: Element) => { setSelected(element['unique-id']); setExpanded(new Set([...expanded, projectId(projectKey(element))])) }
  const flowEdges = model.relationships.map(relationship => { const edge = edgeInfo(relationship); return { id: relationship['unique-id'], source: edge.source.node, target: edge.destination.node, label: edge.protocol?.['dependency-kind'] ?? 'dependency', animated: edge.source.node === selected || edge.destination.node === selected } }) as Edge[]
  return <div className="shell"><header><div><p className="eyebrow">LOCAL ARCHITECTURE WORKSPACE</p><h1>Architecture browser</h1></div><div className="toolbar"><span>{loading || `${nodes.length} visible types`}</span><button onClick={() => flowRef.current?.fitView({ padding: 0.2, duration: 250 })}>Fit graph</button><button onClick={() => repo && loadRepo(repo)}>Refresh</button></div></header><div className="workspace"><aside className="sidebar"><label>Repositories<input value={repoQuery} onChange={event => setRepoQuery(event.target.value)} placeholder="Filter repositories" /></label><div className="repo-list">{available.map(item => <button className={item.repo === repo ? 'repo active' : 'repo'} key={item.repo} onClick={() => loadRepo(item.repo)}><b>{item.repo}</b><small>{item.node_count} nodes · {item.relationship_count} links</small></button>)}</div>{document && <><label>Types<input value={query} onChange={event => setQuery(event.target.value)} placeholder="Search types or projects" /></label><div className="type-list">{filtered.slice(0, 120).map(element => <button className={selected === element['unique-id'] ? 'type active' : 'type'} key={element['unique-id']} onClick={() => reveal(element)}><span>{element.name}</span><small>{projectName(element)}</small></button>)}</div><p className="muted">{filtered.length} shown / {document.nodes.length} available</p></>}</aside><main className="canvas"><div className="canvas-note">Project-first graph · select a type or expand a project</div><ReactFlow nodes={nodes} edges={flowEdges} nodeTypes={nodeTypes} fitView onInit={instance => { flowRef.current = instance }} onNodesChange={changes => { setNodes(current => { const next = applyNodeChanges(changes, current); next.forEach(node => positions.current.set(node.id, node.position)); return next }) }} onNodeDragStart={() => { layoutJob.current++ }} onNodeDragStop={(_, node) => positions.current.set(node.id, node.position)} onNodeClick={(_, node) => isProject(node.data?.element) ? toggleProject(projectId(projectName(node.data.element))) : setSelected(node.id)}><Background gap={24} color="#d9e2ec" /><MiniMap /><Controls /></ReactFlow></main><Inspector element={selectedElement} relationships={selectedRelationships} /></div>{error && <div className="error">{error}</div>}</div>
}

createRoot(document.getElementById('root')!).render(<App />)
