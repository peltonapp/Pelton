// guidedimport.ts holds the types of the guided import (bind_migrate.go) and
// the pure steps the flow takes between them: what is picked by default, the
// plan built from the picks, and how a folder list becomes a tree.

/** The sources the guided import reads from; "folder" is a picked folder of mail files. */
export type ImportSourceKind = 'thunderbird' | 'applemail' | 'files' | 'folder' | 'pst'

/** An account found in a source: settings only, never a password. */
export interface ImportAccount {
  email: string
  displayName: string
  imapHost: string
  imapPort: number
  smtpHost: string
  smtpPort: number
  kind: string
  signInWithProvider: boolean
  exists: boolean
}

/** A mail folder found in a source. */
export interface ImportFolder {
  key: string
  path: string[]
  count: number
  sizeBytes: number
  serverCopy: boolean
}

/** An address book or calendar found in a source. */
export interface ImportPart {
  key: string
  name: string
  count: number
}

/** A message filter found in a source, with what of it has no Pelton counterpart. */
export interface ImportFilter {
  key: string
  name: string
  account: string
  enabled: boolean
  mappable: boolean
  unmappable: string[]
}

/** Something found in a source that will not come over. */
export interface ImportNotice {
  code: string
  name: string
}

/** What a scanned source holds. */
export interface ImportInventory {
  kind: string
  paths: string[]
  accounts: ImportAccount[]
  folders: ImportFolder[]
  addressBooks: ImportPart[]
  calendars: ImportPart[]
  filters: ImportFilter[]
  notices: ImportNotice[]
}

/** Where mail goes: kept here, or uploaded into an account's folder. */
export interface ImportDestination {
  mode: 'local' | 'account'
  accountId: number
  folder: string
}

/** What the user picked, as the backend runs it. */
export interface ImportPlan {
  kind: string
  paths: string[]
  accounts: string[]
  folders: string[]
  addressBooks: string[]
  calendars: string[]
  filters: string[]
  destination: ImportDestination
}

/** Messages and their size, which is about the space an upload needs. */
export interface ImportEstimate {
  messages: number
  bytes: number
}

/** An account mail can be uploaded into. */
export interface ImportTarget {
  accountId: number
  email: string
  folders: string[]
}

/** Where a running import is. */
export interface ImportProgress {
  phase: string
  current: string
  done: number
  total: number
  imported: number
  skipped: number
  failed: number
}

/** One part of the source in the final report. */
export interface ImportReportLine {
  kind: 'account' | 'folder' | 'contacts' | 'calendar' | 'filter' | 'notice'
  name: string
  imported: number
  skipped: number
  failed: number
  reasons?: Record<string, number>
  details?: string[]
}

/** The states a job moves through. */
export type ImportJobState = 'running' | 'paused' | 'cancelled' | 'done' | 'failed'

/** One guided import and where it stands. */
export interface ImportJob {
  id: number
  kind: string
  sourcePath: string
  state: ImportJobState
  plan: ImportPlan
  progress: ImportProgress
  report: { lines: ImportReportLine[] }
  error: string
  createdAt: string
  updatedAt: string
}

/** The picks the summary step holds, by key or address. */
export interface ImportSelection {
  accounts: Set<string>
  folders: Set<string>
  addressBooks: Set<string>
  calendars: Set<string>
  filters: Set<string>
}

/** canImportAccount reports whether an account can be added: imap and not here yet. */
export function canImportAccount(a: ImportAccount): boolean {
  return a.kind === 'imap' && !a.exists
}

/**
 * defaultSelection picks what most people want: every account that can be
 * added, every folder that is not a cached copy of server mail (adding the
 * account brings that back), every address book and calendar, and every
 * filter that maps onto an automation.
 */
export function defaultSelection(inv: ImportInventory): ImportSelection {
  return {
    accounts: new Set(inv.accounts.filter(canImportAccount).map((a) => a.email)),
    folders: new Set(inv.folders.filter((f) => !f.serverCopy && f.count > 0).map((f) => f.key)),
    addressBooks: new Set(inv.addressBooks.map((b) => b.key)),
    calendars: new Set(inv.calendars.map((c) => c.key)),
    filters: new Set(inv.filters.filter((f) => f.mappable).map((f) => f.key)),
  }
}

/** buildPlan turns the picks and the destination into the plan the backend runs. */
export function buildPlan(inv: ImportInventory, sel: ImportSelection, destination: ImportDestination): ImportPlan {
  const keep = <T>(items: T[], key: (item: T) => string, picked: Set<string>) =>
    items.map(key).filter((k) => picked.has(k))
  return {
    kind: inv.kind,
    paths: inv.paths,
    accounts: keep(inv.accounts.filter(canImportAccount), (a) => a.email, sel.accounts),
    folders: keep(inv.folders, (f) => f.key, sel.folders),
    addressBooks: keep(inv.addressBooks, (b) => b.key, sel.addressBooks),
    calendars: keep(inv.calendars, (c) => c.key, sel.calendars),
    filters: keep(inv.filters.filter((f) => f.mappable), (f) => f.key, sel.filters),
    destination: destination.mode === 'local' ? { mode: 'local', accountId: 0, folder: '' } : destination,
  }
}

/** isEmptyPlan reports whether a plan brings nothing over. */
export function isEmptyPlan(p: ImportPlan): boolean {
  return [p.accounts, p.folders, p.addressBooks, p.calendars, p.filters].every((list) => list.length === 0)
}

/** A folder row as the summary shows it: indented under its parents. */
export interface FolderRow {
  folder: ImportFolder
  depth: number
  label: string
}

/**
 * folderRows sorts folders into tree order and indents each one by how many
 * of its parents are listed too, so a nested folder sits under its parent
 * even when the parent holds no mail of its own.
 */
export function folderRows(folders: ImportFolder[]): FolderRow[] {
  const sorted = [...folders].sort((a, b) => {
    const n = Math.min(a.path.length, b.path.length)
    for (let i = 0; i < n; i++) {
      const c = a.path[i].localeCompare(b.path[i])
      if (c !== 0) {
        return c
      }
    }
    return a.path.length - b.path.length
  })
  return sorted.map((folder) => ({
    folder,
    depth: Math.max(0, folder.path.length - 1),
    label: folder.path.length > 0 ? folder.path[folder.path.length - 1] : '',
  }))
}

/** progressPercent is how far a run is, 0 to 100, 100 once it is done. */
export function progressPercent(job: Pick<ImportJob, 'state' | 'progress'>): number {
  if (job.state === 'done') {
    return 100
  }
  const { done, total } = job.progress
  return total > 0 ? Math.min(100, Math.round((done / total) * 100)) : 0
}

/** reportTotals adds up a report's lines. */
export function reportTotals(lines: ImportReportLine[]): { imported: number; skipped: number; failed: number } {
  return lines.reduce(
    (sum, l) => ({ imported: sum.imported + l.imported, skipped: sum.skipped + l.skipped, failed: sum.failed + l.failed }),
    { imported: 0, skipped: 0, failed: 0 },
  )
}
