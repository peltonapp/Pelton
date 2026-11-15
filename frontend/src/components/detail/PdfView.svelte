<script lang="ts">
  // renders a pdf with the bundled pdf.js instead of the webview's own viewer,
  // which WebKitGTK does not have. every support file is served from the app
  // itself under /pdfjs/, so opening a pdf makes no network request. pages are
  // drawn as they scroll into view, each with a text layer for selecting and
  // copying. scripts and forms in the pdf are never run.
  import { onDestroy, onMount, tick } from 'svelte'
  import { IconMinus, IconPlus, IconArrowsHorizontal } from '@tabler/icons-svelte'
  import type { PDFDocumentProxy, PDFPageProxy, RenderTask, TextLayer } from 'pdfjs-dist/legacy/build/pdf.mjs'
  import { t } from '../../lib/i18n'

  export let data: Uint8Array

  const minZoom = 0.25
  const maxZoom = 4
  const zoomStep = 1.2
  const pagePadding = 24

  let scroller: HTMLDivElement
  let doc: PDFDocumentProxy | null = null
  let loadingTask: { destroy: () => Promise<void> } | null = null
  // the size of each page at 100%, read once so the placeholders can be laid
  // out before any page is drawn.
  let sizes: { width: number; height: number }[] = []
  let fit = true
  let scale = 1
  let errorText = ''
  let loading = true
  let destroyed = false

  // per page: the scale it was last drawn at, and the work still running.
  const drawn = new Map<number, number>()
  const tasks = new Map<number, { render: RenderTask; text: TextLayer | null }>()
  const pageEls: HTMLDivElement[] = []
  let observer: IntersectionObserver | null = null
  let resizeObserver: ResizeObserver | null = null
  const visible = new Set<number>()

  onMount(() => {
    void open()
  })

  async function open(): Promise<void> {
    try {
      const pdfjs = await import('pdfjs-dist/legacy/build/pdf.mjs')
      const worker = await import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?url')
      pdfjs.GlobalWorkerOptions.workerSrc = worker.default
      const base = (dir: string) => new URL(`pdfjs/${dir}/`, document.baseURI).href
      // pdf.js takes ownership of the buffer it is given, so it gets a copy.
      const task = pdfjs.getDocument({
        data: data.slice(),
        cMapUrl: base('cmaps'),
        cMapPacked: true,
        standardFontDataUrl: base('standard_fonts'),
        iccUrl: base('iccs'),
        wasmUrl: base('wasm'),
        enableXfa: false,
      })
      loadingTask = task
      const d = await task.promise
      if (destroyed) {
        void task.destroy()
        return
      }
      doc = d
      const out: { width: number; height: number }[] = []
      for (let i = 1; i <= d.numPages; i++) {
        const page = await d.getPage(i)
        const vp = page.getViewport({ scale: 1 })
        out.push({ width: vp.width, height: vp.height })
      }
      sizes = out
      loading = false
      await tick()
      fitWidth()
      watch()
    } catch (err) {
      loading = false
      const name = (err as { name?: string })?.name
      errorText = name === 'PasswordException' ? $t('detail.attachments.pdfLocked') : $t('detail.attachments.pdfFailed')
    }
  }

  function watch(): void {
    observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const n = Number((e.target as HTMLElement).dataset.page)
          if (e.isIntersecting) {
            visible.add(n)
            void draw(n)
          } else {
            visible.delete(n)
          }
        }
      },
      { root: scroller, rootMargin: '600px 0px' },
    )
    pageEls.forEach((el) => el && observer?.observe(el))
    resizeObserver = new ResizeObserver(() => {
      if (fit) {
        fitWidth()
      }
    })
    resizeObserver.observe(scroller)
  }

  function fitWidth(): void {
    if (!scroller || sizes.length === 0) {
      return
    }
    const widest = Math.max(...sizes.map((s) => s.width))
    const next = clamp((scroller.clientWidth - 2 * pagePadding) / widest)
    fit = true
    setScale(next)
  }

  function clamp(s: number): number {
    return Math.min(maxZoom, Math.max(minZoom, s))
  }

  function zoom(factor: number): void {
    fit = false
    setScale(clamp(scale * factor))
  }

  function setScale(next: number): void {
    scale = next
    void tick().then(() => visible.forEach((n) => void draw(n)))
  }

  async function draw(n: number): Promise<void> {
    if (!doc || drawn.get(n) === scale) {
      return
    }
    const el = pageEls[n - 1]
    if (!el) {
      return
    }
    const at = scale
    drawn.set(n, at)
    cancel(n)

    let page: PDFPageProxy
    try {
      page = await doc.getPage(n)
    } catch {
      drawn.delete(n)
      return
    }
    if (destroyed || drawn.get(n) !== at) {
      return
    }
    const viewport = page.getViewport({ scale: at })
    const ratio = window.devicePixelRatio || 1
    const canvas = document.createElement('canvas')
    canvas.width = Math.floor(viewport.width * ratio)
    canvas.height = Math.floor(viewport.height * ratio)

    const render = page.render({
      canvas,
      viewport,
      transform: ratio !== 1 ? [ratio, 0, 0, ratio, 0, 0] : undefined,
    })
    const job: { render: RenderTask; text: TextLayer | null } = { render, text: null }
    tasks.set(n, job)
    try {
      await render.promise
    } catch {
      // cancelled by a newer draw, or the page failed; either way this one is done.
      if (tasks.get(n) === job) {
        tasks.delete(n)
        drawn.delete(n)
      }
      return
    }
    if (destroyed || tasks.get(n) !== job) {
      return
    }

    const textDiv = document.createElement('div')
    textDiv.className = 'textLayer'
    el.replaceChildren(canvas, textDiv)

    try {
      const { TextLayer } = await import('pdfjs-dist/legacy/build/pdf.mjs')
      const text = new TextLayer({ textContentSource: page.streamTextContent(), container: textDiv, viewport })
      job.text = text
      await text.render()
    } catch {
      // the page stays readable without its text layer, it just cannot be selected.
    }
    if (tasks.get(n) === job) {
      tasks.delete(n)
    }
  }

  function cancel(n: number): void {
    const job = tasks.get(n)
    if (job) {
      job.render.cancel()
      job.text?.cancel()
      tasks.delete(n)
    }
  }

  onDestroy(() => {
    destroyed = true
    observer?.disconnect()
    resizeObserver?.disconnect()
    for (const n of [...tasks.keys()]) {
      cancel(n)
    }
    void loadingTask?.destroy()
  })

  $: percent = Math.round(scale * 100)
</script>

<div class="pdfview">
  {#if errorText}
    <div class="state">{errorText}</div>
  {:else}
    <div class="bar">
      {#if !loading}
        <span class="count">
          {sizes.length === 1 ? $t('detail.attachments.pdfPage') : $t('detail.attachments.pdfPages').replace('{count}', String(sizes.length))}
        </span>
      {/if}
      <span class="spacer"></span>
      <button type="button" class="zbtn" title={$t('detail.attachments.zoomOut')} aria-label={$t('detail.attachments.zoomOut')} disabled={loading || scale <= minZoom} on:click={() => zoom(1 / zoomStep)}>
        <IconMinus size={14} stroke={1.8} />
      </button>
      <span class="percent">{percent}%</span>
      <button type="button" class="zbtn" title={$t('detail.attachments.zoomIn')} aria-label={$t('detail.attachments.zoomIn')} disabled={loading || scale >= maxZoom} on:click={() => zoom(zoomStep)}>
        <IconPlus size={14} stroke={1.8} />
      </button>
      <button type="button" class="zbtn" class:active={fit} title={$t('detail.attachments.fitWidth')} aria-label={$t('detail.attachments.fitWidth')} disabled={loading} on:click={fitWidth}>
        <IconArrowsHorizontal size={14} stroke={1.8} />
      </button>
    </div>
    <div class="pages" bind:this={scroller}>
      {#if loading}
        <div class="state">{$t('detail.attachments.loadingPreview')}</div>
      {:else}
        {#each sizes as size, i (i)}
          <div
            class="page"
            data-page={i + 1}
            bind:this={pageEls[i]}
            style="width: {Math.floor(size.width * scale)}px; height: {Math.floor(size.height * scale)}px; --scale-factor: {scale}; --total-scale-factor: {scale};"
          ></div>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<style>
  .pdfview {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .bar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-overlay);
    flex-shrink: 0;
  }

  .count,
  .percent {
    font-size: var(--fz-meta, var(--fz-label));
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .percent {
    min-width: 3.5em;
    text-align: center;
  }

  .spacer {
    flex: 1;
  }

  .zbtn {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }
  .zbtn:hover:not(:disabled),
  .zbtn.active {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .zbtn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .pages {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-3);
    padding: 24px;
  }

  /* auto margins rather than align-items: center, so a page zoomed wider than
     the viewer still scrolls to its left edge. */
  .page {
    position: relative;
    flex-shrink: 0;
    margin-inline: auto;
    background: #fff;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.25);
    --scale-round-x: 1px;
    --scale-round-y: 1px;
  }

  /* sized by the page box, so a page drawn at the old zoom stretches until it
     is drawn again. */
  .page :global(canvas) {
    display: block;
    width: 100%;
    height: 100%;
  }

  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    padding: var(--space-5);
  }

  /* the essentials of pdf.js's own text layer rules: invisible text laid over
     the canvas, positioned glyph run by glyph run, so selection follows the page. */
  .page :global(.textLayer) {
    position: absolute;
    inset: 0;
    overflow: clip;
    line-height: 1;
    text-align: initial;
    letter-spacing: normal;
    word-spacing: normal;
    text-size-adjust: none;
    -webkit-text-size-adjust: none;
    forced-color-adjust: none;
    transform-origin: 0 0;
    z-index: 0;
    --min-font-size: 1;
    --text-scale-factor: calc(var(--total-scale-factor) * var(--min-font-size));
    --min-font-size-inv: calc(1 / var(--min-font-size));
  }
  .page :global(.textLayer :is(span, br)) {
    color: transparent;
    position: absolute;
    white-space: pre;
    cursor: text;
    transform-origin: 0% 0%;
    user-select: text;
    -webkit-user-select: text;
  }
  .page :global(.textLayer > :not(.markedContent)),
  .page :global(.textLayer .markedContent span:not(.markedContent)) {
    z-index: 1;
    --font-height: 0;
    font-size: calc(var(--text-scale-factor) * var(--font-height));
    --scale-x: 1;
    --rotate: 0deg;
    transform: rotate(var(--rotate)) scaleX(var(--scale-x)) scale(var(--min-font-size-inv));
  }
  .page :global(.textLayer .markedContent) {
    display: contents;
  }
  .page :global(.textLayer span[role='img']) {
    user-select: none;
    -webkit-user-select: none;
    cursor: default;
  }
  .page :global(.textLayer ::selection) {
    background: color-mix(in srgb, var(--accent) 35%, transparent);
    color: transparent;
  }
  .page :global(.textLayer .endOfContent) {
    display: block;
    position: absolute;
    inset: 100% 0 0;
    z-index: 0;
    cursor: default;
    user-select: none;
    -webkit-user-select: none;
  }
</style>
