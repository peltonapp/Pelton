// newsletters.ts holds the newsletter manager's list and the actions on its
// senders. Unsubscribing reuses the reading pane's flow: a one-click POST or a
// mailto send through the backend, a plain link opened in the browser, and
// always a confirmation first. Archive and delete count the messages first,
// confirm with that count and offer an undo afterwards.

import { get, writable } from 'svelte/store'
import {
  archiveMessages,
  deleteMessages,
  getMessage,
  listNewsletters,
  newsletterMessageIds,
  restoreMessage,
  undoDeletes,
  unsubscribeMessage,
  type NewsletterSelection,
} from '../lib/api'
import { openURL } from '../lib/browser'
import { t } from '../lib/i18n'
import { senderLabel, type NewsletterSender } from '../lib/mailstats'
import { askConfirm } from './confirm'
import { closeMailScreen } from './mailscreen'
import { loadList, removeFromList } from './messages'
import { refreshCountsSoon } from './sidebarcounts'
import { selection } from './selection'
import { setDraft, submitSearch } from './search'
import { errorMessage, pushAction, toastError, toastInfo, toastSuccess } from './toast'

/** The manager's list: loading, the senders, or why they could not be read. */
export interface NewsletterState {
  loading: boolean
  senders: NewsletterSender[]
  error: string
}

/** The newsletter senders, reloaded when the screen opens and after actions. */
export const newsletters = writable<NewsletterState>({ loading: false, senders: [], error: '' })

/** Work in progress, for the pie loader: done of total, or null when idle. */
export const newsletterProgress = writable<{ done: number; total: number } | null>(null)

/** How long an undo stays offered, in ms. */
const undoMs = 10000

/** Reads the senders again. */
export async function loadNewsletters(): Promise<void> {
  newsletters.update((s) => ({ ...s, loading: true, error: '' }))
  try {
    const senders = await listNewsletters()
    newsletters.set({ loading: false, senders, error: '' })
  } catch (err) {
    newsletters.update((s) => ({ ...s, loading: false, error: errorMessage(err) }))
  }
}

function label(key: string, n?: number, name?: string): string {
  let text = get(t)(key)
  if (n !== undefined) {
    text = text.replace('{n}', String(n))
  }
  if (name !== undefined) {
    text = text.replace('{name}', name)
  }
  return text
}

function markDone(emails: string[]): void {
  const at = new Date().toISOString()
  newsletters.update((s) => ({
    ...s,
    senders: s.senders.map((x) => (emails.includes(x.email) ? { ...x, unsubscribedAt: at } : x)),
  }))
}

/**
 * Unsubscribes from the senders after a confirmation. One sender with only a
 * link opens it in the browser. In a multi-selection the link-only senders are
 * left for one at a time, since opening a page per sender is not something to
 * do in bulk.
 */
export async function unsubscribeSenders(senders: NewsletterSender[]): Promise<void> {
  const usable = senders.filter((s) => s.unsubscribeKind !== '' && !s.unsubscribedAt)
  if (usable.length === 0) {
    toastInfo(label('newsletters.unsubscribe.nothing'))
    return
  }
  if (usable.length === 1) {
    const s = usable[0]
    const link = s.unsubscribeKind === 'link'
    const ok = await askConfirm({
      title: label('newsletters.unsubscribe.confirmTitle', undefined, senderLabel(s)),
      body: label(`newsletters.unsubscribe.how.${s.unsubscribeKind}`),
      confirmLabel: link ? label('detail.unsubscribe.confirmOpen') : label('detail.unsubscribe.button'),
    })
    if (!ok) {
      return
    }
    try {
      if (link) {
        const detail = await getMessage(s.latestId)
        if (detail.unsubscribe?.kind === 'link') {
          openURL(detail.unsubscribe.target)
        }
        return
      }
      await unsubscribeMessage(s.latestId)
      markDone([s.email])
      toastSuccess(label('detail.unsubscribe.done'))
    } catch (err) {
      toastError(errorMessage(err))
    }
    return
  }

  const direct = usable.filter((s) => s.unsubscribeKind !== 'link')
  const links = usable.length - direct.length
  if (direct.length === 0) {
    toastInfo(label('newsletters.unsubscribe.linksOnly', links))
    return
  }
  const ok = await askConfirm({
    title: label('newsletters.unsubscribe.confirmMany', direct.length),
    body: label('newsletters.unsubscribe.howMany'),
    confirmLabel: label('detail.unsubscribe.button'),
  })
  if (!ok) {
    return
  }
  const done: string[] = []
  let failure = ''
  newsletterProgress.set({ done: 0, total: direct.length })
  for (const s of direct) {
    try {
      await unsubscribeMessage(s.latestId)
      done.push(s.email)
    } catch (err) {
      failure = errorMessage(err)
    }
    newsletterProgress.update((p) => (p ? { ...p, done: p.done + 1 } : p))
  }
  newsletterProgress.set(null)
  markDone(done)
  if (failure) {
    toastError(failure)
  } else {
    toastSuccess(label('newsletters.unsubscribe.doneMany', done.length))
  }
  if (links > 0) {
    toastInfo(label('newsletters.unsubscribe.linksLeft', links))
  }
}

function selectionOf(senders: NewsletterSender[], extra: Partial<NewsletterSelection>): NewsletterSelection {
  return { emails: senders.map((s) => s.email), olderThanDays: 0, keepLatest: false, forArchive: false, ...extra }
}

/** Archives all newsletter mail of the senders, with an undo. */
export async function archiveSenders(senders: NewsletterSender[]): Promise<void> {
  const ids = await countOrSay(selectionOf(senders, { forArchive: true }))
  if (ids === null) {
    return
  }
  const ok = await askConfirm({
    title: label('newsletters.archive.confirmTitle', ids.length),
    body: label('newsletters.archive.confirmBody'),
    confirmLabel: label('newsletters.archive.confirmAction', ids.length),
  })
  if (!ok) {
    return
  }
  newsletterProgress.set({ done: 0, total: ids.length })
  try {
    const res = await archiveMessages(ids)
    for (const id of res.done) {
      removeFromList(id)
    }
    if (res.failed > 0) {
      toastError(`${label('newsletters.partial', res.failed)} ${res.firstError}`)
    }
    const undoable = res.undo.filter((u) => u.messageId !== '')
    if (res.done.length > 0) {
      pushAction(
        'success',
        label('newsletters.archive.done', res.done.length),
        { label: label('newsletters.undo'), run: () => void undoArchive(undoable) },
        undoMs,
      )
    }
  } catch (err) {
    toastError(errorMessage(err))
  } finally {
    newsletterProgress.set(null)
  }
  refreshCountsSoon()
  await loadNewsletters()
}

async function undoArchive(undo: { messageId: string; originalFolderId: number; destFolderId: number }[]): Promise<void> {
  let failure = ''
  newsletterProgress.set({ done: 0, total: undo.length })
  for (const u of undo) {
    try {
      await restoreMessage(u.messageId, u.destFolderId, u.originalFolderId)
    } catch (err) {
      failure = errorMessage(err)
    }
    newsletterProgress.update((p) => (p ? { ...p, done: p.done + 1 } : p))
  }
  newsletterProgress.set(null)
  if (failure) {
    toastError(failure)
  } else {
    toastInfo(label('undo.archiveDoneCount', undo.length))
  }
  await afterRestore()
}

/** Deletes the senders' newsletter mail older than the given number of days. */
export async function deleteOlderThan(senders: NewsletterSender[], days: number): Promise<void> {
  await deleteSelected(selectionOf(senders, { olderThanDays: days }), 'newsletters.delete.olderTitle', days)
}

/** Deletes all but each sender's newest newsletter. */
export async function keepLatestOnly(senders: NewsletterSender[]): Promise<void> {
  await deleteSelected(selectionOf(senders, { keepLatest: true }), 'newsletters.delete.keepTitle')
}

async function deleteSelected(sel: NewsletterSelection, titleKey: string, days?: number): Promise<void> {
  const ids = await countOrSay(sel)
  if (ids === null) {
    return
  }
  const title = label(titleKey, ids.length).replace('{days}', String(days ?? ''))
  const ok = await askConfirm({
    title,
    body: label('newsletters.delete.confirmBody'),
    confirmLabel: label('messageList.bulk.deleteConfirmAction', ids.length),
    danger: true,
  })
  if (!ok) {
    return
  }
  try {
    const res = await deleteMessages(ids)
    for (const id of res.done) {
      removeFromList(id)
    }
    if (res.failed > 0) {
      toastError(`${label('newsletters.partial', res.failed)} ${res.firstError}`)
    }
    if (res.done.length > 0) {
      pushAction(
        'success',
        label('newsletters.delete.done', res.done.length),
        { label: label('newsletters.undo'), run: () => void undoDelete(res.done) },
        undoMs,
      )
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
  refreshCountsSoon()
  await loadNewsletters()
}

async function undoDelete(ids: number[]): Promise<void> {
  try {
    const res = await undoDeletes(ids)
    if (res.failed > 0) {
      toastError(res.firstError)
    } else {
      toastInfo(label('undo.deleteDoneCount', res.done.length))
    }
  } catch (err) {
    toastError(errorMessage(err))
  }
  await afterRestore()
}

async function afterRestore(): Promise<void> {
  refreshCountsSoon()
  await loadList(get(selection))
  await loadNewsletters()
}

// countOrSay fetches the ids an action would touch, or says there are none.
async function countOrSay(sel: NewsletterSelection): Promise<number[] | null> {
  try {
    const ids = await newsletterMessageIds(sel)
    if (ids.length === 0) {
      toastInfo(label('newsletters.nothingToDo'))
      return null
    }
    return ids
  } catch (err) {
    toastError(errorMessage(err))
    return null
  }
}

/** Closes the manager and searches the sender's mail in the top bar. */
export function showSenderMessages(sender: NewsletterSender): void {
  closeMailScreen()
  setDraft({ text: '', chips: [{ field: 'from', value: sender.email, label: senderLabel(sender) }] })
  submitSearch()
}
