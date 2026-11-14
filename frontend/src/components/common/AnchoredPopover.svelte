<script lang="ts">
  // a panel hanging off a control: the send-later menu, the composer's
  // overflow menu, the outbox popover. it lives at the end of <body> so no
  // ancestor clips it, places itself against the anchor (flipping when there
  // is no room on the asked-for side), and closes on Escape or a click
  // outside it and its anchor.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { portal } from '../../lib/portal'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { currentUIScale } from '../../theme/theme'

  /** The control the panel hangs off. */
  export let anchor: HTMLElement
  /** The side of the anchor the panel opens on, when there is room. */
  export let side: 'above' | 'below' = 'below'
  /** The edge of the anchor the panel lines up with, in reading direction. */
  export let align: 'start' | 'end' = 'end'
  /** Accessible name of the panel. */
  export let label: string
  /** ARIA role of the panel. */
  export let role: 'dialog' | 'menu' = 'dialog'

  const dispatch = createEventDispatcher<{ close: void }>()

  const gap = 6
  const margin = 8

  let panel: HTMLDivElement
  let left = 0
  let top = 0
  let up = false
  let placed = false

  // place works in the zoomed layout space a fixed element is drawn in: the
  // root carries a css zoom for the interface scale, and client rects come
  // back unzoomed, so they are divided by the scale first.
  async function place(): Promise<void> {
    await tick()
    if (!anchor || !panel) {
      return
    }
    const scale = currentUIScale()
    const rect = anchor.getBoundingClientRect()
    const a = { left: rect.left / scale, right: rect.right / scale, top: rect.top / scale, bottom: rect.bottom / scale }
    const vw = window.innerWidth / scale
    const vh = window.innerHeight / scale
    const w = panel.offsetWidth
    const h = panel.offsetHeight
    const rtl = getComputedStyle(document.documentElement).direction === 'rtl'
    const toEnd = (align === 'end') !== rtl

    const roomAbove = a.top - gap - margin
    const roomBelow = vh - a.bottom - gap - margin
    up = side === 'above' ? !(h > roomAbove && roomBelow > roomAbove) : h > roomBelow && roomAbove > roomBelow

    const x = toEnd ? a.right - w : a.left
    const y = up ? a.top - gap - h : a.bottom + gap
    left = Math.min(Math.max(x, margin), Math.max(margin, vw - w - margin))
    top = Math.min(Math.max(y, margin), Math.max(margin, vh - h - margin))
    placed = true
  }

  // a press inside something opened from this panel, such as the list of a
  // Select in it, is not outside: those are moved to the end of <body> after
  // the panel, so anything placed after it there belongs to it.
  function openedFromPanel(target: Node): boolean {
    let top: Node = target
    while (top.parentNode && top.parentNode !== document.body) {
      top = top.parentNode
    }
    return top.parentNode === document.body && !!panel && (panel.compareDocumentPosition(top) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
  }

  function onPointerDown(event: PointerEvent): void {
    const target = event.target as Node | null
    if (target && !panel?.contains(target) && !anchor?.contains(target) && !openedFromPanel(target)) {
      dispatch('close')
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dispatch('close')
      anchor?.focus()
    }
  }

  onMount(() => {
    pushPopup()
    void place()
    window.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('resize', place)
  })

  onDestroy(() => {
    popPopup()
    window.removeEventListener('pointerdown', onPointerDown, true)
    window.removeEventListener('resize', place)
  })

  /** Measures again, for content that changed size while open. */
  export function reposition(): void {
    void place()
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="popover"
  class:up
  class:placed
  {role}
  aria-label={label}
  tabindex="-1"
  bind:this={panel}
  use:portal
  style={`left:${left}px; top:${top}px`}
  on:keydown={onKeydown}
>
  <slot />
</div>

<style>
  .popover {
    position: fixed;
    z-index: 800;
    display: flex;
    flex-direction: column;
    max-height: var(--popover-max-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
    visibility: hidden;
    transform-origin: top center;
    outline: none;
  }

  .popover.up {
    transform-origin: bottom center;
  }

  .popover.placed {
    visibility: visible;
    animation: rise var(--motion-base) var(--ease-out);
  }

  .popover.up.placed {
    animation-name: sink;
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(calc(-1 * var(--space-2))) scale(0.98);
    }
  }

  @keyframes sink {
    from {
      opacity: 0;
      transform: translateY(var(--space-2)) scale(0.98);
    }
  }
</style>
