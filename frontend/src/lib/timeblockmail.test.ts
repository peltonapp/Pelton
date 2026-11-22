import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  blockTimeForMessages: vi.fn(),
  deleteTimeBlocks: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  ...api,
}))

import { spaceDrop } from './spaces'
import { mailDropHint } from '../components/calendar/maildrop'
import { calendarSeed } from '../stores/calendar'
import { space } from '../stores/space'
import { toasts } from '../stores/toast'

beforeEach(() => {
  vi.clearAllMocks()
  toasts.set([])
  space.set('mail')
  calendarSeed.set(null)
  api.blockTimeForMessages.mockResolvedValue([
    { id: 9, title: 'Invoice', start: '2026-11-16T09:00:00+01:00', end: '2026-11-16T09:30:00+01:00' },
  ])
  api.deleteTimeBlocks.mockResolvedValue(undefined)
})

describe('mail dropped on the Calendar entry with Alt held', () => {
  it('blocks the next free time in place and offers undo', async () => {
    const drop = spaceDrop('calendar')
    drop.onDrop([{ id: 4 } as never], { shift: false, alt: true })
    await vi.waitFor(() => expect(api.blockTimeForMessages).toHaveBeenCalledWith([4], ''))
    expect(get(space)).toBe('mail')
    expect(get(calendarSeed)).toBeNull()
    const toast = await vi.waitFor(() => {
      const found = get(toasts).find((x) => x.message.startsWith('Time blocked'))
      expect(found).toBeTruthy()
      return found
    })
    toast?.action?.run()
    await vi.waitFor(() => expect(api.deleteTimeBlocks).toHaveBeenCalledWith([9]))
  })

  it('still makes an event from a plain drop', () => {
    spaceDrop('calendar').onDrop([{ id: 4 } as never], { shift: false })
    expect(api.blockTimeForMessages).not.toHaveBeenCalled()
    expect(get(calendarSeed)).toMatchObject({ messageIds: [4], shift: false })
  })

  it('says what Alt does while dragging', () => {
    expect(mailDropHint(1, false, true)).toContain('to block time for it instead')
    expect(mailDropHint(1, false, true, true).split('\n')[0]).toBe('Block time here to deal with the mail')
    expect(mailDropHint(3, false, false, true).split('\n')[0]).toBe('3 time blocks, one per mail')
  })
})
