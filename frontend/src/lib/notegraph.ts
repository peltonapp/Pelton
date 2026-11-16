// notegraph.ts is the graph of the notes space: which notes link to which,
// optionally with tags and the Pelton objects notes link to as nodes of
// their own, and a small force-directed layout to draw it with. The layout
// is seeded, so the same graph always settles the same way, and it steps in
// slices so the caller can spread the work over animation frames and stop
// once it has settled. Repulsion uses a Barnes-Hut quadtree, which keeps a
// step near n log n for a vault of a few thousand notes.

import type { VaultNote } from './types'
import { parsePeltonUrl } from './peltonlinks'

/** What a node stands for. */
export type GraphNodeKind = 'note' | 'tag' | 'object' | 'missing'

/** One node of the graph. id is the note path, '#tag', the pelton:// address or the missing link target. */
export interface GraphNode {
  id: string
  kind: GraphNodeKind
  label: string
  degree: number
}

/** A graph: nodes and the undirected edges between them, by index. */
export interface Graph {
  nodes: GraphNode[]
  edges: [number, number][]
}

/** What the graph shows. */
export interface GraphOptions {
  /** Tags as nodes linked to their notes. */
  tags: boolean
  /** Linked mails, events, contacts and tasks as nodes. */
  objects: boolean
  /** Notes with no links at all. */
  orphans: boolean
  /** Links to notes that do not exist yet. */
  missing: boolean
  /** Only notes in this folder (and below); '' for all. */
  folder: string
  /** Only notes carrying this tag; '' for all. */
  tag: string
  /** Only notes whose title or path holds this text; '' for all. */
  query: string
}

/** The options a new graph view starts with. */
export const defaultGraphOptions: GraphOptions = { tags: false, objects: false, orphans: true, missing: false, folder: '', tag: '', query: '' }

function objectLabel(url: string, display: string): string {
  const p = parsePeltonUrl(url)
  return display.trim() || (p ? p.id : url)
}

/** Builds the graph of the notes the options let through. */
export function buildGraph(notes: readonly VaultNote[], opts: GraphOptions): Graph {
  const folder = opts.folder.replace(/\/+$/, '')
  const tag = opts.tag.toLowerCase()
  const query = opts.query.trim().toLowerCase()
  const shown = notes.filter((n) => {
    if (folder && !n.path.startsWith(`${folder}/`)) {
      return false
    }
    if (tag && !n.tags.some((x) => x.toLowerCase() === tag || x.toLowerCase().startsWith(`${tag}/`))) {
      return false
    }
    if (query && !n.title.toLowerCase().includes(query) && !n.path.toLowerCase().includes(query)) {
      return false
    }
    return true
  })
  const index = new Map<string, number>()
  const nodes: GraphNode[] = []
  const edges: [number, number][] = []
  const seen = new Set<string>()
  const add = (id: string, kind: GraphNodeKind, label: string): number => {
    let i = index.get(id)
    if (i === undefined) {
      i = nodes.length
      index.set(id, i)
      nodes.push({ id, kind, label, degree: 0 })
    }
    return i
  }
  const link = (a: number, b: number): void => {
    if (a === b) {
      return
    }
    const key = a < b ? `${a}:${b}` : `${b}:${a}`
    if (seen.has(key)) {
      return
    }
    seen.add(key)
    edges.push([a, b])
    nodes[a].degree++
    nodes[b].degree++
  }
  for (const n of shown) {
    add(n.path, 'note', n.title)
  }
  for (const n of shown) {
    const from = index.get(n.path)!
    for (const l of n.links) {
      if ((l.kind === 'wiki' || l.kind === 'markdown') && l.resolved) {
        const to = index.get(l.resolved)
        if (to !== undefined && /\.md$/i.test(l.resolved)) {
          link(from, to)
        }
      } else if (opts.missing && l.kind === 'wiki' && !l.resolved && l.target) {
        link(from, add(`missing:${l.target.toLowerCase()}`, 'missing', l.target))
      } else if (opts.objects && l.kind === 'pelton') {
        link(from, add(l.target, 'object', objectLabel(l.target, l.display)))
      }
    }
    if (opts.tags) {
      for (const t of n.tags) {
        link(from, add(`#${t.toLowerCase()}`, 'tag', `#${t}`))
      }
    }
  }
  if (opts.orphans) {
    return { nodes, edges }
  }
  const keep = nodes.map((n) => n.degree > 0)
  const remap = new Map<number, number>()
  const kept: GraphNode[] = []
  nodes.forEach((n, i) => {
    if (keep[i]) {
      remap.set(i, kept.length)
      kept.push(n)
    }
  })
  return { nodes: kept, edges: edges.map(([a, b]) => [remap.get(a)!, remap.get(b)!]) }
}

/** The neighbourhood of one note: everything within depth links, in either direction. */
export function localGraph(notes: readonly VaultNote[], center: string, depth: number, opts: Pick<GraphOptions, 'tags' | 'objects'>): Graph {
  const full = buildGraph(notes, { ...defaultGraphOptions, tags: opts.tags, objects: opts.objects })
  const start = full.nodes.findIndex((n) => n.id === center)
  if (start < 0) {
    return { nodes: [], edges: [] }
  }
  const adj: number[][] = full.nodes.map(() => [])
  for (const [a, b] of full.edges) {
    adj[a].push(b)
    adj[b].push(a)
  }
  const dist = new Map<number, number>([[start, 0]])
  const queue = [start]
  while (queue.length) {
    const cur = queue.shift()!
    const d = dist.get(cur)!
    if (d >= depth) {
      continue
    }
    for (const next of adj[cur]) {
      if (!dist.has(next)) {
        dist.set(next, d + 1)
        queue.push(next)
      }
    }
  }
  const order = [...dist.keys()].sort((a, b) => a - b)
  const remap = new Map(order.map((old, i) => [old, i]))
  const nodes = order.map((i) => ({ ...full.nodes[i], degree: 0 }))
  const edges: [number, number][] = []
  for (const [a, b] of full.edges) {
    const x = remap.get(a)
    const y = remap.get(b)
    if (x !== undefined && y !== undefined) {
      edges.push([x, y])
      nodes[x].degree++
      nodes[y].degree++
    }
  }
  return { nodes, edges }
}

// --- layout -------------------------------------------------------------------------

/** A seeded pseudo-random source (mulberry32). */
export function seededRandom(seed: number): () => number {
  let s = seed >>> 0
  return () => {
    s = (s + 0x6d2b79f5) >>> 0
    let t = s
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** The state of a running layout. Positions are in graph units around 0,0. */
export interface Layout {
  n: number
  x: Float64Array
  y: Float64Array
  vx: Float64Array
  vy: Float64Array
  /** Edge endpoints, two entries per edge. */
  edges: Int32Array
  /** How strongly each node pushes, from its degree. */
  weight: Float64Array
  /** Nodes held in place, such as one being dragged. */
  pinned: Uint8Array
  /** The layout's temperature; it cools each step and stops at alphaMin. */
  alpha: number
  random: () => number
}

const alphaMin = 0.002
const alphaDecay = 0.0228
const velocityDecay = 0.6
const linkDistance = 46
const linkStrength = 0.5
const charge = -110
const gravity = 0.03
const theta2 = 0.81

/** Places the nodes on a seeded spiral, ready to settle. */
export function createLayout(graph: Graph, seed = 1, previous?: Map<string, { x: number; y: number }>): Layout {
  const n = graph.nodes.length
  const random = seededRandom(seed)
  const l: Layout = {
    n,
    x: new Float64Array(n),
    y: new Float64Array(n),
    vx: new Float64Array(n),
    vy: new Float64Array(n),
    edges: new Int32Array(graph.edges.length * 2),
    weight: new Float64Array(n),
    pinned: new Uint8Array(n),
    alpha: 1,
    random,
  }
  const golden = Math.PI * (3 - Math.sqrt(5))
  for (let i = 0; i < n; i++) {
    const keep = previous?.get(graph.nodes[i].id)
    if (keep) {
      l.x[i] = keep.x
      l.y[i] = keep.y
    } else {
      const r = 10 * Math.sqrt(0.5 + i)
      const a = i * golden + random() * 0.1
      l.x[i] = r * Math.cos(a)
      l.y[i] = r * Math.sin(a)
    }
    l.weight[i] = 1 + Math.sqrt(graph.nodes[i].degree)
  }
  graph.edges.forEach(([a, b], i) => {
    l.edges[i * 2] = a
    l.edges[i * 2 + 1] = b
  })
  return l
}

interface Quad {
  x0: number
  y0: number
  size: number
  mass: number
  cx: number
  cy: number
  point: number
  kids: (Quad | null)[] | null
}

function quad(x0: number, y0: number, size: number): Quad {
  return { x0, y0, size, mass: 0, cx: 0, cy: 0, point: -1, kids: null }
}

// add puts node i into the tree under q, keeping every cell's centre of mass.
// Points that land on the same spot stop splitting at a fixed depth and
// simply share the cell.
function add(q: Quad, i: number, l: Layout, depth: number): void {
  const w = l.weight[i]
  q.cx = (q.cx * q.mass + l.x[i] * w) / (q.mass + w)
  q.cy = (q.cy * q.mass + l.y[i] * w) / (q.mass + w)
  q.mass += w
  if (!q.kids) {
    if (q.point < 0) {
      q.point = i
      return
    }
    if (depth > 40) {
      return
    }
    q.kids = [null, null, null, null]
    const old = q.point
    q.point = -1
    addToKid(q, old, l, depth)
  }
  addToKid(q, i, l, depth)
}

function addToKid(q: Quad, i: number, l: Layout, depth: number): void {
  const half = q.size / 2
  const right = l.x[i] >= q.x0 + half ? 1 : 0
  const bottom = l.y[i] >= q.y0 + half ? 1 : 0
  const k = bottom * 2 + right
  let kid = q.kids![k]
  if (!kid) {
    kid = quad(q.x0 + right * half, q.y0 + bottom * half, half)
    q.kids![k] = kid
  }
  add(kid, i, l, depth + 1)
}

function repel(q: Quad, i: number, l: Layout, alpha: number): void {
  if (q.mass === 0 || q.point === i) {
    return
  }
  let dx = q.cx - l.x[i]
  let dy = q.cy - l.y[i]
  let d2 = dx * dx + dy * dy
  if (q.kids && (q.size * q.size) / Math.max(d2, 1e-9) >= theta2) {
    for (const k of q.kids) {
      if (k) {
        repel(k, i, l, alpha)
      }
    }
    return
  }
  if (d2 === 0) {
    dx = (l.random() - 0.5) * 1e-3
    dy = (l.random() - 0.5) * 1e-3
    d2 = dx * dx + dy * dy
  }
  if (d2 < 1) {
    d2 = Math.sqrt(d2)
  }
  const f = (charge * q.mass * alpha) / d2
  l.vx[i] += dx * f * (1 / l.weight[i])
  l.vy[i] += dy * f * (1 / l.weight[i])
}

/** Runs steps of the layout and returns its temperature; at or below alphaMin it has settled. */
export function stepLayout(l: Layout, steps = 1): number {
  for (let s = 0; s < steps && l.alpha > alphaMin; s++) {
    const alpha = l.alpha
    if (l.n > 1) {
      let minX = Infinity
      let minY = Infinity
      let maxX = -Infinity
      let maxY = -Infinity
      for (let i = 0; i < l.n; i++) {
        minX = Math.min(minX, l.x[i])
        minY = Math.min(minY, l.y[i])
        maxX = Math.max(maxX, l.x[i])
        maxY = Math.max(maxY, l.y[i])
      }
      const size = Math.max(maxX - minX, maxY - minY, 1) + 1
      const root = quad(minX, minY, size)
      for (let i = 0; i < l.n; i++) {
        add(root, i, l, 0)
      }
      for (let i = 0; i < l.n; i++) {
        repel(root, i, l, alpha)
      }
    }
    for (let e = 0; e < l.edges.length; e += 2) {
      const a = l.edges[e]
      const b = l.edges[e + 1]
      let dx = l.x[b] + l.vx[b] - l.x[a] - l.vx[a]
      let dy = l.y[b] + l.vy[b] - l.y[a] - l.vy[a]
      const d = Math.sqrt(dx * dx + dy * dy) || 1e-6
      const k = ((d - linkDistance) / d) * alpha * linkStrength
      dx *= k
      dy *= k
      const wa = l.weight[a]
      const wb = l.weight[b]
      const bias = wa / (wa + wb)
      l.vx[b] -= dx * bias
      l.vy[b] -= dy * bias
      l.vx[a] += dx * (1 - bias)
      l.vy[a] += dy * (1 - bias)
    }
    for (let i = 0; i < l.n; i++) {
      l.vx[i] -= l.x[i] * gravity * alpha
      l.vy[i] -= l.y[i] * gravity * alpha
      if (l.pinned[i]) {
        l.vx[i] = 0
        l.vy[i] = 0
        continue
      }
      l.vx[i] *= velocityDecay
      l.vy[i] *= velocityDecay
      l.x[i] += l.vx[i]
      l.y[i] += l.vy[i]
    }
    l.alpha += (0 - l.alpha) * alphaDecay
  }
  return l.alpha
}

/** True once the layout has cooled down and stepping it changes nothing visible. */
export function layoutSettled(l: Layout): boolean {
  return l.alpha <= alphaMin
}

/** Warms a layout back up after a node was dragged or the graph changed. */
export function reheat(l: Layout, alpha = 0.3): void {
  l.alpha = Math.max(l.alpha, alpha)
}

/** Runs a layout until it settles or the step cap is reached; for reduced motion and tests. */
export function settleLayout(l: Layout, maxSteps = 600): void {
  stepLayout(l, maxSteps)
}

/** The node nearest a graph-space point within radius, or -1. */
export function nodeAt(l: Layout, x: number, y: number, radius: number): number {
  let best = -1
  let bestD = radius * radius
  for (let i = 0; i < l.n; i++) {
    const dx = l.x[i] - x
    const dy = l.y[i] - y
    const d = dx * dx + dy * dy
    if (d <= bestD) {
      best = i
      bestD = d
    }
  }
  return best
}
