// smoothcaret.ts draws the text caret of a rich editor as an element of its
// own, so it can glide from one place to the next instead of jumping there,
// the way word processors do. the editor's native caret is hidden while this
// one is on.

import type { Editor } from '@tiptap/core'

const idleMs = 520

/**
 * Starts the gliding caret for an editor whose content scrolls inside `host`.
 * Returns the function that removes it and gives the native caret back.
 */
export function smoothCaret(editor: Editor, host: HTMLElement): () => void {
  const caret = document.createElement('div')
  caret.className = 'smooth-caret'
  caret.setAttribute('aria-hidden', 'true')
  host.classList.add('smooth-caret-on')
  host.appendChild(caret)

  let idle: ReturnType<typeof setTimeout> | undefined
  let placed = false

  function place(): void {
    const { state, view } = editor
    if (!view.hasFocus() || !state.selection.empty) {
      caret.classList.remove('shown')
      placed = false
      return
    }
    let at: { left: number; top: number; bottom: number }
    try {
      at = view.coordsAtPos(state.selection.head)
    } catch {
      return
    }
    // the root carries a css zoom for the interface scale; rects are in zoomed
    // pixels and offsets inside the host are not.
    const scale = Number(getComputedStyle(document.documentElement).getPropertyValue('--ui-scale')) || 1
    const box = host.getBoundingClientRect()
    const x = (at.left - box.left) / scale + host.scrollLeft
    const y = (at.top - box.top) / scale + host.scrollTop
    caret.style.height = `${(at.bottom - at.top) / scale}px`
    // the first placement has nowhere to glide from.
    caret.classList.toggle('jump', !placed)
    caret.style.transform = `translate(${x}px, ${y}px)`
    caret.classList.add('shown')
    placed = true
    // it holds steady while it moves and blinks once typing rests.
    caret.classList.remove('blink')
    clearTimeout(idle)
    idle = setTimeout(() => caret.classList.add('blink'), idleMs)
  }

  const observer = new ResizeObserver(place)
  observer.observe(host)
  editor.on('transaction', place)
  editor.on('focus', place)
  editor.on('blur', place)
  place()

  return () => {
    clearTimeout(idle)
    observer.disconnect()
    editor.off('transaction', place)
    editor.off('focus', place)
    editor.off('blur', place)
    caret.remove()
    host.classList.remove('smooth-caret-on')
  }
}
