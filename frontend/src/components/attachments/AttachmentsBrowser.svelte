<script lang="ts">
  // the attachments browser of the new design: every file across the mail of
  // this profile, newest first, in place of the list and the reader. A filter
  // bar narrows it, the files show as rows or as a grid of thumbnails, and the
  // one in focus is previewed beside them with what can be done with it. The
  // list holds only the pages that were scrolled to and renders only what is
  // on screen, so a store of tens of thousands of files opens as fast as a
  // small one.
  import { onDestroy, tick } from 'svelte'
  import listIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import gridIcon from '@phosphor-icons/core/assets/duotone/squares-four-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import cloudIcon from '@phosphor-icons/core/assets/duotone/cloud-arrow-down-duotone.svg?raw'
  import saveIcon from '@phosphor-icons/core/assets/duotone/download-simple-duotone.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import Spinner from '../common/Spinner.svelte'
  import AttachmentInspector from './AttachmentInspector.svelte'
  import AttachmentTile from './AttachmentTile.svelte'
  import { kindIcon, kindOrder, kindLabelKey, asAttachment } from './kinds'
  import { openPreview } from '../../stores/preview'
  import { queryAttachments, saveAttachmentsTo } from '../../lib/api'
  import { formatBytes, formatListDate, accountLabel, type TimeFormat } from '../../lib/format'
  import { attachmentFilter, attachmentLayout, closeAttachments, emptyAttachmentFilter } from '../../stores/attachments'
  import { sidebar } from '../../stores/accounts'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { AttachmentEntry, AttachmentFilter, AttachmentKind } from '../../lib/types'

  const pageSize = 200
  const overscan = 6
  const typingDelay = 250
  // below this width, in css pixels, the preview moves under the files.
  const narrowWidth = 760

  let browserWidth = 0
  $: narrow = browserWidth > 0 && browserWidth < narrowWidth

  // the filter the list was loaded for; the fields edit a copy so typing can
  // wait for a pause before it asks again.
  let draft: AttachmentFilter = { ...$attachmentFilter }
  let typingTimer: ReturnType<typeof setTimeout> | undefined
  // a filter set from outside (the sender panel's "all files") shows in the
  // fields too.
  $: draft = { ...$attachmentFilter }

  function commit(next: AttachmentFilter, wait = false): void {
    draft = next
    clearTimeout(typingTimer)
    if (wait) {
      typingTimer = setTimeout(() => attachmentFilter.set({ ...draft }), typingDelay)
    } else {
      attachmentFilter.set({ ...draft })
    }
  }

  function toggleKind(kind: AttachmentKind): void {
    const kinds = draft.kinds.includes(kind) ? draft.kinds.filter((k) => k !== kind) : [...draft.kinds, kind]
    commit({ ...draft, kinds })
  }

  function clearFilters(): void {
    commit(emptyAttachmentFilter())
  }

  $: filtering = JSON.stringify($attachmentFilter) !== JSON.stringify(emptyAttachmentFilter())

  $: mailboxItems = [
    { value: '0', label: $t('attachments.filter.allMailboxes') },
    ...($sidebar.data?.accounts ?? []).map((a) => ({ value: String(a.id), label: accountLabel(a) })),
  ]

  // the loaded pages, by page number, and the total for the current filter.
  let total = -1
  let pages = new Map<number, AttachmentEntry[]>()
  const loadingPages = new Set<number>()
  let generation = 0
  let loadError = ''

  $: if ($attachmentFilter) {
    reload()
  }

  function reload(): void {
    generation += 1
    total = -1
    pages = new Map()
    loadingPages.clear()
    loadError = ''
    selected = new Set()
    anchorIndex = -1
    activeIndex = -1
    if (scroller) {
      scroller.scrollTop = 0
      scrollTop = 0
    }
    void loadPage(0)
  }

  async function loadPage(page: number): Promise<void> {
    if (pages.has(page) || loadingPages.has(page)) {
      return
    }
    const gen = generation
    loadingPages.add(page)
    try {
      const result = await queryAttachments({
        ...$attachmentFilter,
        senderAddress: '',
        limit: pageSize,
        offset: page * pageSize,
        withTotal: page === 0,
      })
      if (gen !== generation) {
        return
      }
      if (result.total >= 0) {
        total = result.total
      }
      pages = new Map(pages).set(page, result.entries)
      if (page === 0 && activeIndex < 0 && result.entries.length > 0) {
        activeIndex = 0
      }
    } catch (err) {
      if (gen === generation) {
        loadError = errorMessage(err)
      }
    } finally {
      loadingPages.delete(page)
    }
  }

  function entryAt(index: number, loaded: Map<number, AttachmentEntry[]>): AttachmentEntry | undefined {
    return loaded.get(Math.floor(index / pageSize))?.[index % pageSize]
  }

  // an entry changed by a fetch is written back where it sits.
  function replaceEntry(next: AttachmentEntry): void {
    for (const [n, list] of pages) {
      const i = list.findIndex((e) => e.id === next.id)
      if (i >= 0) {
        const copy = list.slice()
        copy[i] = next
        pages = new Map(pages).set(n, copy)
        return
      }
    }
  }

  // virtual window. Rows are one height; the grid is rows of tiles, as many
  // across as fit.
  let scroller: HTMLDivElement | undefined
  let scrollTop = 0
  let viewportHeight = 0
  let viewportWidth = 0
  let rowHeight = 0
  let tileWidth = 0

  $: grid = $attachmentLayout === 'grid'
  $: count = Math.max(total, 0)
  $: perRow = grid ? Math.max(1, Math.floor(viewportWidth / Math.max(tileWidth, 1))) : 1
  $: rowsTotal = Math.ceil(count / perRow)
  $: estRow = rowHeight || 40
  $: firstRow = Math.max(0, Math.floor(scrollTop / estRow) - overscan)
  $: lastRow = Math.min(rowsTotal, Math.ceil((scrollTop + viewportHeight) / estRow) + overscan)
  $: windowRows = range(firstRow, lastRow)
  $: topPad = firstRow * estRow
  $: bottomPad = Math.max(0, (rowsTotal - lastRow) * estRow)

  // the pages the window touches are asked for as it reaches them.
  $: if (total > 0) {
    const from = Math.floor((firstRow * perRow) / pageSize)
    const to = Math.floor((Math.min(count, lastRow * perRow) - 1) / pageSize)
    for (let p = from; p <= to; p++) {
      void loadPage(p)
    }
  }

  function range(from: number, to: number): number[] {
    const out: number[] = []
    for (let i = from; i < to; i++) {
      out.push(i)
    }
    return out
  }

  function cells(row: number): number[] {
    const start = row * perRow
    return range(start, Math.min(count, start + perRow))
  }

  // the first rendered row sets the height every row is assumed to have.
  function measure(node: HTMLElement): void {
    const read = (): void => {
      if (node.offsetHeight > 0) {
        rowHeight = node.offsetHeight
      }
    }
    read()
  }

  function measureTile(node: HTMLElement): void {
    if (node.offsetWidth > 0) {
      tileWidth = node.offsetWidth
    }
  }

  $: if ($attachmentLayout) {
    rowHeight = 0
    tileWidth = 0
  }

  // selection: one file in focus, any number marked for saving.
  let activeIndex = -1
  let anchorIndex = -1
  let selected = new Set<number>()

  $: active = activeIndex >= 0 ? entryAt(activeIndex, pages) : undefined
  $: selectedEntries = collectSelected(selected, pages)

  function collectSelected(ids: Set<number>, loaded: Map<number, AttachmentEntry[]>): AttachmentEntry[] {
    const out: AttachmentEntry[] = []
    for (const list of loaded.values()) {
      for (const e of list) {
        if (ids.has(e.id)) {
          out.push(e)
        }
      }
    }
    return out
  }

  function choose(index: number, event: MouseEvent | KeyboardEvent): void {
    const entry = entryAt(index, pages)
    if (!entry) {
      return
    }
    const toggle = event.metaKey || event.ctrlKey
    if (event.shiftKey && anchorIndex >= 0) {
      const next = new Set(selected)
      const [from, to] = anchorIndex < index ? [anchorIndex, index] : [index, anchorIndex]
      for (let i = from; i <= to; i++) {
        const e = entryAt(i, pages)
        if (e) {
          next.add(e.id)
        }
      }
      selected = next
    } else if (toggle) {
      const next = new Set(selected)
      if (next.has(entry.id)) {
        next.delete(entry.id)
      } else {
        next.add(entry.id)
      }
      selected = next
      anchorIndex = index
    } else {
      selected = new Set([entry.id])
      anchorIndex = index
    }
    activeIndex = index
  }

  function toggleMark(index: number): void {
    const entry = entryAt(index, pages)
    if (!entry) {
      return
    }
    const next = new Set(selected)
    if (next.has(entry.id)) {
      next.delete(entry.id)
    } else {
      next.add(entry.id)
    }
    selected = next
    anchorIndex = index
  }

  async function onKeydown(event: KeyboardEvent): Promise<void> {
    if (count === 0) {
      return
    }
    const step = grid ? perRow : 1
    let next = activeIndex
    switch (event.key) {
      case 'ArrowDown':
        next = Math.min(count - 1, activeIndex + step)
        break
      case 'ArrowUp':
        next = Math.max(0, activeIndex - step)
        break
      case 'ArrowRight':
        if (!grid) {
          return
        }
        next = Math.min(count - 1, activeIndex + (document.dir === 'rtl' ? -1 : 1))
        break
      case 'ArrowLeft':
        if (!grid) {
          return
        }
        next = Math.max(0, activeIndex + (document.dir === 'rtl' ? 1 : -1))
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = count - 1
        break
      case ' ':
        event.preventDefault()
        toggleMark(Math.max(activeIndex, 0))
        return
      case 'a':
        if (event.metaKey || event.ctrlKey) {
          event.preventDefault()
          selected = new Set(collectAll())
        }
        return
      case 'Escape':
        if (selected.size > 1) {
          event.preventDefault()
          event.stopPropagation()
          selected = active ? new Set([active.id]) : new Set()
        }
        return
      default:
        return
    }
    event.preventDefault()
    next = Math.max(0, next)
    if (event.shiftKey) {
      if (anchorIndex < 0) {
        anchorIndex = Math.max(activeIndex, 0)
      }
      choose(next, event)
    } else {
      activeIndex = next
      anchorIndex = next
      const e = entryAt(next, pages)
      selected = e ? new Set([e.id]) : new Set()
    }
    await tick()
    reveal(next)
  }

  function collectAll(): number[] {
    const ids: number[] = []
    for (const list of pages.values()) {
      ids.push(...list.map((e) => e.id))
    }
    return ids
  }

  function reveal(index: number): void {
    if (!scroller) {
      return
    }
    const top = Math.floor(index / perRow) * estRow
    if (top < scroller.scrollTop) {
      scroller.scrollTop = top
    } else if (top + estRow > scroller.scrollTop + scroller.clientHeight) {
      scroller.scrollTop = top + estRow - scroller.clientHeight
    }
  }

  let savingSelected = false

  async function saveSelected(): Promise<void> {
    if (savingSelected || selectedEntries.length === 0) {
      return
    }
    savingSelected = true
    try {
      const dir = await saveAttachmentsTo(selectedEntries.map((e) => ({ messageId: e.messageId, attachmentId: e.id })))
      if (dir) {
        toastSuccess($t('attachments.savedCount').replace('{count}', String(selectedEntries.length)))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      savingSelected = false
    }
  }

  function senderOf(e: AttachmentEntry): string {
    return e.fromName || e.fromAddress
  }

  onDestroy(() => clearTimeout(typingTimer))
</script>

<section class="browser" class:narrow aria-label={$t('attachments.title')} bind:clientWidth={browserWidth}>
  <header class="head">
    <h1>{$t('attachments.title')}</h1>
    {#if total >= 0}
      <span class="count">{$t('attachments.count').replace('{count}', total.toLocaleString())}</span>
    {/if}
    <span class="spacer"></span>
    <div class="layout" role="radiogroup" aria-label={$t('attachments.layout')}>
      <button
        type="button"
        class="tool"
        role="radio"
        aria-checked={!grid}
        aria-label={$t('attachments.layout.list')}
        title={$t('attachments.layout.list')}
        on:click={() => attachmentLayout.set('list')}
      >
        <Ph svg={listIcon} size={15} />
      </button>
      <button
        type="button"
        class="tool"
        role="radio"
        aria-checked={grid}
        aria-label={$t('attachments.layout.grid')}
        title={$t('attachments.layout.grid')}
        on:click={() => attachmentLayout.set('grid')}
      >
        <Ph svg={gridIcon} size={15} />
      </button>
    </div>
    <button type="button" class="tool" aria-label={$t('attachments.close')} title={$t('attachments.close')} on:click={closeAttachments}>
      <Ph svg={closeIcon} size={15} />
    </button>
  </header>

  <div class="filters" role="search" aria-label={$t('attachments.filters')}>
    <div class="kinds" role="group" aria-label={$t('attachments.filter.type')}>
      {#each kindOrder as kind (kind)}
        <button
          type="button"
          class="kind"
          aria-pressed={draft.kinds.includes(kind)}
          on:click={() => toggleKind(kind)}
        >
          <Ph svg={kindIcon[kind]} size={14} />
          {$t(kindLabelKey[kind])}
        </button>
      {/each}
    </div>
    <div class="fields">
      <label class="field name">
        <Ph svg={searchIcon} size={14} />
        <input
          type="search"
          placeholder={$t('attachments.filter.name')}
          aria-label={$t('attachments.filter.name')}
          value={draft.name}
          on:input={(e) => commit({ ...draft, name: e.currentTarget.value }, true)}
        />
      </label>
      <input
        class="field"
        type="search"
        placeholder={$t('attachments.filter.sender')}
        aria-label={$t('attachments.filter.sender')}
        value={draft.sender}
        on:input={(e) => commit({ ...draft, sender: e.currentTarget.value }, true)}
      />
      <div class="mailbox">
        <Select
          value={String(draft.accountId)}
          ariaLabel={$t('attachments.filter.mailbox')}
          items={mailboxItems}
          on:change={(e) => commit({ ...draft, accountId: Number(e.detail) })}
        />
      </div>
      <label class="date">
        <span>{$t('attachments.filter.from')}</span>
        <input
          class="field"
          type="date"
          value={draft.since}
          max={draft.until || undefined}
          on:change={(e) => commit({ ...draft, since: e.currentTarget.value })}
        />
      </label>
      <label class="date">
        <span>{$t('attachments.filter.to')}</span>
        <input
          class="field"
          type="date"
          value={draft.until}
          min={draft.since || undefined}
          on:change={(e) => commit({ ...draft, until: e.currentTarget.value })}
        />
      </label>
      {#if filtering}
        <button type="button" class="clear" on:click={clearFilters}>{$t('attachments.filter.clear')}</button>
      {/if}
    </div>
    <p class="rule-note">{$t('attachments.excludedNote')}</p>
  </div>

  <div class="main">
    <div class="results-col">
      {#if loadError}
        <div class="state" role="alert">{loadError}</div>
      {:else if total < 0}
        <div class="state"><Spinner label={$t('attachments.loading')} /></div>
      {:else if total === 0}
        <div class="state">
          <p>{filtering ? $t('attachments.emptyFiltered') : $t('attachments.empty')}</p>
          {#if filtering}
            <button type="button" class="clear" on:click={clearFilters}>{$t('attachments.filter.clear')}</button>
          {/if}
        </div>
      {:else}
        <div
          class="results"
          class:grid
          role="listbox"
          aria-label={$t('attachments.title')}
          aria-multiselectable="true"
          aria-activedescendant={active ? `att-${active.id}` : undefined}
          tabindex="0"
          bind:this={scroller}
          bind:clientHeight={viewportHeight}
          bind:clientWidth={viewportWidth}
          on:scroll={() => (scrollTop = scroller?.scrollTop ?? 0)}
          on:keydown={onKeydown}
        >
          <div style={`height:${topPad}px`} aria-hidden="true"></div>
          {#each windowRows as row (row)}
            {#if grid}
              <div class="tile-row" use:measure>
                {#each cells(row) as index (index)}
                  {@const entry = entryAt(index, pages)}
                  {#if entry}
                    <div class="tile-slot" use:measureTile>
                      <AttachmentTile
                        {entry}
                        selected={selected.has(entry.id)}
                        active={index === activeIndex}
                        on:choose={(e) => choose(index, e.detail)}
                        on:open={() => openPreview(entry.messageId, asAttachment(entry))}
                      />
                    </div>
                  {:else}
                    <div class="tile-slot placeholder" use:measureTile aria-hidden="true"></div>
                  {/if}
                {/each}
              </div>
            {:else}
              {@const entry = entryAt(row, pages)}
              {#if entry}
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <div
                  id={`att-${entry.id}`}
                  class="row"
                  class:active={row === activeIndex}
                  role="option"
                  aria-selected={selected.has(entry.id)}
                  tabindex="-1"
                  use:measure
                  on:click={(e) => choose(row, e)}
                  on:dblclick={() => openPreview(entry.messageId, asAttachment(entry))}
                >
                  <span class="glyph" class:remote={!entry.downloaded}>
                    <Ph svg={kindIcon[entry.kind] ?? kindIcon.other} size={18} />
                  </span>
                  <span class="name-col">
                    <span class="fname" dir="auto" title={entry.filename}>{entry.filename}</span>
                    <span class="subject" dir="auto">{entry.subject || $t('detail.noSubject')}</span>
                  </span>
                  <span class="sender" dir="auto" title={entry.fromAddress}>{senderOf(entry)}</span>
                  <span class="size">
                    {#if !entry.downloaded}
                      <span class="cloud" title={$t('attachments.notDownloaded')}>
                        <Ph svg={cloudIcon} size={13} />
                        <span class="sr">{$t('attachments.notDownloaded')}</span>
                      </span>
                    {/if}
                    {formatBytes(entry.sizeBytes)}
                  </span>
                  <span class="date">{formatListDate(entry.date, $prefs.timeFormat as TimeFormat)}</span>
                </div>
              {:else}
                <div class="row placeholder" use:measure aria-hidden="true"><span class="bar"></span></div>
              {/if}
            {/if}
          {/each}
          <div style={`height:${bottomPad}px`} aria-hidden="true"></div>
        </div>
      {/if}

      {#if selected.size > 1}
        <div class="selection-bar" role="region" aria-label={$t('attachments.selection')}>
          <span>{$t('attachments.selectedCount').replace('{count}', String(selected.size))}</span>
          <span class="spacer"></span>
          <button type="button" class="primary" disabled={savingSelected} on:click={saveSelected}>
            <Ph svg={saveIcon} size={14} />
            {$t('attachments.saveSelected')}
          </button>
          <button type="button" class="clear" on:click={() => (selected = active ? new Set([active.id]) : new Set())}>
            {$t('attachments.clearSelection')}
          </button>
        </div>
      {/if}
    </div>

    <aside class="inspector-col" aria-label={$t('attachments.preview')}>
      {#if active}
        <AttachmentInspector
          entry={active}
          selectedCount={selected.size}
          on:changed={(e) => replaceEntry(e.detail)}
          on:saveSelected={saveSelected}
        />
      {:else}
        <div class="state">{$t('attachments.pick')}</div>
      {/if}
    </aside>
  </div>
</section>

<style>
  .browser {
    display: grid;
    grid-template-rows: auto auto minmax(0, 1fr);
    min-width: 0;
    min-height: 0;
    height: 100%;
    background: var(--surface-raised);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  h1 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .count {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .spacer {
    flex: 1;
  }

  .layout {
    display: inline-flex;
    gap: var(--space-1);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool[aria-checked='true'] {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .filters {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .kinds,
  .fields {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .kind {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    padding-inline: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .kind:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .kind[aria-pressed='true'] {
    background: var(--selection-bg);
    border-color: var(--accent);
    color: var(--text-primary);
  }

  .field {
    height: var(--control-height);
    min-width: 0;
    padding-inline: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
  }

  .field:focus-within,
  .field:focus {
    outline: none;
    border-color: var(--accent);
  }

  .field.name {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1 1 calc(var(--attachments-tile) * 1.6);
    color: var(--text-tertiary);
  }

  .field.name input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    outline: none;
  }

  input[type='search'].field {
    flex: 0 1 calc(var(--attachments-tile) * 1.4);
  }

  .mailbox {
    min-width: calc(var(--attachments-tile) * 1.2);
  }

  .date {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .clear {
    height: var(--control-height);
    padding-inline: var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .clear:hover {
    background: var(--surface-hover);
  }

  .rule-note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .main {
    display: grid;
    grid-template-columns: minmax(0, 1fr) var(--attachments-preview-width);
    min-height: 0;
  }

  .results-col {
    position: relative;
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    min-width: 0;
    min-height: 0;
    border-inline-end: var(--hairline) solid var(--border-subtle);
  }

  .inspector-col {
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-6);
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    text-align: center;
    height: 100%;
  }

  .state p {
    margin: 0;
  }

  .results {
    overflow-y: auto;
    min-height: 0;
    outline: none;
  }

  .results:focus-visible {
    box-shadow: inset 0 0 0 2px var(--accent);
  }

  .row {
    display: grid;
    grid-template-columns: auto minmax(0, 2fr) minmax(0, 1fr) auto auto;
    align-items: center;
    gap: var(--space-4);
    padding: var(--row-pad-y) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-list);
    cursor: var(--cursor-action);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row[aria-selected='true'] {
    background: var(--selection-bg);
  }

  .row.active {
    box-shadow: inset var(--space-1) 0 0 var(--accent);
  }

  :global([dir='rtl']) .row.active {
    box-shadow: inset calc(var(--space-1) * -1) 0 0 var(--accent);
  }

  .row.placeholder {
    cursor: default;
  }

  .bar {
    grid-column: 1 / -1;
    height: var(--fz-list);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .glyph {
    display: inline-flex;
    color: var(--accent);
  }

  .glyph.remote {
    color: var(--text-tertiary);
  }

  .name-col {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .fname,
  .subject,
  .sender {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fname {
    color: var(--text-primary);
  }

  .subject {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .sender {
    color: var(--text-secondary);
  }

  .size,
  .date {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .size {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    justify-content: flex-end;
  }

  .cloud {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .tile-row {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-2) var(--pane-pad);
  }

  .tile-slot {
    width: var(--attachments-tile);
    flex-shrink: 0;
  }

  .tile-slot.placeholder {
    aspect-ratio: 1;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .selection-bar {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--pane-pad);
    border-top: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .primary {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding-inline: var(--space-4);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font: inherit;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .primary:disabled {
    opacity: 0.6;
    cursor: default;
  }

  /* a narrow window puts the preview under the files and keeps the rows to
     name and size. */
  .browser.narrow .main {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: minmax(0, 3fr) minmax(0, 2fr);
  }

  .browser.narrow .results-col {
    border-inline-end: none;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .browser.narrow .row {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .browser.narrow .row .sender,
  .browser.narrow .row .date {
    display: none;
  }
</style>
