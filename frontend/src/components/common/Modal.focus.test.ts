import { afterEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import { createRawSnippet, tick, type ComponentProps } from 'svelte'
import Modal from './Modal.svelte'
import { prefs } from '../../stores/prefs'

// the dialog opens with a transition, and jsdom has no web animations.
Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}

const field = createRawSnippet(() => ({ render: () => '<label>Name <input type="text" /></label>' }))

async function open(): Promise<void> {
  const props = { title: 'Rename', children: field, $$slots: { default: true } }
  render(Modal, { props: props as unknown as ComponentProps<typeof Modal> })
  await tick()
  await tick()
}

afterEach(() => prefs.update((p) => ({ ...p, design: 'new' })))

describe('Modal focus on open', () => {
  it('focuses the dialog itself in the classic design', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    await open()

    expect(document.activeElement).toBe(screen.getByRole('dialog'))
  })

  it('focuses the first text field in the new design', async () => {
    await open()

    expect(document.activeElement).toBe(screen.getByRole('textbox', { name: 'Name' }))
  })
})
