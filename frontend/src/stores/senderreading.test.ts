import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  listSenderReading: vi.fn(),
  setSenderColors: vi.fn(),
  setSenderFontSize: vi.fn(),
  resetSenderReading: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

import { choiceFor, ensureSenderReading, forgetSenderReading, messageColors, rememberColors, rememberFontSize, senderKey, senderReading, setMessageColors } from './senderreading'

beforeEach(() => {
  for (const fn of Object.values(api)) {
    fn.mockReset().mockResolvedValue(undefined)
  }
  senderReading.set(new Map())
  messageColors.set(new Map())
})

describe('senderreading', () => {
  it('keys a sender by its bare lower case address', () => {
    expect(senderKey('Ada Lovelace <Ada@Example.com>')).toBe('ada@example.com')
  })

  it('reads the remembered choices once', async () => {
    api.listSenderReading.mockResolvedValue([{ address: 'ada@example.com', colors: 'original', fontSize: 18 }])
    await ensureSenderReading()
    await ensureSenderReading()
    expect(api.listSenderReading).toHaveBeenCalledTimes(1)
    expect(choiceFor(get(senderReading), 'ADA@example.com')).toEqual({ colors: 'original', fontSize: 18 })
  })

  it('keeps one choice when the other is forgotten, and drops the sender once both are', async () => {
    await rememberColors('ada@example.com', 'adapt')
    await rememberFontSize('ada@example.com', 16)
    await rememberColors('ada@example.com', '')
    expect(choiceFor(get(senderReading), 'ada@example.com')).toEqual({ colors: '', fontSize: 16 })
    await rememberFontSize('ada@example.com', 0)
    expect(get(senderReading).size).toBe(0)
    expect(api.setSenderFontSize).toHaveBeenLastCalledWith('ada@example.com', 0)
  })

  it('puts the choice back when the store refuses it', async () => {
    api.setSenderFontSize.mockRejectedValue(new Error('disk full'))
    await rememberFontSize('ada@example.com', 18)
    expect(get(senderReading).size).toBe(0)
  })

  it('forgets everything, the message choices of the session included', async () => {
    api.resetSenderReading.mockResolvedValue(2)
    await rememberFontSize('ada@example.com', 18)
    setMessageColors(4, 'original')
    expect(await forgetSenderReading()).toBe(2)
    expect(get(senderReading).size).toBe(0)
    expect(get(messageColors).size).toBe(0)
  })
})
