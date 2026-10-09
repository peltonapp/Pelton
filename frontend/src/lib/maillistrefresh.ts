import type { SyncPhase } from './syncstatus'

/** What shouldReplaceListOnMailNew needs to know about the list on screen. */
export interface MailNewListRefreshContext {
  syncPhase: SyncPhase
  // true when the user has scrolled beyond the first page of cached rows.
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
