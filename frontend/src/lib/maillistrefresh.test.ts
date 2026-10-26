import { describe, expect, it, vi } from 'vitest'
import { refreshListOnMailNew, shouldReplaceListOnMailNew } from './maillistrefresh'
import type { SyncPhase } from './syncstatus'

// Body sync keeps the list; stub sync (or none) reloads only the first page and
// merges once the user has paged ahead.
describe('shouldReplaceListOnMailNew', () => {
  it.each<{ syncPhase: SyncPhase; paginated: boolean; replace: boolean }>([
    { syncPhase: 'bodies', paginated: false, replace: false },
    { syncPhase: 'bodies', paginated: true, replace: false },
    { syncPhase: 'stubs', paginated: false, replace: true },
    { syncPhase: '', paginated: false, replace: true },
    { syncPhase: 'stubs', paginated: true, replace: false },
    { syncPhase: '', paginated: true, replace: false },
  ])('phase "$syncPhase", paginated $paginated → replace $replace', ({ syncPhase, paginated, replace }) => {
    expect(shouldReplaceListOnMailNew({ syncPhase, paginated })).toBe(replace)
  })
})

// App's mail:new handler: which of the two list refreshes one event runs.
describe('refreshListOnMailNew', () => {
  const inbox = { kind: 'folder', folderId: 1, accountId: 1, label: 'INBOX' } as const

  function deps(phases: Record<number, SyncPhase>, paginated: boolean) {
    return {
      selection: () => inbox,
      phase: (id: number) => phases[id] ?? '',
      paginated: () => paginated,
      loadList: vi.fn(),
      refreshListHead: vi.fn(),
    }
  }

  it('reloads the first page of a list that is not paged ahead', () => {
    const d = deps({}, false)
    refreshListOnMailNew(1, d)
    expect(d.loadList).toHaveBeenCalledExactlyOnceWith(inbox)
    expect(d.refreshListHead).not.toHaveBeenCalled()
  })

  it('merges the head into a paged list', () => {
    const d = deps({}, true)
    refreshListOnMailNew(1, d)
    expect(d.refreshListHead).toHaveBeenCalledExactlyOnceWith(inbox)
    expect(d.loadList).not.toHaveBeenCalled()
  })

  it('merges the head while the sending account fills bodies', () => {
    const d = deps({ 1: 'bodies' }, false)
    refreshListOnMailNew(1, d)
    expect(d.refreshListHead).toHaveBeenCalledExactlyOnceWith(inbox)
    expect(d.loadList).not.toHaveBeenCalled()
  })

  it("reads the sending account's phase, not another account's", () => {
    const d = deps({ 1: 'stubs', 2: 'bodies' }, false)
    refreshListOnMailNew(1, d)
    expect(d.loadList).toHaveBeenCalledOnce()

    const e = deps({ 1: 'stubs', 2: 'bodies' }, false)
    refreshListOnMailNew(2, e)
    expect(e.refreshListHead).toHaveBeenCalledOnce()
  })
})
