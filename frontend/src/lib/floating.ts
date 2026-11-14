// floating.ts places a fixed-position popover below the element that opened
// it, so the popover escapes any pane that clips its overflow. The root carries
// a css zoom for the interface scale: client rects come back zoomed while fixed
// offsets do not, so the scale is divided back out (as lib/tip.ts does).

import { currentUIScale } from '../theme/theme'

/** Where a popover opens relative to its anchor. */
export interface FloatOptions {
  /** The element the popover hangs from. */
  anchor: HTMLElement | null | undefined
  /** Which inline edge of the anchor the popover lines up with. */
  align?: 'start' | 'end'
}

const gap = 4
const margin = 4

function place(node: HTMLElement, options: FloatOptions): void {
  const anchor = options.anchor
  if (!anchor) {
    return
  }
  const scale = currentUIScale()
  const rect = anchor.getBoundingClientRect()
  const own = node.getBoundingClientRect()
  const rtl = getComputedStyle(anchor).direction === 'rtl'
  const atEnd = (options.align === 'end') !== rtl
  let left = atEnd ? rect.right - own.width : rect.left
  left = Math.max(margin, Math.min(left, window.innerWidth - own.width - margin))
  let top = rect.bottom + gap
  if (top + own.height > window.innerHeight - margin) {
    top = Math.max(margin, rect.top - own.height - gap)
  }
  node.style.position = 'fixed'
  node.style.left = `${left / scale}px`
  node.style.top = `${top / scale}px`
}

/** Svelte action: keeps the node placed under its anchor while it is shown. */
export function float(node: HTMLElement, options: FloatOptions): { update(next: FloatOptions): void; destroy(): void } {
  let current = options
  const reposition = (): void => place(node, current)
  reposition()
  window.addEventListener('resize', reposition)
  return {
    update(next: FloatOptions): void {
      current = next
      reposition()
    },
    destroy(): void {
      window.removeEventListener('resize', reposition)
    },
  }
}
