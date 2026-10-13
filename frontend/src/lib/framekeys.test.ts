import { afterEach, describe, expect, it, vi } from 'vitest'
import { frameKeyRelay, relayedKey, replayKey } from './framekeys'

describe('mail body key relay (#496)', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('posts a key pressed in the body to the parent', () => {
    const posted = vi.spyOn(window, 'postMessage').mockImplementation(() => {})
    const listeners: EventListener[] = []
    vi.spyOn(document, 'addEventListener').mockImplementation((type, fn) => {
      if (type === 'keydown') listeners.push(fn as EventListener)
    })
    // the relay runs in the iframe, where window.parent is the app; here they
    // are the same window.
    new Function(frameKeyRelay)()

    for (const fn of listeners) {
      fn(new KeyboardEvent('keydown', { key: 'k', code: 'KeyK', ctrlKey: true }))
      fn(new KeyboardEvent('keydown', { key: 'Tab' }))
    }

    expect(posted).toHaveBeenCalledOnce()
    expect(posted.mock.calls[0][0]).toEqual({
      peltonKey: { key: 'k', code: 'KeyK', ctrlKey: true, metaKey: false, altKey: false, shiftKey: false, repeat: false },
    })
  })

  it('reads a relayed key and nothing else', () => {
    expect(relayedKey({ peltonKey: { key: 'k', metaKey: true } })).toMatchObject({ key: 'k', metaKey: true, ctrlKey: false })
    expect(relayedKey({ peltonOpenUrl: 'https://example.com' })).toBeNull()
    expect(relayedKey({ peltonKey: { key: '' } })).toBeNull()
    expect(relayedKey(null)).toBeNull()
  })

  it('replays the key where the app listens for shortcuts', () => {
    const frame = document.createElement('iframe')
    document.body.appendChild(frame)
    const seen = vi.fn()
    window.addEventListener('keydown', seen)

    replayKey(frame, { key: 'k', ctrlKey: true })

    window.removeEventListener('keydown', seen)
    frame.remove()
    expect(seen).toHaveBeenCalledOnce()
    const event = seen.mock.calls[0][0] as KeyboardEvent
    expect(event.key).toBe('k')
    expect(event.ctrlKey).toBe(true)
    expect(event.target).toBe(frame)
  })
})
