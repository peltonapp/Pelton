import { describe, expect, it } from 'vitest'
import {
  buildPlan,
  defaultSelection,
  folderRows,
  isEmptyPlan,
  progressPercent,
  reportTotals,
  type ImportInventory,
} from './guidedimport'

function inventory(): ImportInventory {
  return {
    kind: 'thunderbird',
    paths: ['/profiles/abc.default'],
    accounts: [
      { email: 'wren@example.org', displayName: 'Wren', imapHost: 'imap.example.org', imapPort: 993, smtpHost: '', smtpPort: 0, kind: 'imap', signInWithProvider: false, exists: false },
      { email: 'juno@example.net', displayName: '', imapHost: 'pop.example.net', imapPort: 995, smtpHost: '', smtpPort: 0, kind: 'pop3', signInWithProvider: false, exists: false },
      { email: 'old@example.com', displayName: '', imapHost: 'imap.example.com', imapPort: 993, smtpHost: '', smtpPort: 0, kind: 'imap', signInWithProvider: false, exists: true },
    ],
    folders: [
      { key: 'mbox:/a/Inbox', path: ['Local Folders', 'Inbox'], count: 3, sizeBytes: 10, serverCopy: false },
      { key: 'mbox:/a/2019', path: ['Local Folders', 'Archive', '2019'], count: 1, sizeBytes: 5, serverCopy: false },
      { key: 'mbox:/b/INBOX', path: ['wren@example.org', 'INBOX'], count: 9, sizeBytes: 50, serverCopy: true },
      { key: 'mbox:/a/Trash', path: ['Local Folders', 'Trash'], count: 0, sizeBytes: 0, serverCopy: false },
    ],
    addressBooks: [{ key: 'tbabook:/a', name: 'Personal', count: 2 }],
    calendars: [],
    filters: [
      { key: 'f1', name: 'Receipts', account: 'wren@example.org', enabled: true, mappable: true, unmappable: [] },
      { key: 'f2', name: 'Tags', account: 'wren@example.org', enabled: true, mappable: false, unmappable: ['AddTag: $label1'] },
    ],
    notices: [],
  }
}

describe('guided import', () => {
  it('picks what can come over and leaves server copies and empty folders out', () => {
    const sel = defaultSelection(inventory())
    expect([...sel.accounts]).toEqual(['wren@example.org'])
    expect([...sel.folders]).toEqual(['mbox:/a/Inbox', 'mbox:/a/2019'])
    expect([...sel.filters]).toEqual(['f1'])
    expect([...sel.addressBooks]).toEqual(['tbabook:/a'])
  })

  it('builds a plan from the picks without unaddable accounts or unmappable filters', () => {
    const inv = inventory()
    const sel = defaultSelection(inv)
    sel.accounts.add('juno@example.net')
    sel.filters.add('f2')
    sel.folders.add('mbox:/b/INBOX')
    const plan = buildPlan(inv, sel, { mode: 'account', accountId: 4, folder: 'Archive' })
    expect(plan.accounts).toEqual(['wren@example.org'])
    expect(plan.filters).toEqual(['f1'])
    expect(plan.folders).toEqual(['mbox:/a/Inbox', 'mbox:/a/2019', 'mbox:/b/INBOX'])
    expect(plan.destination).toEqual({ mode: 'account', accountId: 4, folder: 'Archive' })
    expect(buildPlan(inv, sel, { mode: 'local', accountId: 4, folder: 'x' }).destination).toEqual({ mode: 'local', accountId: 0, folder: '' })
    expect(isEmptyPlan(plan)).toBe(false)
    const none = { accounts: new Set<string>(), folders: new Set<string>(), addressBooks: new Set<string>(), calendars: new Set<string>(), filters: new Set<string>() }
    expect(isEmptyPlan(buildPlan(inv, none, { mode: 'local', accountId: 0, folder: '' }))).toBe(true)
  })

  it('orders folders as a tree and indents them', () => {
    const rows = folderRows(inventory().folders)
    expect(rows.map((r) => r.folder.path.join('/'))).toEqual([
      'Local Folders/Archive/2019',
      'Local Folders/Inbox',
      'Local Folders/Trash',
      'wren@example.org/INBOX',
    ])
    expect(rows[0].depth).toBe(2)
    expect(rows[0].label).toBe('2019')
    expect(folderRows([{ key: 'k', path: [], count: 1, sizeBytes: 1, serverCopy: false }])[0].label).toBe('')
  })

  it('reports progress and totals', () => {
    const progress = { phase: 'mail', current: 'Inbox', done: 5, total: 20, imported: 4, skipped: 1, failed: 0 }
    expect(progressPercent({ state: 'running', progress })).toBe(25)
    expect(progressPercent({ state: 'done', progress })).toBe(100)
    expect(progressPercent({ state: 'running', progress: { ...progress, total: 0 } })).toBe(0)
    expect(
      reportTotals([
        { kind: 'folder', name: 'a', imported: 2, skipped: 1, failed: 0 },
        { kind: 'contacts', name: 'b', imported: 3, skipped: 0, failed: 1 },
      ]),
    ).toEqual({ imported: 5, skipped: 1, failed: 1 })
  })
})
