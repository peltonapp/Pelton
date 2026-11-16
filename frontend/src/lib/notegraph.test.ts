import { describe, expect, it } from 'vitest'
import { buildGraph, createLayout, defaultGraphOptions, layoutSettled, localGraph, nodeAt, seededRandom, stepLayout } from './notegraph'
import type { VaultLink, VaultNote } from './types'

function link(resolved: string, over: Partial<VaultLink> = {}): VaultLink {
  return { kind: 'wiki', embed: false, target: resolved.replace(/\.md$/, ''), subpath: '', display: '', line: 0, resolved, ...over }
}

function note(path: string, links: VaultLink[] = [], tags: string[] = []): VaultNote {
  return { path, title: path.replace(/^.*\//, '').replace(/\.md$/, ''), mtime: 0, size: 0, tags, aliases: [], headings: [], snippet: '', links }
}

const notes = [
  note('A.md', [link('B.md'), link('C.md'), link('', { target: 'Ghost' }), link('', { kind: 'pelton', target: 'pelton://mail/m1', display: 'Invoice' })], ['work']),
  note('B.md', [link('A.md')]),
  note('dir/C.md', [], ['work/x']),
  note('Lonely.md'),
  note('img.md', [link('img/pic.png', { embed: true })]),
]
notes[0].links[1] = link('dir/C.md')

describe('buildGraph', () => {
  it('links notes once each way and keeps orphans by default', () => {
    const g = buildGraph(notes, defaultGraphOptions)
    expect(g.nodes.map((n) => n.id)).toEqual(['A.md', 'B.md', 'dir/C.md', 'Lonely.md', 'img.md'])
    expect(g.edges).toEqual([
      [0, 1],
      [0, 2],
    ])
    expect(g.nodes[0].degree).toBe(2)
  })

  it('adds tags, objects and missing notes when asked, and drops orphans', () => {
    const g = buildGraph(notes, { ...defaultGraphOptions, tags: true, objects: true, missing: true, orphans: false })
    const ids = g.nodes.map((n) => n.id)
    expect(ids).toContain('#work')
    expect(ids).toContain('#work/x')
    expect(ids).toContain('pelton://mail/m1')
    expect(ids).toContain('missing:ghost')
    expect(ids).not.toContain('Lonely.md')
    expect(g.nodes.find((n) => n.id === 'pelton://mail/m1')?.label).toBe('Invoice')
  })

  it('filters by folder, tag and text', () => {
    expect(buildGraph(notes, { ...defaultGraphOptions, folder: 'dir' }).nodes.map((n) => n.id)).toEqual(['dir/C.md'])
    expect(buildGraph(notes, { ...defaultGraphOptions, tag: 'work' }).nodes.map((n) => n.id)).toEqual(['A.md', 'dir/C.md'])
    expect(buildGraph(notes, { ...defaultGraphOptions, query: 'lone' }).nodes.map((n) => n.id)).toEqual(['Lonely.md'])
  })
})

describe('localGraph', () => {
  it('takes the neighbourhood in both directions', () => {
    expect(localGraph(notes, 'B.md', 1, { tags: false, objects: false }).nodes.map((n) => n.id)).toEqual(['A.md', 'B.md'])
    expect(localGraph(notes, 'B.md', 2, { tags: false, objects: false }).nodes.map((n) => n.id)).toEqual(['A.md', 'B.md', 'dir/C.md'])
    expect(localGraph(notes, 'nope.md', 2, { tags: false, objects: false }).nodes).toEqual([])
  })
})

describe('layout', () => {
  function ring(n: number) {
    const many = Array.from({ length: n }, (_, i) => note(`n${i}.md`, [link(`n${(i + 1) % n}.md`)]))
    return buildGraph(many, defaultGraphOptions)
  }

  it('is deterministic for a seed', () => {
    const g = ring(40)
    const a = createLayout(g, 3)
    const b = createLayout(g, 3)
    stepLayout(a, 50)
    stepLayout(b, 50)
    expect(Array.from(a.x)).toEqual(Array.from(b.x))
    expect(Array.from(a.y)).toEqual(Array.from(b.y))
    const c = createLayout(g, 4)
    stepLayout(c, 50)
    expect(Array.from(c.x)).not.toEqual(Array.from(a.x))
  })

  it('settles and stops moving', () => {
    const l = createLayout(ring(30), 1)
    stepLayout(l, 1000)
    expect(layoutSettled(l)).toBe(true)
    const before = Array.from(l.x)
    stepLayout(l, 10)
    expect(Array.from(l.x)).toEqual(before)
  })

  it('pulls linked nodes closer than unlinked ones', () => {
    const g = buildGraph([note('a.md', [link('b.md')]), note('b.md'), note('c.md')], defaultGraphOptions)
    const l = createLayout(g, 9)
    stepLayout(l, 1000)
    const d = (i: number, j: number) => Math.hypot(l.x[i] - l.x[j], l.y[i] - l.y[j])
    expect(d(0, 1)).toBeLessThan(d(0, 2))
  })

  it('stays fast enough for a large vault', () => {
    const g = ring(2000)
    const l = createLayout(g, 1)
    const started = performance.now()
    stepLayout(l, 5)
    expect(performance.now() - started).toBeLessThan(2500)
    expect(Number.isFinite(l.x[0])).toBe(true)
  })

  it('keeps positions of nodes that stay', () => {
    const g = ring(5)
    const l = createLayout(g, 2, new Map([['n0.md', { x: 123, y: -4 }]]))
    expect(l.x[0]).toBe(123)
    expect(l.y[0]).toBe(-4)
    expect(nodeAt(l, 120, -4, 5)).toBe(0)
    expect(nodeAt(l, 1e6, 0, 5)).toBe(-1)
  })

  it('draws the same numbers from the same seed', () => {
    const a = seededRandom(42)
    const b = seededRandom(42)
    expect([a(), a(), a()]).toEqual([b(), b(), b()])
  })
})
