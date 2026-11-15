import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  sendMessage: vi.fn(),
  saveDraft: vi.fn(),
  deleteDraft: vi.fn(),
  uploadDraft: vi.fn(),
  composeProtectionStatus: vi.fn(),
}))

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  ...api,
}))

vi.mock('../../stores/outbox', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/outbox')>()),
  loadOutbox: vi.fn(),
}))

vi.mock('../../stores/signatures', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../stores/signatures')>()),
  getAccountSignatures: vi.fn().mockResolvedValue({ headerId: 0, footerId: 0 }),
}))

import Composer2 from './Composer2.svelte'
import { composeSessions, getSession, openCompose, updateCompose, type ComposeSession } from '../../stores/compose'
import { activeComposer, runComposeCommand } from '../../stores/composecommands'
import { extraPaletteCommands } from '../../lib/palettesources'
import { prefs } from '../../stores/prefs'
import { t } from '../../lib/i18n'
import { buildFollowUpPresets } from '../../lib/composeactions'

let unsubscribe: (() => void) | null = null

function mount(id: number): void {
  updateCompose(id, { signaturesApplied: true })
  const session = getSession(id) as ComposeSession
  const { rerender } = render(Composer2, { props: { session, layout: 'window' } })
  unsubscribe = composeSessions.subscribe((list) => {
    const next = list.find((s) => s.id === id)
    if (next) {
      void rerender({ session: next, layout: 'window' })
    }
  })
}

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset()
  }
  api.sendMessage.mockResolvedValue(7)
  api.composeProtectionStatus.mockResolvedValue({ canSign: false, canEncrypt: false, suggested: 'none', recipients: [] })
  prefs.update((p) => ({ ...p, reduceMotion: true, composeVimMode: false, sendDelaySeconds: 0 }))
})

afterEach(() => {
  unsubscribe?.()
  unsubscribe = null
  composeSessions.set([])
})

describe('buildFollowUpPresets', () => {
  it('offers tomorrow, three days and a week, each at nine', () => {
    const now = new Date(2026, 10, 15, 16, 30)
    const presets = buildFollowUpPresets(now, (k) => k)
    expect(presets.map((p) => p.key)).toEqual(['tomorrow', 'threeDays', 'week'])
    expect(presets.map((p) => p.when.getDate())).toEqual([16, 18, 22])
    expect(presets.every((p) => p.when.getHours() === 9 && p.when.getMinutes() === 0)).toBe(true)
  })
})

describe('the follow-up reminder', () => {
  it('is set from the send options and goes out with the message', async () => {
    const id = openCompose(1, 'wysiwyg')
    updateCompose(id, { to: 'ann@example.com', subject: 'Quote' })
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Remind me' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /In 3 days/ }))

    const at = getSession(id)?.followUpAt ?? ''
    expect(new Date(at).getHours()).toBe(9)
    expect(screen.getByRole('button', { name: /^Remind / })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Send' }))
    await vi.waitFor(() => expect(api.sendMessage).toHaveBeenCalledOnce())
    expect(api.sendMessage.mock.calls[0][0].followUpAt).toBe(at)
  })

  it('can be taken off again', async () => {
    const id = openCompose(1, 'wysiwyg')
    updateCompose(id, { followUpAt: new Date(Date.now() + 86_400_000).toISOString() })
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: /^Remind / }))
    await userEvent.click(screen.getByRole('menuitem', { name: "Don't remind me" }))

    expect(getSession(id)?.followUpAt).toBeUndefined()
  })

  it('refuses a reminder that would come before a scheduled send', async () => {
    const id = openCompose(1, 'wysiwyg')
    updateCompose(id, { to: 'ann@example.com', subject: 'Quote', followUpAt: new Date(Date.now() - 1000).toISOString() })
    mount(id)

    await userEvent.click(screen.getByRole('button', { name: 'Send' }))

    expect(api.sendMessage).not.toHaveBeenCalled()
    expect(getSession(id)).toBeDefined()
  })

  it('opens from its shortcut', async () => {
    const id = openCompose(1, 'wysiwyg')
    mount(id)
    screen.getByRole('textbox', { name: 'Subject' }).focus()

    await userEvent.keyboard('{Control>}{Shift>}f{/Shift}{/Control}')

    expect(screen.getByRole('menu', { name: 'Remind me if nobody replies' })).toBeInTheDocument()
  })

  it('opens from the command palette on the active composer', async () => {
    const id = openCompose(1, 'wysiwyg')
    mount(id)
    expect(get(activeComposer)).toBe(id)

    const entry = extraPaletteCommands(get(t)).find((c) => c.id === 'compose:compose-follow-up')
    expect(entry).toBeDefined()
    entry?.run()

    expect(await screen.findByRole('menu', { name: 'Remind me if nobody replies' })).toBeInTheDocument()
  })

  it('does nothing when no composer is open', () => {
    activeComposer.set(null)
    expect(runComposeCommand('compose-follow-up')).toBe(false)
  })
})
