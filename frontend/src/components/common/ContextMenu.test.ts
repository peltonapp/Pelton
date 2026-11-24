import { afterEach, describe, expect, it } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { tick } from 'svelte'
import ContextMenu from './ContextMenu.svelte'
import { closeContextMenu, openContextMenu } from '../../stores/contextmenu'
import { prefs } from '../../stores/prefs'
import { openMenuFromKeyboard } from '../../lib/keymenu'
import { expectAccessible } from '../../test/a11y'
import { expectAxeClean } from '../../test/axe'

function invoker(): HTMLButtonElement {
  const button = document.createElement('button')
  button.textContent = 'Row'
  button.addEventListener('contextmenu', (e) => {
    openContextMenu(e.clientX, e.clientY, [
      { label: 'Archive', action: () => {} },
      'separator',
      { label: 'Delete', action: () => {} },
    ])
  })
  document.body.appendChild(button)
  return button
}

afterEach(() => {
  closeContextMenu()
  document.body.innerHTML = ''
  prefs.update((p) => ({ ...p, design: 'new' }))
})

describe('ContextMenu keyboard', () => {
  it('opens from shift+F10, takes focus, walks with arrows and hands focus back on Escape', async () => {
    const user = userEvent.setup()
    render(ContextMenu)
    const button = invoker()
    button.focus()

    const event = new KeyboardEvent('keydown', { key: 'F10', shiftKey: true, cancelable: true })
    expect(openMenuFromKeyboard(event)).toBe(true)
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Archive' })))
    expectAccessible()
    await expectAxeClean()

    await user.keyboard('{ArrowDown}')
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Delete' }))
    await user.keyboard('{ArrowDown}')
    expect(document.activeElement).toBe(screen.getByRole('menuitem', { name: 'Archive' }))

    await user.keyboard('{Escape}')
    await tick()
    expect(screen.queryByRole('menu')).toBeNull()
    expect(document.activeElement).toBe(button)
  })

  it('leaves focus alone in the classic design', async () => {
    prefs.update((p) => ({ ...p, design: 'classic' }))
    render(ContextMenu)
    const button = invoker()
    button.focus()
    openContextMenu(0, 0, [{ label: 'Archive', action: () => {} }])
    await tick()
    await tick()
    expect(screen.getByRole('menuitem', { name: 'Archive' })).not.toHaveAttribute('tabindex')
    expect(document.activeElement).toBe(button)
  })
})
