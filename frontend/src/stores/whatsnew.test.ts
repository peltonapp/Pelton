import { beforeEach, describe, expect, it, vi } from 'vitest'
import { get } from 'svelte/store'

const api = vi.hoisted(() => ({
  appVersion: vi.fn(),
  getSetting: vi.fn(),
  setSetting: vi.fn(),
}))

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  ...api,
}))

beforeEach(() => {
  vi.resetModules()
  api.setSetting.mockResolvedValue(undefined)
})

async function load(version: string, stored: string | null): Promise<typeof import('./whatsnew')> {
  api.appVersion.mockResolvedValue(version)
  api.getSetting.mockResolvedValue(stored === null ? { found: false, value: '' } : { found: true, value: stored })
  const m = await import('./whatsnew')
  await m.initWhatsNew()
  return m
}

describe("what's new store", () => {
  it('marks a version with unopened notes, and opening clears it for good', async () => {
    const m = await load('v2027.1', null)
    expect(get(m.whatsNewUnseen)).toBe(true)
    m.openWhatsNew()
    expect(get(m.whatsNewOpen)).toBe(true)
    await vi.waitFor(() => expect(api.setSetting).toHaveBeenCalledWith('last_seen_version', expect.stringMatching(/^2027\.1:/)))
    const key = api.setSetting.mock.calls[0][1] as string

    const again = await load('nightly-2026.12.01+abc', key)
    expect(get(again.whatsNewUnseen)).toBe(false)
  })

  it('falls back to the newest notes when the version cannot be read', async () => {
    api.appVersion.mockRejectedValue(new Error('no bindings'))
    api.getSetting.mockResolvedValue({ found: false, value: '' })
    const m = await import('./whatsnew')
    await m.initWhatsNew()
    expect(get(m.whatsNewRelease)?.line).toBe('2027.1')
  })
})
