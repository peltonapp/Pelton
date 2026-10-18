import { describe, expect, it, vi } from 'vitest'
import { render } from '@testing-library/svelte'
import { tick } from 'svelte'
import Compose from './Compose.svelte'
import { composeSessions, getSession, updateCompose, type ComposeSession } from '../../stores/compose'
import { composeProtectionStatus } from '../../lib/api'

vi.mock('../../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../lib/api')>()),
  composeProtectionStatus: vi.fn(),
}))

const status = (canEncrypt: boolean) =>
  ({
  canSign: true,
  signerLocked: false,
  canEncrypt,
  recipients: [],
  default: 'none',
  suggested: 'none',
  }) as never

function session(to: string): ComposeSession {
  return {
    id: 1,
    accountId: 1,
    mode: 'plaintext',
    to,
    cc: '',
    bcc: '',
    showCc: false,
    showBcc: false,
    subject: '',
    body: '',
    attachments: [],
    inReplyTo: '',
    references: [],
    draftId: 0,
    fullscreen: false,
    minimized: false,
    signaturesApplied: true,
    protection: 'none',
  }
}

describe('Compose protection check', () => {
  it('ignores a late answer for an older recipient list', async () => {
    let answerA!: (s: never) => void
    const asked: string[][] = []
    vi.mocked(composeProtectionStatus).mockImplementation((_account, recipients) => {
      asked.push(recipients)
      if (recipients.join() === 'a@x.com') {
        return new Promise((resolve) => (answerA = resolve))
      }
      return Promise.resolve(status(true))
    })

    composeSessions.set([session('a@x.com')])
    const { rerender } = render(Compose, { session: session('a@x.com') })
    await tick()

    await rerender({ session: session('b@x.com') })
    await vi.waitFor(() => expect(asked).toHaveLength(2))
    updateCompose(1, { protection: 'encrypt' })
    await rerender({ session: { ...getSession(1)!, to: 'b@x.com' } })

    answerA(status(false))
    await tick()
    await tick()

    expect(asked).toEqual([['a@x.com'], ['b@x.com']])
    expect(getSession(1)!.protection).toBe('encrypt')
  })

  it('asks again when the account changes and ignores the old account\'s late answer', async () => {
    let answerFirst!: (s: never) => void
    const asked: [number, string[]][] = []
    vi.mocked(composeProtectionStatus).mockImplementation((account, recipients) => {
      asked.push([account, recipients])
      if (account === 1) {
        return new Promise((resolve) => (answerFirst = resolve))
      }
      return Promise.resolve(status(true))
    })

    composeSessions.set([session('a@x.com')])
    const { rerender } = render(Compose, { session: session('a@x.com') })
    await tick()

    updateCompose(1, { accountId: 2 })
    await rerender({ session: getSession(1)! })
    await vi.waitFor(() => expect(asked).toHaveLength(2))
    updateCompose(1, { protection: 'encrypt' })
    await rerender({ session: getSession(1)! })

    answerFirst(status(false))
    await tick()
    await tick()

    expect(asked).toEqual([
      [1, ['a@x.com']],
      [2, ['a@x.com']],
    ])
    expect(getSession(1)!.protection).toBe('encrypt')
  })

  it('passes a quoted local part with a comma as one recipient', async () => {
    const asked: string[][] = []
    vi.mocked(composeProtectionStatus).mockImplementation((_account, recipients) => {
      asked.push(recipients)
      return Promise.resolve(status(true))
    })

    composeSessions.set([session('"a,b"@x.com')])
    render(Compose, { session: session('"a,b"@x.com') })
    await vi.waitFor(() => expect(asked).toHaveLength(1))

    expect(asked[0]).toEqual(['"a,b"@x.com'])
  })
})
