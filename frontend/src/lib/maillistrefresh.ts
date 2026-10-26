import type { SyncPhase } from './syncstatus'
import type { Selection } from './types'

/** What shouldReplaceListOnMailNew needs to know about the list on screen. */
export interface MailNewListRefreshContext {
  syncPhase: SyncPhase
  // true when the user has loaded, or is loading, beyond the first page.
  paginated: boolean
}

/**
 * Whether mail:new should replace the list with a fresh first page. Body
 * download and OnStored batches reuse existing row ids, so reloading there
 * only resets scroll. When the user has paged ahead, merging new head rows
 * preserves the loaded window.
 */
export function shouldReplaceListOnMailNew(ctx: MailNewListRefreshContext): boolean {
  if (ctx.syncPhase === 'bodies') {
    return false
  }
  if (ctx.paginated) {
    return false
  }
  return true
}

/** What refreshListOnMailNew reads and calls; App passes the real ones. */
export interface MailNewListDeps {
  selection: () => Selection
  // the sync phase of one account.
  phase: (accountId: number) => SyncPhase
  paginated: () => boolean
  loadList: (sel: Selection) => unknown
  refreshListHead: (sel: Selection) => unknown
}

/**
 * Routes one mail:new to the list: a fresh first page, or a head merge that
 * keeps the loaded window. The phase is the sending account's: another
 * account may be listing stubs while this one fills bodies.
 */
export function refreshListOnMailNew(accountId: number, deps: MailNewListDeps): void {
  const sel = deps.selection()
  if (shouldReplaceListOnMailNew({ syncPhase: deps.phase(accountId), paginated: deps.paginated() })) {
    void deps.loadList(sel)
  } else {
    void deps.refreshListHead(sel)
  }
}
