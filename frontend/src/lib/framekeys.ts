// framekeys.ts carries key presses out of the mail body iframe. A key pressed
// after clicking into a message lands in the iframe's own document and never
// reaches the app's window listener, so every shortcut, the command palette
// included, went dead until focus moved back out (#496).

// frameKeyRelay is the iframe half: script text for the body document that
// posts each key press to the parent. Tab stays behind so focus can still move
// through the body's links, and an IME composition is the user typing.
export const frameKeyRelay =
  'document.addEventListener("keydown",function(e){if(e.key==="Tab"||e.isComposing)return;' +
  'window.parent.postMessage({peltonKey:{key:e.key,code:e.code,ctrlKey:e.ctrlKey,metaKey:e.metaKey,altKey:e.altKey,shiftKey:e.shiftKey,repeat:e.repeat}},"*")});'

// relayedKey reads a relayed press back out of a posted message, or null when
// the message is not one. Only the fields a shortcut match looks at are taken.
export function relayedKey(data: unknown): KeyboardEventInit | null {
  const key = (data as { peltonKey?: unknown } | null)?.peltonKey as Record<string, unknown> | undefined
  if (!key || typeof key !== 'object' || typeof key.key !== 'string' || key.key === '') {
    return null
  }
  return {
    key: key.key,
    code: typeof key.code === 'string' ? key.code : '',
    ctrlKey: key.ctrlKey === true,
    metaKey: key.metaKey === true,
    altKey: key.altKey === true,
    shiftKey: key.shiftKey === true,
    repeat: key.repeat === true,
  }
}

// replayKey fires a relayed press from the iframe element, so it bubbles to the
// window listener as if it had been pressed with the pane focused.
export function replayKey(frame: HTMLElement, init: KeyboardEventInit): void {
  frame.dispatchEvent(new KeyboardEvent('keydown', { ...init, bubbles: true, cancelable: true }))
}
