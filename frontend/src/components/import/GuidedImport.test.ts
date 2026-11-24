import { beforeEach, describe, expect, it, vi } from 'vitest'
import { render, screen, waitFor } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import type { ImportInventory, ImportJob } from '../../lib/guidedimport'
import { expectAxeClean } from '../../test/axe'

const api = vi.hoisted(() => ({
  findThunderbirdProfiles: vi.fn(),
  chooseImportSource: vi.fn(),
  scanImportSource: vi.fn(),
  estimateImport: vi.fn(),
  importTargets: vi.fn(),
  startImportJob: vi.fn(),
  pauseImportJob: vi.fn(),
  resumeImportJob: vi.fn(),
  cancelImportJob: vi.fn(),
  importJobs: vi.fn(),
}))
const events = vi.hoisted(() => ({
  listener: null as ((job: ImportJob) => void) | null,
}))

vi.mock('../../lib/api', async (importOriginal) => ({ ...(await importOriginal<object>()), ...api }))
vi.mock('../../lib/events', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  onGuidedImport: (cb: (job: ImportJob) => void) => {
    events.listener = cb
    return () => undefined
  },
}))
vi.mock('../../stores/accounts', () => ({ loadSidebar: vi.fn() }))
vi.mock('../../stores/passwordprompt', () => ({ promptForMissingPasswords: vi.fn() }))

Element.prototype.animate ??= function () {
  const animation = { onfinish: null as (() => void) | null, cancel() {}, finished: Promise.resolve() }
  queueMicrotask(() => animation.onfinish?.())
  return animation as unknown as Animation
}
Element.prototype.getAnimations ??= function () {
  return []
}

import GuidedImport from './GuidedImport.svelte'

const inventory: ImportInventory = {
  kind: 'folder',
  paths: ['/home/wren/Mail'],
  accounts: [],
  folders: [
    { key: 'maildir:/home/wren/Mail', path: ['Mail'], count: 2, sizeBytes: 2048, serverCopy: false },
    { key: 'maildir:/home/wren/Mail/.Work', path: ['Mail', 'Work'], count: 1, sizeBytes: 1024, serverCopy: false },
  ],
  addressBooks: [{ key: 'vcf:/home/wren/Mail/people.vcf', name: 'people', count: 2 }],
  calendars: [],
  filters: [],
  notices: [{ code: 'pstUnavailable', name: 'old.pst' }],
}

function job(over: Partial<ImportJob>): ImportJob {
  return {
    id: 7, kind: 'files', sourcePath: '/home/wren/Mail', state: 'running',
    plan: { kind: 'files', paths: [], accounts: [], folders: [], addressBooks: [], calendars: [], filters: [], destination: { mode: 'local', accountId: 0, folder: '' } },
    progress: { phase: 'mail', current: 'Mail/Work', done: 1, total: 3, imported: 1, skipped: 0, failed: 0 },
    report: { lines: [] }, error: '', createdAt: '', updatedAt: '',
    ...over,
  }
}

beforeEach(() => {
  vi.clearAllMocks()
  events.listener = null
  api.importJobs.mockResolvedValue([])
  api.chooseImportSource.mockResolvedValue(['/home/wren/Mail'])
  api.scanImportSource.mockResolvedValue(inventory)
  api.estimateImport.mockResolvedValue({ messages: 3, bytes: 3072 })
  api.importTargets.mockResolvedValue([{ accountId: 4, email: 'wren@example.org', folders: ['INBOX', 'Archive'] }])
  api.startImportJob.mockResolvedValue(7)
  api.pauseImportJob.mockResolvedValue(undefined)
})

describe('GuidedImport', () => {
  it('walks from a source through the summary and destination to a report', async () => {
    const user = userEvent.setup()
    render(GuidedImport)
    await user.click(await screen.findByRole('button', { name: /A folder of mail/ }))
    expect(api.chooseImportSource).toHaveBeenCalledWith('folder')
    expect(api.scanImportSource).toHaveBeenCalledWith('folder', ['/home/wren/Mail'])

    const work = await screen.findByRole('checkbox', { name: 'Mail / Work' })
    expect(work).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /people/ })).toBeChecked()
    expect(screen.getByText(/Outlook data files cannot be read yet/)).toBeTruthy()
    await expectAxeClean()
    await user.click(work)

    await user.click(screen.getByRole('button', { name: 'Next' }))
    const upload = await screen.findByRole('radio', { name: /Upload it into one of your accounts/ })
    await waitFor(() => expect(upload).toBeEnabled())
    await user.click(upload)
    await user.selectOptions(screen.getByRole('combobox', { name: 'Account' }), '4')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Folder' }), 'Archive')
    expect(screen.getByText(/needs about that much free space/)).toBeTruthy()

    await user.click(screen.getByRole('button', { name: 'Start import' }))
    expect(api.startImportJob).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: 'folder',
        folders: ['maildir:/home/wren/Mail'],
        addressBooks: ['vcf:/home/wren/Mail/people.vcf'],
        destination: { mode: 'account', accountId: 4, folder: 'Archive' },
      }),
    )

    events.listener?.(job({}))
    const bar = await screen.findByRole('progressbar')
    await waitFor(() => expect(bar.getAttribute('aria-valuenow')).toBe('33'))
    expect(screen.getByRole('status').textContent).toContain('Importing mail: Mail/Work')
    await expectAxeClean()

    await user.click(screen.getByRole('button', { name: 'Pause' }))
    expect(api.pauseImportJob).toHaveBeenCalledWith(7)
    events.listener?.(job({ state: 'paused' }))
    expect(await screen.findByRole('button', { name: 'Resume' })).toBeTruthy()

    events.listener?.(
      job({
        state: 'done',
        report: {
          lines: [
            { kind: 'folder', name: 'Mail', imported: 1, skipped: 1, failed: 0, reasons: { duplicate: 1 } },
            { kind: 'contacts', name: 'people', imported: 2, skipped: 0, failed: 0 },
          ],
        },
      }),
    )
    expect(await screen.findByRole('heading', { name: 'Import finished' })).toBeTruthy()
    expect(screen.getByText('3 imported, 1 skipped, 0 failed')).toBeTruthy()
    expect(screen.getByText('1 already there')).toBeTruthy()
  })

  it('offers to resume an import that stopped', async () => {
    api.importJobs.mockResolvedValue([job({ state: 'paused', progress: { phase: 'mail', current: '', done: 40, total: 100, imported: 40, skipped: 0, failed: 0 } })])
    api.resumeImportJob.mockResolvedValue(undefined)
    const user = userEvent.setup()
    render(GuidedImport)
    expect(await screen.findByText('An import stopped after 40 of 100 messages.')).toBeTruthy()
    await user.click(screen.getByRole('button', { name: 'Resume' }))
    expect(api.resumeImportJob).toHaveBeenCalledWith(7)
    expect(await screen.findByRole('progressbar')).toBeTruthy()
  })
})
