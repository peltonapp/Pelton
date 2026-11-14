<script lang="ts">
  // an animated, interactive preview of the compose editor for the onboarding
  // extras step (and reused from settings). autoplay and real typing share one
  // small command engine (applyKey) so "click to try it yourself" behaves
  // exactly like the animation that was just playing. when vim is off it shows
  // natural human typing: uneven per-key timing, pauses at spaces/punctuation,
  // and the occasional mistype that gets backspaced and corrected. when vim is
  // on it demonstrates insert/normal/visual mode, motions (h j k l), yy/p
  // (yank + put a line), dd (delete a line) and visual-mode delete, with a
  // live strip of the keys being "pressed". clicking the preview stops the
  // loop, clears the buffer and hands control to the user, still honoring
  // whichever mode (vim on/off) is currently selected.
  import { onMount, onDestroy } from 'svelte'

  export let enabled = false

  let lines: string[] = ['']
  let cursorLine = 0
  let cursorCol = 0
  let mode: 'INSERT' | 'NORMAL' | 'VISUAL' = 'INSERT'
  let visualAnchor: { line: number; col: number } | null = null
  let yankBuffer = ''
  let pendingKey = ''
  let keysLog: string[] = []
  let interactive = false
  let el: HTMLDivElement

  let gen = 0
  let alive = true

  onMount(start)
  onDestroy(() => {
    alive = false
  })

  // restart the animation whenever the mode toggles, unless the user has taken
  // over the preview interactively.
  $: if (!interactive) {
    void enabled
    restart()
  }

  function resetBuffer(): void {
    lines = ['']
    cursorLine = 0
    cursorCol = 0
    visualAnchor = null
    yankBuffer = ''
    pendingKey = ''
    keysLog = []
    mode = enabled ? 'NORMAL' : 'INSERT'
  }

  function restart(): void {
    gen += 1
    resetBuffer()
    if (alive) {
      void run(gen)
    }
  }
  function start(): void {
    restart()
  }

  const rand = (lo: number, hi: number): number => lo + Math.random() * (hi - lo)
  function sleep(ms: number): Promise<void> {
    return new Promise((r) => setTimeout(r, ms))
  }
  const running = (g: number): boolean => alive && g === gen && !interactive

  // a plausible wrong key for a mistype: a neighbour on the keyboard, else random.
  const neighbours: Record<string, string> = {
    a: 's', s: 'd', d: 'f', e: 'r', r: 't', t: 'y', o: 'i', i: 'o', n: 'm', l: 'k', c: 'v', k: 'l',
  }
  function wrongKey(ch: string): string {
    return neighbours[ch] ?? 'e'
  }

  function logKey(label: string): void {
    keysLog = [...keysLog.slice(-9), label]
  }

  // --- the shared command engine: both the autoplay loop and real keystrokes
  // (once the user clicks in) go through this, so the demo and the real thing
  // never drift apart. ---
  function applyKey(key: string): void {
    if (!enabled) {
      applyPlainKey(key)
      return
    }
    if (mode === 'INSERT') {
      applyInsertKey(key)
    } else if (mode === 'NORMAL') {
      applyNormalKey(key)
    } else {
      applyVisualKey(key)
    }
  }

  function currentLine(): string {
    return lines[cursorLine] ?? ''
  }
  function setLine(i: number, value: string): void {
    lines = lines.map((l, idx) => (idx === i ? value : l))
  }

  function applyPlainKey(key: string): void {
    const line = currentLine()
    if (key === 'Backspace') {
      if (cursorCol > 0) {
        setLine(cursorLine, line.slice(0, cursorCol - 1) + line.slice(cursorCol))
        cursorCol -= 1
      } else if (cursorLine > 0) {
        const prev = lines[cursorLine - 1]
        cursorCol = prev.length
        lines = [...lines.slice(0, cursorLine - 1), prev + line, ...lines.slice(cursorLine + 1)]
        cursorLine -= 1
      }
      return
    }
    if (key === 'Enter') {
      lines = [...lines.slice(0, cursorLine), line.slice(0, cursorCol), line.slice(cursorCol), ...lines.slice(cursorLine + 1)]
      cursorLine += 1
      cursorCol = 0
      return
    }
    if (key.length === 1) {
      setLine(cursorLine, line.slice(0, cursorCol) + key + line.slice(cursorCol))
      cursorCol += 1
    }
  }

  function applyInsertKey(key: string): void {
    if (key === 'Escape') {
      mode = 'NORMAL'
      cursorCol = Math.max(0, cursorCol - 1)
      return
    }
    applyPlainKey(key)
  }

  function clampCol(): void {
    cursorCol = Math.max(0, Math.min(cursorCol, Math.max(0, currentLine().length - 1)))
  }

  function applyNormalKey(key: string): void {
    // dd and yy are two-key commands; track the first press.
    if (pendingKey === 'd' && key === 'd') {
      pendingKey = ''
      if (lines.length > 1) {
        lines = [...lines.slice(0, cursorLine), ...lines.slice(cursorLine + 1)]
        cursorLine = Math.min(cursorLine, lines.length - 1)
      } else {
        lines = ['']
      }
      clampCol()
      return
    }
    if (pendingKey === 'y' && key === 'y') {
      pendingKey = ''
      yankBuffer = currentLine()
      return
    }
    pendingKey = ''

    switch (key) {
      case 'd':
      case 'y':
        pendingKey = key
        break
      case 'p':
        lines = [...lines.slice(0, cursorLine + 1), yankBuffer, ...lines.slice(cursorLine + 1)]
        cursorLine += 1
        break
      case 'i':
        mode = 'INSERT'
        break
      case 'a':
        cursorCol = Math.min(cursorCol + 1, currentLine().length)
        mode = 'INSERT'
        break
      case 'o':
        lines = [...lines.slice(0, cursorLine + 1), '', ...lines.slice(cursorLine + 1)]
        cursorLine += 1
        cursorCol = 0
        mode = 'INSERT'
        break
      case 'v':
        mode = 'VISUAL'
        visualAnchor = { line: cursorLine, col: cursorCol }
        break
      case 'x':
        if (currentLine().length > 0) {
          setLine(cursorLine, currentLine().slice(0, cursorCol) + currentLine().slice(cursorCol + 1))
          clampCol()
        }
        break
      case 'h':
        cursorCol = Math.max(0, cursorCol - 1)
        break
      case 'l':
        cursorCol = Math.min(currentLine().length - 1 < 0 ? 0 : currentLine().length - 1, cursorCol + 1)
        break
      case 'j':
        cursorLine = Math.min(lines.length - 1, cursorLine + 1)
        clampCol()
        break
      case 'k':
        cursorLine = Math.max(0, cursorLine - 1)
        clampCol()
        break
    }
  }

  function applyVisualKey(key: string): void {
    switch (key) {
      case 'Escape':
        mode = 'NORMAL'
        visualAnchor = null
        break
      case 'h':
        cursorCol = Math.max(0, cursorCol - 1)
        break
      case 'l':
        cursorCol = Math.min(Math.max(0, currentLine().length - 1), cursorCol + 1)
        break
      case 'j':
        cursorLine = Math.min(lines.length - 1, cursorLine + 1)
        break
      case 'k':
        cursorLine = Math.max(0, cursorLine - 1)
        break
      case 'd': {
        // char-visual delete, same line only (enough for the demo).
        if (visualAnchor && visualAnchor.line === cursorLine) {
          const from = Math.min(visualAnchor.col, cursorCol)
          const to = Math.max(visualAnchor.col, cursorCol) + 1
          const line = currentLine()
          setLine(cursorLine, line.slice(0, from) + line.slice(to))
          cursorCol = from
        }
        mode = 'NORMAL'
        visualAnchor = null
        break
      }
    }
  }

  // typeChars drives applyKey with human timing and occasional mistypes, used
  // by both the "vim off" plain paragraph and the "vim on" insert-mode text.
  async function typeChars(g: number, str: string): Promise<void> {
    for (const ch of str) {
      if (!running(g)) return
      if (/[a-z]/i.test(ch) && Math.random() < 0.07) {
        const wrong = wrongKey(ch.toLowerCase())
        applyKey(wrong)
        logKey(wrong)
        await sleep(rand(90, 160))
        if (!running(g)) return
        await sleep(rand(140, 260)) // the "oops" beat
        applyKey('Backspace')
        logKey('⌫')
        await sleep(rand(80, 140))
        if (!running(g)) return
      }
      applyKey(ch)
      logKey(ch === ' ' ? '␣' : ch)
      if (ch === ' ') {
        await sleep(rand(120, 240))
      } else if ('.,'.includes(ch)) {
        await sleep(rand(220, 420))
      } else {
        await sleep(rand(45, 130))
      }
    }
  }

  // press runs one discrete command key with a visible pause, for the vim demo.
  async function press(g: number, key: string, label: string, pause = 350): Promise<void> {
    if (!running(g)) return
    applyKey(key)
    logKey(label)
    await sleep(pause)
  }

  async function run(g: number): Promise<void> {
    while (running(g)) {
      if (enabled) {
        // --- vim on: insert, motions, yy/p, dd, visual delete ---
        await press(g, 'i', 'i', 220)
        if (!running(g)) return
        await typeChars(g, 'Hi team,')
        if (!running(g)) return
        await press(g, 'Enter', '⏎', 180)
        if (!running(g)) return
        await typeChars(g, 'See the reprot attached.')
        if (!running(g)) return
        await sleep(rand(400, 600))
        await press(g, 'Escape', 'Esc', 500)
        if (!running(g)) return
        await press(g, 'k', 'k', 350)
        await press(g, 'y', 'y', 180)
        await press(g, 'y', 'y', 450)
        if (!running(g)) return
        await press(g, 'j', 'j', 350)
        await press(g, 'p', 'p', 550)
        if (!running(g)) return
        await press(g, 'j', 'j', 350)
        await press(g, 'd', 'd', 180)
        await press(g, 'd', 'd', 550)
        if (!running(g)) return
        // visual-select "reprot" and delete it, then fix the typo in insert mode.
        cursorLine = 1
        cursorCol = 8
        await press(g, 'v', 'v', 250)
        for (let i = 0; i < 5; i++) {
          await press(g, 'l', 'l', 90)
          if (!running(g)) return
        }
        await press(g, 'd', 'd', 500)
        if (!running(g)) return
        await press(g, 'i', 'i', 220)
        await typeChars(g, 'report')
      } else {
        // --- vim off: natural writing ---
        await typeChars(g, 'Dear team, the launch is on track for Friday.')
      }
      if (!running(g)) return
      await sleep(1800)
      if (!running(g)) return
      resetBuffer()
      await sleep(400)
    }
  }

  // --- interactivity: clicking the preview stops the loop, clears the buffer
  // and lets the user type or use vim motions, per the currently selected
  // vim on/off option. ---
  function enterInteractive(): void {
    if (interactive) {
      el?.focus()
      return
    }
    interactive = true
    gen += 1 // stop the autoplay loop
    resetBuffer()
    keysLog = []
    requestAnimationFrame(() => el?.focus())
  }

  function onPreviewKeydown(event: KeyboardEvent): void {
    if (!interactive) return
    if (event.metaKey || event.ctrlKey || event.altKey) return
    const key = event.key
    // ignore pure modifier presses and anything not meaningful to the tiny
    // command engine above.
    if (key.length > 1 && !['Enter', 'Backspace', 'Escape'].includes(key)) return
    event.preventDefault()
    applyKey(key)
    logKey(key === ' ' ? '␣' : key.length === 1 ? key : key === 'Escape' ? 'Esc' : key === 'Enter' ? '⏎' : key)
  }

  function exitInteractive(): void {
    interactive = false
    restart()
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions a11y-click-events-have-key-events -->
<div
  class="vp"
  class:interactive
  bind:this={el}
  tabindex="0"
  role="button"
  aria-label={interactive ? 'Vim preview, click elsewhere to hand control back to the animation' : 'Click to try it yourself'}
  on:click={enterInteractive}
  on:keydown={onPreviewKeydown}
  on:blur={exitInteractive}
>
  <div class="vp-lines">
    {#each lines as line, i (i)}
      <div class="vp-line">
        {#if enabled && mode === 'VISUAL' && visualAnchor && visualAnchor.line === i && i === cursorLine}
          {@const from = Math.min(visualAnchor.col, cursorCol)}
          {@const to = Math.max(visualAnchor.col, cursorCol) + 1}
          <span>{line.slice(0, from)}</span><span class="vp-visual">{line.slice(from, to) || ' '}</span><span>{line.slice(to)}</span>
        {:else}
          {line}
        {/if}
        {#if i === cursorLine}
          <span class="vp-caret" class:block={enabled && mode !== 'INSERT'}></span>
        {/if}
      </div>
    {/each}
  </div>

  <div class="vp-foot">
    <div class="vp-keys" aria-hidden="true">
      {#each keysLog as k, i (i)}
        <span class="vp-key">{k}</span>
      {/each}
    </div>
    {#if enabled}
      <span class="vp-badge" class:normal={mode === 'NORMAL'} class:visual={mode === 'VISUAL'}>{mode}</span>
    {/if}
  </div>

  {#if !interactive}
    <div class="vp-hint">Click to try it yourself</div>
  {/if}
</div>

<style>
  .vp {
    position: relative;
    width: 100%;
    min-height: 96px;
    padding: 2px;
    border-radius: var(--radius-control);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: var(--fz-label);
    line-height: 1.7;
    color: var(--text-primary);
    cursor: text;
    outline: none;
  }

  .vp.interactive {
    box-shadow: 0 0 0 2px var(--accent);
  }

  .vp-lines {
    white-space: pre-wrap;
    word-break: break-word;
  }

  .vp-line {
    min-height: 1.7em;
  }

  .vp-visual {
    background: var(--selection-bg-strong, var(--selection-bg));
    border-radius: 2px;
  }

  /* the caret: a thin bar in insert, a solid block in vim normal/visual mode. */
  .vp-caret {
    display: inline-block;
    width: 2px;
    height: 1.05em;
    margin-inline-start: 1px;
    background: var(--accent);
    vertical-align: text-bottom;
    animation: vpblink 1s steps(2) infinite;
  }
  .vp-caret.block {
    width: 0.55em;
    background: var(--accent);
    opacity: 0.5;
  }
  @keyframes vpblink {
    50% {
      opacity: 0;
    }
  }

  .vp-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }

  /* a live strip of the last few keys "pressed", oldest to newest. */
  .vp-keys {
    display: flex;
    gap: 3px;
    min-height: 18px;
    overflow: hidden;
  }

  .vp-key {
    padding: 1px 5px;
    border: var(--hairline) solid var(--border-default);
    border-radius: 4px;
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  .vp-badge {
    flex-shrink: 0;
    padding: 1px var(--space-2);
    border-radius: var(--radius-control);
    background: var(--success, var(--accent));
    color: #fff;
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.04em;
  }
  .vp-badge.normal {
    background: var(--text-tertiary);
  }
  .vp-badge.visual {
    background: var(--warning, var(--accent));
  }

  .vp-hint {
    position: absolute;
    top: 0;
    right: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    opacity: 0;
    transition: opacity 0.15s;
    pointer-events: none;
  }
  .vp:hover .vp-hint {
    opacity: 1;
  }
</style>
