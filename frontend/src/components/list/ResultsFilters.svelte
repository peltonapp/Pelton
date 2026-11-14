<script lang="ts">
  // the filter chips above search results. each one refines the running search
  // at once: the flag toggles flip a chip, the others open a small picker for a
  // sender, recipient, date range, folder or account. sort and save as view sit
  // at the end of the row.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import caretIcon from '@phosphor-icons/core/assets/duotone/caret-down-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import sortIcon from '@phosphor-icons/core/assets/duotone/sort-ascending-duotone.svg?raw'
  import bookmarkIcon from '@phosphor-icons/core/assets/duotone/bookmark-simple-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import DateTimePicker from '../common/DateTimePicker.svelte'
  import SortOptions from './SortOptions.svelte'
  import { searchAddresses } from '../../lib/api'
  import { accountLabel } from '../../lib/format'
  import { float } from '../../lib/floating'
  import { t } from '../../lib/i18n'
  import { buildFilter, chipLabel, type ChipField, type SearchChip } from '../../lib/searchquery'
  import type { SearchSortPref } from '../../lib/types'
  import { sidebar } from '../../stores/accounts'
  import { prefs } from '../../stores/prefs'
  import { searchSortKind } from '../../stores/messages'
  import { activeSearch, commitChip, dropChip, flipChip, searchDraft, setChips } from '../../stores/search'
  import { openViewEditor } from '../../stores/views'

  const dispatch = createEventDispatcher<{ sort: SearchSortPref }>()

  type Pop = 'from' | 'to' | 'date' | 'folder' | 'account' | 'sort'

  const pickers: { pop: Exclude<Pop, 'sort'>; field: ChipField | null }[] = [
    { pop: 'from', field: 'from' },
    { pop: 'to', field: 'to' },
    { pop: 'date', field: null },
    { pop: 'folder', field: 'in' },
    { pop: 'account', field: 'account' },
  ]

  let openPop: Pop | null = null
  let anchors: Partial<Record<Pop, HTMLElement>> = {}
  let popEl: HTMLDivElement | undefined
  let personInput: HTMLInputElement | undefined
  let personQuery = ''
  let people: { email: string; name: string }[] = []
  let peopleTimer: ReturnType<typeof setTimeout> | undefined
  let peopleGen = 0
  let folderQuery = ''
  let afterDate = ''
  let beforeDate = ''

  $: chips = $searchDraft.chips
  $: chipOf = (field: ChipField): SearchChip | undefined => chips.find((c) => c.field === field)
  $: has = (field: ChipField, value: string): boolean => chips.some((c) => c.field === field && c.value === value)
  $: flagsOff = !chips.some((c) => c.field === 'is' || c.field === 'has')
  $: accounts = $sidebar.data?.accounts ?? []
  $: folderGroups = accounts.map((a) => ({
    label: accountLabel(a),
    folders: ($sidebar.data?.foldersByAccount[a.id] ?? []).filter(
      (f) => folderQuery.trim() === '' || f.name.toLowerCase().includes(folderQuery.trim().toLowerCase()),
    ),
  }))
  $: dateChips = chips.filter((c) => c.field === 'after' || c.field === 'before')

  onDestroy(() => clearTimeout(peopleTimer))

  async function toggle(pop: Pop): Promise<void> {
    if (openPop === pop) {
      openPop = null
      return
    }
    openPop = pop
    if (pop === 'from' || pop === 'to') {
      personQuery = ''
      people = []
      lookup('')
      await tick()
      personInput?.focus()
    } else if (pop === 'date') {
      afterDate = chipOf('after')?.value ?? ''
      beforeDate = chipOf('before')?.value ?? ''
    } else if (pop === 'folder') {
      folderQuery = ''
    }
  }

  function lookup(query: string): void {
    clearTimeout(peopleTimer)
    const gen = ++peopleGen
    peopleTimer = setTimeout(async () => {
      try {
        const found = await searchAddresses(query, 6)
        if (gen === peopleGen) {
          people = (found ?? []).map((p) => ({ email: p.email, name: p.name }))
        }
      } catch {
        if (gen === peopleGen) {
          people = []
        }
      }
    }, 120)
  }

  function pickPerson(field: 'from' | 'to', value: string, label?: string): void {
    if (value.trim() === '') {
      return
    }
    commitChip({ field, value: value.trim(), label })
    openPop = null
  }

  function onPersonKey(event: KeyboardEvent, field: 'from' | 'to'): void {
    if (event.key === 'Enter') {
      event.preventDefault()
      pickPerson(field, personQuery)
    }
  }

  function applyDates(): void {
    let next = chips.filter((c) => c.field !== 'after' && c.field !== 'before')
    if (afterDate) {
      next = [...next, { field: 'after', value: afterDate }]
    }
    if (beforeDate) {
      next = [...next, { field: 'before', value: beforeDate }]
    }
    setChips(next)
    openPop = null
  }

  function clearDates(): void {
    setChips(chips.filter((c) => c.field !== 'after' && c.field !== 'before'))
    openPop = null
  }

  function clearField(field: ChipField): void {
    const current = chipOf(field)
    if (current) {
      dropChip(current)
    }
    openPop = null
  }

  function pickFolder(id: number | null, name = ''): void {
    if (id === null) {
      clearField('in')
      return
    }
    commitChip({ field: 'in', value: name, id, label: name })
    openPop = null
  }

  function pickAccount(id: number | null, label = ''): void {
    if (id === null) {
      clearField('account')
      return
    }
    commitChip({ field: 'account', value: label, id, label })
    openPop = null
  }

  function showAll(): void {
    setChips(chips.filter((c) => c.field !== 'is' && c.field !== 'has'))
  }

  function pickSort(pref: SearchSortPref): void {
    dispatch('sort', pref)
    openPop = null
  }

  // the chips name the view when no words were typed.
  function saveAsView(): void {
    const active = $activeSearch
    if (!active) {
      return
    }
    const f = buildFilter(active.chips)
    const name = active.text.trim() || active.chips.map((c) => chipLabel(c, $t)).join(' ')
    openViewEditor({
      name,
      queryText: active.text.trim(),
      queryFrom: f.from ? [f.from] : [],
      queryTo: f.to ? [f.to] : [],
      querySubject: f.subject,
      hasAttachment: f.hasAttachment,
      unreadOnly: f.unreadOnly,
      flaggedOnly: f.flaggedOnly,
      accountId: f.accountId,
    })
  }

  function dateLabel(): string {
    const after = chipOf('after')?.value
    const before = chipOf('before')?.value
    if (after && before) {
      return `${after} - ${before}`
    }
    if (after) {
      return `${$t('messageList.search.chip.after')} ${after}`
    }
    if (before) {
      return `${$t('messageList.search.chip.before')} ${before}`
    }
    return $t('search.filter.date')
  }

  function onOutside(event: PointerEvent): void {
    if (!openPop) {
      return
    }
    const target = event.target as Node
    const anchor = anchors[openPop]
    if (popEl?.contains(target) || anchor?.contains(target)) {
      return
    }
    // a date picker's calendar is a popup of its own, outside this one.
    if ((target as HTMLElement).closest?.('.panel')) {
      return
    }
    openPop = null
  }

  function onKey(event: KeyboardEvent): void {
    if (openPop && event.key === 'Escape') {
      event.stopPropagation()
      const anchor = anchors[openPop]
      openPop = null
      anchor?.focus()
    }
  }

  document.addEventListener('pointerdown', onOutside, true)
  onDestroy(() => document.removeEventListener('pointerdown', onOutside, true))
</script>

<svelte:window on:keydown={onKey} />

<div class="bar" role="toolbar" aria-label={$t('search.filtersLabel')}>
  <button type="button" class="chip" class:on={flagsOff} aria-pressed={flagsOff} on:click={showAll}>
    {$t('search.filter.all')}
  </button>
  <button type="button" class="chip" class:on={has('is', 'unread')} aria-pressed={has('is', 'unread')} on:click={() => flipChip({ field: 'is', value: 'unread' })}>
    {$t('messageList.search.chip.isUnread')}
  </button>
  <button type="button" class="chip" class:on={has('is', 'flagged')} aria-pressed={has('is', 'flagged')} on:click={() => flipChip({ field: 'is', value: 'flagged' })}>
    {$t('sidebar.view.flagged')}
  </button>
  <button
    type="button"
    class="chip"
    class:on={has('has', 'attachment')}
    aria-pressed={has('has', 'attachment')}
    on:click={() => flipChip({ field: 'has', value: 'attachment' })}
  >
    {$t('messageList.search.chip.hasAttachment')}
  </button>

  <span class="sep" aria-hidden="true"></span>

  {#each pickers as entry (entry.pop)}
    {@const pop = entry.pop}
    {@const set = entry.field ? chipOf(entry.field) : dateChips[0]}
    {#if pop !== 'account' || accounts.length > 1}
      <button
        type="button"
        class="chip pick"
        class:on={!!set}
        aria-haspopup="dialog"
        aria-expanded={openPop === pop}
        bind:this={anchors[pop]}
        on:click={() => toggle(pop)}
      >
        <span class="pick-text">
          {#if pop === 'date'}
            {dateLabel()}
          {:else if set}
            {chipLabel(set, $t)}
          {:else if pop === 'from'}
            {$t('messageList.search.chip.from')}
          {:else if pop === 'to'}
            {$t('messageList.search.chip.to')}
          {:else if pop === 'folder'}
            {$t('search.filter.folder')}
          {:else}
            {$t('search.chip.account')}
          {/if}
        </span>
        <Ph svg={caretIcon} size={11} />
      </button>
    {/if}
  {/each}

  <span class="grow"></span>

  {#if $searchSortKind}
    <button
      type="button"
      class="tool"
      aria-label={$t('messageList.search.sortBy')}
      aria-haspopup="menu"
      aria-expanded={openPop === 'sort'}
      bind:this={anchors.sort}
      on:click={() => toggle('sort')}
    >
      <Ph svg={sortIcon} size={15} />
    </button>
  {/if}
  {#if $prefs.viewsPlacement !== 'hidden'}
    <button type="button" class="tool" aria-label={$t('views.saveAsView')} title={$t('views.saveAsView')} on:click={saveAsView}>
      <Ph svg={bookmarkIcon} size={15} />
    </button>
  {/if}
</div>

{#if openPop}
  <div
    class="pop"
    class:menu={openPop === 'sort'}
    role={openPop === 'sort' ? 'menu' : 'dialog'}
    aria-label={openPop === 'sort' ? $t('messageList.search.sortBy') : $t('search.filtersLabel')}
    bind:this={popEl}
    use:float={{ anchor: anchors[openPop], align: openPop === 'sort' ? 'end' : 'start' }}
  >
    {#if openPop === 'from' || openPop === 'to'}
      {@const field = openPop}
      <input
        class="find"
        type="text"
        bind:this={personInput}
        bind:value={personQuery}
        aria-label={$t(`messageList.search.chip.${field}`)}
        placeholder={$t('search.filter.personPlaceholder')}
        on:input={() => lookup(personQuery)}
        on:keydown={(e) => onPersonKey(e, field)}
      />
      {#if chipOf(field)}
        <button type="button" class="row muted" on:click={() => clearField(field)}>
          <Ph svg={xIcon} size={13} />
          <span>{$t('search.filter.anyone')}</span>
        </button>
      {/if}
      {#each people as person (person.email)}
        <button type="button" class="row" on:click={() => pickPerson(field, person.email, person.name || person.email)}>
          <Ph svg={userIcon} size={13} />
          <span class="row-main">{person.name || person.email}</span>
          {#if person.name}<span class="row-meta">{person.email}</span>{/if}
        </button>
      {/each}
    {:else if openPop === 'date'}
      <div class="dates">
        <span class="date-label">{$t('messageList.search.chip.after')}</span>
        <DateTimePicker mode="date" bind:value={afterDate} />
        <span class="date-label">{$t('messageList.search.chip.before')}</span>
        <DateTimePicker mode="date" bind:value={beforeDate} />
      </div>
      <div class="actions">
        {#if dateChips.length > 0}
          <button type="button" class="ghost" on:click={clearDates}>{$t('search.filter.clearDates')}</button>
        {/if}
        <button type="button" class="primary" on:click={applyDates}>{$t('messageList.search.apply')}</button>
      </div>
    {:else if openPop === 'folder'}
      <input class="find" type="text" bind:value={folderQuery} aria-label={$t('search.filter.folder')} placeholder={$t('search.filter.folderPlaceholder')} />
      <div class="scroll">
        <button type="button" class="row muted" on:click={() => pickFolder(null)}>
          <span class="row-main">{$t('search.filter.anyFolder')}</span>
        </button>
        {#each folderGroups as group (group.label)}
          {#if group.folders.length > 0}
            {#if accounts.length > 1}<span class="group">{group.label}</span>{/if}
            {#each group.folders as folder (folder.id)}
              <button
                type="button"
                class="row"
                class:current={chipOf('in')?.id === folder.id}
                on:click={() => pickFolder(folder.id, folder.name)}
              >
                <Ph svg={folderIcon} size={13} />
                <span class="row-main">{folder.name}</span>
              </button>
            {/each}
          {/if}
        {/each}
      </div>
    {:else if openPop === 'account'}
      <button type="button" class="row muted" on:click={() => pickAccount(null)}>
        <span class="row-main">{$t('search.filter.allAccounts')}</span>
      </button>
      {#each accounts as account (account.id)}
        <button
          type="button"
          class="row"
          class:current={chipOf('account')?.id === account.id}
          on:click={() => pickAccount(account.id, accountLabel(account))}
        >
          <span class="row-main">{accountLabel(account)}</span>
        </button>
      {/each}
    {:else}
      <SortOptions on:pick={(e) => pickSort(e.detail)} />
    {/if}
  </div>
{/if}

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 100%;
    height: calc(var(--chip-height) + var(--space-1));
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .chip:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .chip:active {
    transform: scale(0.96);
  }

  .chip.on {
    border-color: transparent;
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .pick {
    padding-inline-end: var(--space-2);
  }

  .pick-text {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .sep {
    width: var(--hairline);
    height: var(--chip-height);
    background: var(--border-default);
  }

  .grow {
    flex: 1;
  }

  .tool {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - var(--space-2));
    height: calc(var(--control-height) - var(--space-2));
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tool:hover,
  .tool[aria-expanded='true'] {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .pop {
    z-index: 70;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: var(--popover-width);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: rise var(--motion-base) var(--ease-out);
  }

  .pop.menu {
    padding: var(--space-1);
    gap: 0;
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  .find {
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    outline: none;
  }

  .find:focus {
    border-color: var(--accent);
  }

  .scroll {
    display: flex;
    flex-direction: column;
    max-height: var(--suggest-max-height);
    overflow-y: auto;
  }

  .group {
    padding: var(--space-2) var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover,
  .row:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .row.current {
    background: var(--selection-bg);
  }

  .row.muted {
    color: var(--text-secondary);
  }

  .row-main {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-meta {
    flex-shrink: 0;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .dates {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
    gap: var(--space-2) var(--space-3);
    padding: var(--space-1);
  }

  .date-label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    padding-top: var(--space-1);
  }

  .primary,
  .ghost {
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .primary {
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .ghost {
    border: var(--hairline) solid var(--border-default);
    background: transparent;
    color: var(--text-secondary);
  }

  .ghost:hover {
    background: var(--surface-hover);
  }
</style>
