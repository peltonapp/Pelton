import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MessageSummary } from './types'

const mocks = vi.hoisted(() => ({
  downloadMessageOffline: vi.fn(),
  patchInList: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('./api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('./api')>()),
  downloadMessageOffline: mocks.downloadMessageOffline,
}))
vi.mock('../stores/messages', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../stores/messages')>()),
  patchInList: mocks.patchInList,
}))
vi.mock('../stores/toast', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../stores/toast')>()),
  toastError: mocks.toastError,
}))

import { setOffline } from './messageactions'

describe('setOffline', () => {
  beforeEach(() => vi.clearAllMocks())

  it('puts the offline flag back and shows the error when the download fails', async () => {
    mocks.downloadMessageOffline.mockRejectedValue(new Error('no connection'))

    await setOffline({ id: 5, offline: false } as MessageSummary, true)

    expect(mocks.patchInList.mock.calls).toEqual([
      [5, { offline: true }],
      [5, { offline: false }],
    ])
    expect(mocks.toastError).toHaveBeenCalledTimes(1)
  })
})
