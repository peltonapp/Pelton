<script lang="ts">
  // the graph of the notes space, drawn on a canvas: notes as dots sized by
  // how linked they are, links as lines. it pans with a drag, zooms with the
  // wheel, nodes can be dragged, hovering lights a node's neighbours and a
  // click opens a note. the layout steps a slice per frame and stops once it
  // has settled; with reduced motion it settles out of sight and is drawn
  // once. colours come from the theme's tokens and follow it.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { createLayout, layoutSettled, nodeAt, reheat, stepLayout, type Graph, type Layout } from '../../lib/notegraph'
  import { currentUIScale } from '../../theme/theme'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'

  /** The graph to draw. */
  export let graph: Graph
  /** The id of the node to mark as current, such as the open note. */
  export let current = ''
  /** Labels on every node from this zoom on; hovered and current always show theirs. */
  export let labelZoom = 1.2
  /** Accessible name. */
  export let label = ''

  const dispatch = createEventDispatcher<{ open: { id: string; kind: string; newTab: boolean } }>()

  let wrap: HTMLDivElement
  let canvas: HTMLCanvasElement
  let width = 0
  let height = 0
  let layout: Layout | null = null
  let positions = new Map<string, { x: number; y: number }>()
  let cam = { x: 0, y: 0, k: 1 }
  let hover = -1
  let neighbours = new Set<number>()
  let frame = 0
  let settling = false
  let adjacency: number[][] = []
  // the node ids of the layout before the graph last changed, so nodes that
  // stay keep their place.
  let prevIds: string[] = []
  let colors = readColors()
  let fitted = false

  $: reduced = $prefs.reduceMotion || (typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches)

  interface Colors {
    node: string
    tag: string
    object: string
    missing: string
    edge: string
    accent: string
    text: string
    halo: string
    font: string
  }

  function readColors(): Colors {
    const el = typeof document !== 'undefined' ? document.documentElement : null
    const s = el ? getComputedStyle(el) : null
    const v = (name: string, fallback: string): string => s?.getPropertyValue(name).trim() || fallback
    return {
      node: v('--text-secondary', 'gray'),
      tag: v('--text-tertiary', 'gray'),
      object: v('--link', 'blue'),
      missing: v('--border-strong', 'gray'),
      edge: v('--border-strong', 'gray'),
      accent: v('--accent', 'blue'),
      text: v('--text-primary', 'black'),
      halo: v('--surface-raised', 'white'),
      font: v('--font-ui', 'sans-serif'),
    }
  }

  $: rebuild(graph)

  function rebuild(g: Graph): void {
    if (layout) {
      for (let i = 0; i < layout.n; i++) {
        const id = prevIds[i]
        if (id) {
          positions.set(id, { x: layout.x[i], y: layout.y[i] })
        }
      }
    }
    layout = createLayout(g, 7, positions)
    prevIds = g.nodes.map((n) => n.id)
    adjacency = g.nodes.map(() => [])
    for (const [a, b] of g.edges) {
      adjacency[a].push(b)
      adjacency[b].push(a)
    }
    hover = -1
    neighbours = new Set()
    if (positions.size && g.nodes.some((n) => positions.has(n.id))) {
      layout.alpha = 0.3
    }
    start()
  }

  function start(): void {
    settling = true
    if (!frame) {
      frame = requestAnimationFrame(tickFrame)
    }
  }

  // one frame: step the layout within a time budget, then draw. reduced
  // motion steps without drawing until the layout has settled.
  function tickFrame(): void {
    frame = 0
    if (!layout) {
      return
    }
    const budget = reduced ? 24 : 8
    const began = performance.now()
    while (!layoutSettled(layout) && performance.now() - began < budget) {
      stepLayout(layout, 1)
    }
    const done = layoutSettled(layout)
    if (!fitted && (done || !reduced)) {
      fit()
    }
    if (!reduced || done) {
      draw()
    }
    settling = !done
    if (!done) {
      frame = requestAnimationFrame(tickFrame)
    }
  }

  // centres the graph and zooms so it fills the canvas.
  function fit(): void {
    if (!layout || layout.n === 0 || !width || !height) {
      return
    }
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    for (let i = 0; i < layout.n; i++) {
      minX = Math.min(minX, layout.x[i])
      minY = Math.min(minY, layout.y[i])
      maxX = Math.max(maxX, layout.x[i])
      maxY = Math.max(maxY, layout.y[i])
    }
    const span = Math.max(maxX - minX, maxY - minY, 60)
    cam.k = Math.min(2, Math.max(0.08, (Math.min(width, height) * 0.85) / span))
    cam.x = -((minX + maxX) / 2) * cam.k
    cam.y = -((minY + maxY) / 2) * cam.k
    fitted = layoutSettled(layout)
  }

  function radius(i: number): number {
    return 2.5 + Math.sqrt(graph.nodes[i]?.degree ?? 0) * 1.4
  }

  function draw(): void {
    if (!canvas || !layout) {
      return
    }
    const dpr = window.devicePixelRatio || 1
    const w = Math.max(1, Math.round(width * dpr))
    const h = Math.max(1, Math.round(height * dpr))
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w
      canvas.height = h
    }
    const ctx = canvas.getContext('2d')
    if (!ctx) {
      return
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    ctx.clearRect(0, 0, width, height)
    ctx.translate(width / 2 + cam.x, height / 2 + cam.y)
    ctx.scale(cam.k, cam.k)
    const l = layout
    const focus = hover >= 0
    const currentIndex = graph.nodes.findIndex((n) => n.id === current)

    ctx.lineWidth = 1 / cam.k
    for (let e = 0; e < l.edges.length; e += 2) {
      const a = l.edges[e]
      const b = l.edges[e + 1]
      const lit = focus && (a === hover || b === hover)
      ctx.globalAlpha = focus ? (lit ? 0.9 : 0.12) : 0.45
      ctx.strokeStyle = lit ? colors.accent : colors.edge
      ctx.beginPath()
      ctx.moveTo(l.x[a], l.y[a])
      ctx.lineTo(l.x[b], l.y[b])
      ctx.stroke()
    }

    for (let i = 0; i < l.n; i++) {
      const node = graph.nodes[i]
      const lit = !focus || i === hover || neighbours.has(i)
      ctx.globalAlpha = lit ? 1 : 0.25
      const r = radius(i)
      const mine = i === hover || i === currentIndex
      ctx.beginPath()
      if (node.kind === 'object') {
        ctx.moveTo(l.x[i], l.y[i] - r)
        ctx.lineTo(l.x[i] + r, l.y[i])
        ctx.lineTo(l.x[i], l.y[i] + r)
        ctx.lineTo(l.x[i] - r, l.y[i])
        ctx.closePath()
      } else {
        ctx.arc(l.x[i], l.y[i], r, 0, Math.PI * 2)
      }
      if (node.kind === 'tag' || node.kind === 'missing') {
        ctx.lineWidth = 1.5 / cam.k
        ctx.strokeStyle = mine ? colors.accent : node.kind === 'tag' ? colors.tag : colors.missing
        ctx.stroke()
      } else {
        ctx.fillStyle = mine ? colors.accent : node.kind === 'object' ? colors.object : colors.node
        ctx.fill()
      }
    }

    const fontSize = 11 / cam.k
    ctx.font = `${fontSize}px ${colors.font}`
    ctx.textAlign = 'center'
    ctx.textBaseline = 'top'
    ctx.lineWidth = 3 / cam.k
    ctx.strokeStyle = colors.halo
    for (let i = 0; i < l.n; i++) {
      const show = i === hover || i === currentIndex || (focus ? neighbours.has(i) : cam.k >= labelZoom)
      if (!show) {
        continue
      }
      ctx.globalAlpha = 1
      ctx.fillStyle = colors.text
      const y = l.y[i] + radius(i) + 2 / cam.k
      const text = graph.nodes[i].label
      ctx.strokeText(text, l.x[i], y)
      ctx.fillText(text, l.x[i], y)
    }
    ctx.globalAlpha = 1
  }

  function redraw(): void {
    if (!settling || reduced) {
      draw()
    }
  }

  // --- pointer -----------------------------------------------------------------------

  // the pointer in the canvas's own pixels: client rects are zoomed by the
  // root's interface scale, the canvas's css size is not.
  function local(e: MouseEvent): { x: number; y: number } {
    const rect = canvas.getBoundingClientRect()
    const scale = currentUIScale()
    return { x: (e.clientX - rect.left) / scale, y: (e.clientY - rect.top) / scale }
  }

  function toGraph(p: { x: number; y: number }): { x: number; y: number } {
    return { x: (p.x - width / 2 - cam.x) / cam.k, y: (p.y - height / 2 - cam.y) / cam.k }
  }

  let drag: { kind: 'pan' | 'node'; index: number; startX: number; startY: number; camX: number; camY: number; moved: boolean } | null = null

  function hit(e: MouseEvent): number {
    if (!layout) {
      return -1
    }
    const g = toGraph(local(e))
    return nodeAt(layout, g.x, g.y, Math.max(8 / cam.k, 6))
  }

  function setHover(i: number): void {
    if (i === hover) {
      return
    }
    hover = i
    neighbours = new Set(i >= 0 ? adjacency[i] : [])
    canvas.style.cursor = i >= 0 ? 'pointer' : 'grab'
    redraw()
  }

  function onPointerDown(e: PointerEvent): void {
    if (e.button !== 0) {
      return
    }
    canvas.setPointerCapture(e.pointerId)
    const i = hit(e)
    const p = local(e)
    fitted = true
    drag = { kind: i >= 0 ? 'node' : 'pan', index: i, startX: p.x, startY: p.y, camX: cam.x, camY: cam.y, moved: false }
    if (i >= 0 && layout) {
      layout.pinned[i] = 1
    }
  }

  function onPointerMove(e: PointerEvent): void {
    if (!drag) {
      setHover(hit(e))
      return
    }
    const p = local(e)
    if (Math.abs(p.x - drag.startX) + Math.abs(p.y - drag.startY) > 3) {
      drag.moved = true
    }
    if (drag.kind === 'pan') {
      cam.x = drag.camX + (p.x - drag.startX)
      cam.y = drag.camY + (p.y - drag.startY)
      redraw()
      return
    }
    if (layout && drag.moved) {
      const g = toGraph(p)
      layout.x[drag.index] = g.x
      layout.y[drag.index] = g.y
      if (reduced) {
        draw()
      } else {
        reheat(layout, 0.15)
        start()
      }
    }
  }

  function onPointerUp(e: PointerEvent): void {
    if (!drag) {
      return
    }
    const d = drag
    drag = null
    if (layout && d.index >= 0) {
      layout.pinned[d.index] = 0
    }
    if (!d.moved && d.index >= 0) {
      const node = graph.nodes[d.index]
      dispatch('open', { id: node.id, kind: node.kind, newTab: e.metaKey || e.ctrlKey })
    }
  }

  function onWheel(e: WheelEvent): void {
    e.preventDefault()
    fitted = true
    const p = local(e)
    const before = toGraph(p)
    const factor = Math.exp(-e.deltaY * 0.0015)
    cam.k = Math.min(6, Math.max(0.05, cam.k * factor))
    cam.x = p.x - width / 2 - before.x * cam.k
    cam.y = p.y - height / 2 - before.y * cam.k
    redraw()
  }

  /** Zooms to show the whole graph. */
  export function zoomToFit(): void {
    fitted = false
    fit()
    draw()
  }

  let themeWatch: MutationObserver | null = null
  let resize: ResizeObserver | null = null

  onMount(() => {
    themeWatch = new MutationObserver(() => {
      colors = readColors()
      draw()
    })
    themeWatch.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme', 'style', 'class'] })
    resize = new ResizeObserver(() => {
      width = wrap.clientWidth
      height = wrap.clientHeight
      if (!fitted) {
        fit()
      }
      draw()
    })
    resize.observe(wrap)
    width = wrap.clientWidth
    height = wrap.clientHeight
    canvas.style.cursor = 'grab'
    start()
  })

  onDestroy(() => {
    if (frame) {
      cancelAnimationFrame(frame)
    }
    themeWatch?.disconnect()
    resize?.disconnect()
  })
</script>

<div class="graph" bind:this={wrap}>
  <canvas
    bind:this={canvas}
    aria-label={label || $t('notes.graph')}
    on:pointerdown={onPointerDown}
    on:pointermove={onPointerMove}
    on:pointerup={onPointerUp}
    on:pointerleave={() => !drag && setHover(-1)}
    on:wheel={onWheel}
  ></canvas>
  {#if settling && reduced}
    <span class="busy"><PieProgress label={$t('notes.graphSettling')} /></span>
  {/if}
  {#if graph.nodes.length === 0}
    <p class="empty">{$t('notes.graphEmpty')}</p>
  {/if}
</div>

<style>
  .graph {
    position: relative;
    width: 100%;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    background: var(--surface-raised);
  }

  canvas {
    display: block;
    width: 100%;
    height: 100%;
    touch-action: none;
  }

  .busy {
    position: absolute;
    inset-block-start: var(--space-3);
    inset-inline-end: var(--space-3);
    color: var(--text-tertiary);
  }

  .empty {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-list);
    pointer-events: none;
  }
</style>
