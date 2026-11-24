// keymenu.ts opens the context menu from the keyboard: the menu key or
// shift+F10 sends a contextmenu event to whatever has focus, or to the row a
// list points at with aria-activedescendant, placed below it. Every place that
// offers a context menu already listens for that event, so they all get a
// keyboard path at once.

/** Whether the event is the key that opens a context menu. */
export function isMenuKey(event: KeyboardEvent): boolean {
  return event.key === 'ContextMenu' || (event.key === 'F10' && event.shiftKey && !event.ctrlKey && !event.metaKey && !event.altKey)
}

/** The element a keyboard context menu is for: the active descendant, else the focused element. */
export function menuTarget(): HTMLElement | null {
  const focused = document.activeElement as HTMLElement | null
  if (!focused || focused === document.body) {
    return null
  }
  const id = focused.getAttribute('aria-activedescendant')
  const active = id ? document.getElementById(id) : null
  return active ?? focused
}

/** Opens the context menu of the focused element from the keyboard. Returns whether it handled the key. */
export function openMenuFromKeyboard(event: KeyboardEvent): boolean {
  if (!isMenuKey(event)) {
    return false
  }
  const target = menuTarget()
  if (!target) {
    return false
  }
  event.preventDefault()
  const rect = target.getBoundingClientRect()
  target.dispatchEvent(
    new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: rect.left + Math.min(rect.width / 2, 24),
      clientY: rect.bottom,
    }),
  )
  return true
}
