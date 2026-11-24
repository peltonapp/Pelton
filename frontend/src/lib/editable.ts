// editable.ts imports nothing, so any component can use it without pulling in
// stores.

/** Whether a key event's target takes typing: a form field or editable content. */
export function isEditableTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  return !!el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
}
