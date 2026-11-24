import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { createRawSnippet, tick, type ComponentProps } from 'svelte'
import type { LockState } from '../../lib/types'

const api = vi.hoisted(() => ({
  unlockWithSystem: vi.fn(),
  unlockWithPassphrase: vi.fn(),
  getLockState: vi.fn(),
  noteActivity: vi.fn(),
  quitApp: vi.fn(),
  windowFrameIntegrated: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

// the lock event, fired by hand.
const lockEvents = vi.hoisted(() => ({ handler: null as ((e: { locked: boolean }) => void) | null }))
vi.mock('../../lib/events', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/events')>()),
  onAppLock: (cb: (e: { locked: boolean }) => void) => {
    lockEvents.handler = cb
    return () => {
      lockEvents.handler = null
    }
  },
}))

import LockScreen from './LockScreen.svelte'
import LockGate from './LockGate.svelte'

function lockState(patch: Partial<LockState> = {}): LockState {
  return {
    enabled: true,
    locked: true,
    method: 'passphrase',
    systemAvailable: false,
    retryAfterMs: 0,
    theme: 'dark',
    accent: '#465AF2',
    language: 'en',
    ...patch,
  }
}

// mail stands in for a window's content.
const mail = createRawSnippet(() => ({ render: () => '<p>Quarterly numbers from Ada</p>' }))

// renderGate mounts the gate with mail as its slot content. A legacy slot
// takes a snippet only when told the slot is filled.
function renderGate(main: boolean, initial: LockState) {
  const props = { main, initial, children: mail, $$slots: { default: true } }
  return render(LockGate, { props: props as unknown as ComponentProps<typeof LockGate> })
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.noteActivity.mockResolvedValue(undefined)
  api.windowFrameIntegrated.mockResolvedValue(false)
  api.getLockState.mockResolvedValue(lockState())
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LockScreen with a passphrase', () => {
  it('unlocks with the right passphrase', async () => {
    api.unlockWithPassphrase.mockResolvedValue({ ok: true })
    const unlocked = vi.fn()
    render(LockScreen, { props: { state: lockState() }, events: { unlocked } })

    expect(screen.getByRole('heading', { name: 'Pelton is locked' })).toBeInTheDocument()
    const unlock = screen.getByRole('button', { name: 'Unlock' })
    expect(unlock).toBeDisabled()

    await userEvent.type(screen.getByLabelText('Passphrase'), 'long enough{Enter}')

    expect(api.unlockWithPassphrase).toHaveBeenCalledWith('long enough')
    expect(unlocked).toHaveBeenCalledTimes(1)
  })

  it('says so when the passphrase is wrong and clears the field', async () => {
    api.unlockWithPassphrase.mockResolvedValue({ ok: false, reason: 'wrong' })
    const unlocked = vi.fn()
    render(LockScreen, { props: { state: lockState() }, events: { unlocked } })

    const field = screen.getByLabelText('Passphrase')
    await userEvent.type(field, 'nope{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('That passphrase is not right.')
    expect(field).toHaveValue('')
    expect(unlocked).not.toHaveBeenCalled()
  })

  it('waits after too many attempts', async () => {
    api.unlockWithPassphrase.mockResolvedValue({ ok: false, reason: 'wrong', retryAfterMs: 5000 })
    render(LockScreen, { props: { state: lockState() } })

    const field = screen.getByLabelText('Passphrase')
    await userEvent.type(field, 'nope{Enter}')

    expect(await screen.findByRole('alert')).toHaveTextContent('Too many attempts. Try again in 5 s.')
    expect(field).toBeDisabled()
  })

  it('keeps the wait it was locked with', () => {
    render(LockScreen, { props: { state: lockState({ retryAfterMs: 12_000 }) } })
    expect(screen.getByLabelText('Passphrase')).toBeDisabled()
    expect(screen.getByRole('alert')).toHaveTextContent('Try again in 12 s.')
  })

  it('never offers the system prompt', () => {
    render(LockScreen, { props: { state: lockState() } })
    expect(api.unlockWithSystem).not.toHaveBeenCalled()
  })
})

describe('LockScreen with the system prompt', () => {
  it('asks once by itself when the window has focus, then on the button', async () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(true)
    api.unlockWithSystem.mockResolvedValue({ ok: false, reason: 'cancelled' })
    const unlocked = vi.fn()
    render(LockScreen, { props: { state: lockState({ method: 'system', systemAvailable: true }) }, events: { unlocked } })

    await waitFor(() => expect(api.unlockWithSystem).toHaveBeenCalledTimes(1))
    expect(await screen.findByRole('alert')).toHaveTextContent('Unlocking was cancelled.')
    expect(screen.queryByLabelText('Passphrase')).toBeNull()

    window.dispatchEvent(new Event('focus'))
    await tick()
    expect(api.unlockWithSystem).toHaveBeenCalledTimes(1)

    api.unlockWithSystem.mockResolvedValue({ ok: true })
    await userEvent.click(screen.getByRole('button', { name: 'Unlock' }))
    expect(api.unlockWithSystem).toHaveBeenCalledTimes(2)
    expect(unlocked).toHaveBeenCalledTimes(1)
  })

  it('waits for focus before asking', async () => {
    vi.spyOn(document, 'hasFocus').mockReturnValue(false)
    api.unlockWithSystem.mockResolvedValue({ ok: true })
    render(LockScreen, { props: { state: lockState({ method: 'system', systemAvailable: true }) } })

    await tick()
    expect(api.unlockWithSystem).not.toHaveBeenCalled()
    window.dispatchEvent(new Event('focus'))
    await waitFor(() => expect(api.unlockWithSystem).toHaveBeenCalledTimes(1))
  })
})

describe('LockGate', () => {
  it('takes the main window content out of the page while locked', async () => {
    renderGate(true, lockState({ locked: false }))
    expect(screen.getByText('Quarterly numbers from Ada')).toBeInTheDocument()

    lockEvents.handler?.({ locked: true })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Pelton is locked' })).toBeInTheDocument())
    expect(screen.queryByText('Quarterly numbers from Ada')).toBeNull()
    expect(document.body.textContent).not.toContain('Ada')

    lockEvents.handler?.({ locked: false })
    await tick()
    expect(screen.getByText('Quarterly numbers from Ada')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Pelton is locked' })).toBeNull()
  })

  it('mounts nothing of the main window when it starts locked', async () => {
    renderGate(true, lockState())
    expect(screen.getByRole('heading', { name: 'Pelton is locked' })).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('Ada')

    api.unlockWithPassphrase.mockResolvedValue({ ok: true })
    await userEvent.type(screen.getByLabelText('Passphrase'), 'long enough{Enter}')
    expect(await screen.findByText('Quarterly numbers from Ada')).toBeInTheDocument()
  })

  it('keeps a secondary window content hidden while locked', async () => {
    const { container } = renderGate(false, lockState({ locked: false }))

    lockEvents.handler?.({ locked: true })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Pelton is locked' })).toBeInTheDocument())
    const held = container.querySelector('.held')
    expect(held).toHaveClass('hidden')
    expect(held).toHaveTextContent('Quarterly numbers from Ada')

    lockEvents.handler?.({ locked: false })
    await tick()
    expect(held).not.toHaveClass('hidden')
  })

  it('waits for the unlock before mounting a secondary window opened while locked', () => {
    renderGate(false, lockState())
    expect(document.body.textContent).not.toContain('Ada')
  })

  it('reports input only while unlocked', async () => {
    renderGate(true, lockState({ locked: false }))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    expect(api.noteActivity).toHaveBeenCalledTimes(1)
  })
})
