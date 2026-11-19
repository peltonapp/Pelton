<script lang="ts">
  // the search field in the top bar of the new design. typed operators become
  // chips inside the field; a dropdown completes operators with real values
  // (people, folders, flags, dates) and offers people, folders and recent
  // searches for plain text. enter shows the results in the list pane.
  import { onDestroy, onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/duotone/x-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-simple-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-counter-clockwise-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import hashIcon from '@phosphor-icons/core/assets/duotone/hash-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import tagIcon from '@phosphor-icons/core/assets/duotone/funnel-simple-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import DateTimePicker from '../common/DateTimePicker.svelte'
  import { searchAddresses } from '../../lib/api'
  import { accountLabel } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import { chipLabel, parseQuery, addChip, type SearchChip, type OperatorKey } from '../../lib/searchquery'
  import { suggest, suggestContext, type FolderOption, type Person, type Suggestion } from '../../lib/searchsuggest'
  import { sidebar } from '../../stores/accounts'
  import { prefs } from '../../stores/prefs'
  import { menuHint, bindings } from '../../stores/shortcuts'
  import { space, openSpace } from '../../stores/space'
  import {
    activeSearch,
    clearRecentSearches,
    clearSearch,
    commitChip,
    dropChip,
    loadRecentSearches,
    recentSearches,
    searchDraft,
    setDraft,
    setDraftText,
    submitSearch,
  } from '../../stores/search'

  const listId = 'top-search-list'

  let wrap: HTMLDivElement
  let inputEl: HTMLInputElement
  let open = false
  let activeIndex = -1
  let optionEls: HTMLElement[] = []
  let picking: 'before' | 'after' | null = null
  let pickDate = ''

  onMount(() => {
    void loadRecentSearches()
    document.addEventListener('pointerdown', onOutside, true)
  })
  onDestroy(() => {
    clearTimeout(peopleTimer)
    document.removeEventListener('pointerdown', onOutside, true)
  })

  $: text = $searchDraft.text
  $: chips = $searchDraft.chips
  $: folderOptions = buildFolderOptions($sidebar.data)
  $: multiAccount = ($sidebar.data?.accounts.length ?? 0) > 1
  $: ctx = suggestContext(text)
  $: rows = suggest({ text, people, folders: folderOptions, recents: $recentSearches })
  $: if (activeIndex >= rows.length) {
    activeIndex = rows.length - 1
  }
  $: showList = open && (rows.length > 0 || picking !== null)
  $: hasContent = text !== '' || chips.length > 0 || $activeSearch !== null
  $: searchKeys = $bindings.search ? menuHint('search') : ''

  function buildFolderOptions(data: typeof $sidebar.data): FolderOption[] {
    if (!data) {
      return []
    }
    const out: FolderOption[] = []
    for (const account of data.accounts) {
      for (const folder of data.foldersByAccount[account.id] ?? []) {
        out.push({ id: folder.id, name: folder.name, accountLabel: accountLabel(account) })
      }
    }
    return out
  }

  function resolveFolder(name: string): { id: number; label: string } | null {
    const lower = name.toLowerCase()
    const hit = folderOptions.find((f) => f.name.toLowerCase() === lower)
    return hit ? { id: hit.id, label: hit.name } : null
  }

  // people come from the address autocomplete: for from:/to: whatever has been
  // typed after the colon, for plain text the last word once it says something.
  let people: Person[] = []
  let peopleTimer: ReturnType<typeof setTimeout> | undefined
  let peopleQuery: string | null = null
  let peopleGen = 0
  $: void fetchPeople(ctx.field, ctx.partial)

  function fetchPeople(field: string | null, partial: string): void {
    const wanted = field === 'from' || field === 'to' ? partial : field === null && partial.length >= 2 ? partial : null
    if (wanted === peopleQuery) {
      return
    }
    peopleQuery = wanted
    clearTimeout(peopleTimer)
    if (wanted === null) {
      people = []
      return
    }
    const gen = ++peopleGen
    peopleTimer = setTimeout(async () => {
      try {
        const found = await searchAddresses(wanted, 6)
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

  function onOutside(event: PointerEvent): void {
    if (wrap && !wrap.contains(event.target as Node)) {
      open = false
      picking = null
    }
  }

  function onInput(event: Event): void {
    setDraftText((event.currentTarget as HTMLInputElement).value)
    open = true
    activeIndex = -1
    picking = null
  }

  async function focusInput(): Promise<void> {
    await tick()
    inputEl?.focus()
  }

  // the text before the token a suggestion replaces, with one space kept
  // between it and whatever comes next.
  function textBeforeToken(): string {
    const head = text.slice(0, ctx.start).replace(/\s+$/, '')
    return head ? `${head} ` : ''
  }

  // run parses any complete operator left in the text into chips and shows the
  // results in the list pane.
  function run(): void {
    const current = get(searchDraft)
    const parsed = parseQuery(current.text, resolveFolder)
    let merged = current.chips
    for (const chip of parsed.chips) {
      merged = addChip(merged, chip)
    }
    setDraft({ text: parsed.text, chips: merged })
    submitSearch()
    open = false
    activeIndex = -1
    if ($space !== 'mail') {
      openSpace('mail')
    }
  }

  function commitAndRun(chip: SearchChip): void {
    setDraftText(textBeforeToken())
    commitChip(chip)
    picking = null
    run()
    void focusInput()
  }

  function apply(row: Suggestion): void {
    switch (row.kind) {
      case 'operator':
        setDraftText(`${textBeforeToken()}${row.key}:`)
        activeIndex = -1
        open = true
        void focusInput()
        return
      case 'chip':
        commitAndRun(row.chip)
        return
      case 'datePicker':
        picking = row.field
        pickDate = ''
        return
      case 'recent':
        setDraft(row.search)
        run()
        return
      case 'clearRecent':
        clearRecentSearches()
        activeIndex = -1
        void focusInput()
        return
    }
  }

  function applyPickedDate(): void {
    if (picking && pickDate) {
      commitAndRun({ field: picking, value: pickDate })
    }
  }

  function clearAll(): void {
    clearSearch()
    open = false
    picking = null
    activeIndex = -1
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      open = true
      activeIndex = Math.min(activeIndex + 1, rows.length - 1)
      void revealActive()
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      activeIndex = Math.max(activeIndex - 1, -1)
      void revealActive()
    } else if (event.key === 'Enter') {
      event.preventDefault()
      if (showList && activeIndex >= 0 && rows[activeIndex]) {
        apply(rows[activeIndex])
      } else {
        run()
      }
    } else if (event.key === 'Tab' && showList && activeIndex >= 0 && rows[activeIndex]?.kind === 'operator') {
      event.preventDefault()
      apply(rows[activeIndex])
    } else if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      if (picking) {
        picking = null
      } else if (showList) {
        open = false
        activeIndex = -1
      } else {
        clearAll()
        inputEl.blur()
      }
    } else if (event.key === 'Backspace' && text === '' && chips.length > 0) {
      dropChip(chips[chips.length - 1])
    }
  }

  async function revealActive(): Promise<void> {
    await tick()
    optionEls[activeIndex]?.scrollIntoView?.({ block: 'nearest' })
  }

  // the hover band slides to the active option rather than jumping there.
  let bandTop = 0
  let bandHeight = 0
  $: void placeBand(activeIndex, rows)
  async function placeBand(index: number, _rows: Suggestion[]): Promise<void> {
    await tick()
    const el = optionEls[index]
    if (el) {
      bandTop = el.offsetTop
      bandHeight = el.offsetHeight
    }
  }

  type Group = 'recent' | 'operators' | 'people' | 'folders' | 'values' | 'dates' | 'clear'

  function groupOf(row: Suggestion): Group {
    switch (row.kind) {
      case 'operator':
        return 'operators'
      case 'chip':
        return row.group
      case 'datePicker':
        return 'dates'
      case 'recent':
        return 'recent'
      case 'clearRecent':
        return 'clear'
    }
  }

  const groupTitles: Partial<Record<Group, string>> = {
    recent: 'search.suggest.recent',
    operators: 'search.suggest.operators',
    people: 'search.suggest.people',
    folders: 'search.suggest.folders',
  }

  function headingFor(index: number, list: Suggestion[]): string | null {
    const group = groupOf(list[index])
    if (index > 0 && groupOf(list[index - 1]) === group) {
      return null
    }
    const key = groupTitles[group]
    return key ? $t(key) : null
  }

  function iconFor(row: Suggestion): string {
    switch (row.kind) {
      case 'operator':
        return hashIcon
      case 'recent':
        return clockIcon
      case 'clearRecent':
        return trashIcon
      case 'datePicker':
        return calendarIcon
      case 'chip':
        if (row.group === 'people') {
          return userIcon
        }
        if (row.group === 'folders') {
          return folderIcon
        }
        return row.group === 'dates' ? calendarIcon : tagIcon
    }
  }

  function recentLabel(search: { text: string; chips: SearchChip[] }): string {
    return [...search.chips.map((c) => chipLabel(c, $t)), search.text].filter(Boolean).join('  ')
  }

  function folderAccount(id: number | undefined): string {
    return folderOptions.find((f) => f.id === id)?.accountLabel ?? ''
  }

  function operatorHint(key: OperatorKey): string {
    return $t(`search.op.${key}`)
  }
</script>

<div class="wrap" bind:this={wrap}>
  <div class="field" class:active={$activeSearch !== null}>
    <span class="lead"><Ph svg={searchIcon} size={15} /></span>
    <div class="chips">
      {#each chips as chip (chip.field + chip.value)}
        <span class="chip">
          <span class="chip-text">{chipLabel(chip, $t)}</span>
          <button
            type="button"
            class="chip-x"
            aria-label={`${$t('messageList.search.removeChip')}: ${chipLabel(chip, $t)}`}
            on:click={() => dropChip(chip)}
          >
            <Ph svg={xIcon} size={11} />
          </button>
        </span>
      {/each}
      <input
        bind:this={inputEl}
        data-top-search
        type="text"
        role="combobox"
        autocomplete="off"
        spellcheck="false"
        aria-label={$t('topbar.search')}
        aria-expanded={showList}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={showList && activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
        placeholder={chips.length === 0 ? $t('topbar.search') : ''}
        value={text}
        on:input={onInput}
        on:keydown={onKeydown}
        on:focus={() => (open = true)}
        on:click={() => (open = true)}
      />
    </div>
    {#if hasContent}
      <button type="button" class="clear" aria-label={$t('messageList.search.clearSearch')} on:click={() => { clearAll(); void focusInput() }}>
        <Ph svg={xIcon} size={13} />
      </button>
    {:else if searchKeys && $prefs.showShortcutHints}
      <kbd>{searchKeys}</kbd>
    {/if}
  </div>

  {#if showList}
    <div class="list" id={listId} role="listbox" aria-label={$t('topbar.search')}>
      <span
        class="band"
        class:shown={activeIndex >= 0}
        style={`transform: translateY(${bandTop}px); height: ${bandHeight}px`}
        aria-hidden="true"
      ></span>
      {#each rows as row, i (i)}
        {@const heading = headingFor(i, rows)}
        {#if heading}
          <div class="heading" role="presentation">{heading}</div>
        {:else if i > 0 && groupOf(row) !== groupOf(rows[i - 1])}
          <div class="divider" role="presentation"></div>
        {/if}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <div
          class="option"
          class:danger={row.kind === 'clearRecent'}
          id={`${listId}-${i}`}
          role="option"
          aria-selected={i === activeIndex}
          tabindex="-1"
          bind:this={optionEls[i]}
          on:pointerenter={() => (activeIndex = i)}
          on:mousedown={(e) => e.preventDefault()}
          on:click={() => apply(row)}
        >
          <span class="icon"><Ph svg={iconFor(row)} size={14} /></span>
          {#if row.kind === 'operator'}
            <span class="main mono">{row.key}:</span>
            <span class="meta">{operatorHint(row.key)}</span>
          {:else if row.kind === 'chip'}
            {#if row.group === 'people'}
              <span class="main">
                <span class="op">{$t(`messageList.search.chip.${row.chip.field}`)}:</span>
                {row.chip.label}
              </span>
              {#if row.chip.label !== row.chip.value}
                <span class="meta">{row.chip.value}</span>
              {/if}
            {:else if row.group === 'folders'}
              <span class="main"><span class="op">{$t('search.chip.in')}:</span> {row.chip.label}</span>
              {#if multiAccount}
                <span class="meta">{folderAccount(row.chip.id)}</span>
              {/if}
            {:else if row.kind === 'chip' && row.shortcut}
              <span class="main">
                <span class="op">{$t(`messageList.search.chip.${row.chip.field}`)}:</span>
                {$t(`search.date.${row.shortcut}`)}
              </span>
              <span class="meta mono">{row.chip.value}</span>
            {:else}
              <span class="main">{chipLabel(row.chip, $t)}</span>
            {/if}
          {:else if row.kind === 'datePicker'}
            <span class="main">{$t('search.suggest.pickDate')}</span>
          {:else if row.kind === 'recent'}
            <span class="main">{recentLabel(row.search)}</span>
          {:else}
            <span class="main">{$t('search.suggest.clearRecent')}</span>
          {/if}
        </div>
      {/each}
      {#if picking}
        <div class="pick">
          <span class="pick-label">{$t(`messageList.search.chip.${picking}`)}</span>
          <div class="pick-field">
            <DateTimePicker mode="date" bind:value={pickDate} />
          </div>
          <button type="button" class="apply" disabled={!pickDate} on:click={applyPickedDate}>
            {$t('messageList.search.apply')}
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .wrap {
    position: relative;
    width: 100%;
    --wails-draggable: no-drag;
  }

  .field {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-2));
    padding-block: 0;
    padding-inline: var(--space-3) var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    cursor: text;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .field:hover {
    border-color: var(--border-strong);
  }

  .field:focus-within {
    border-color: var(--accent);
    color: var(--text-secondary);
  }

  .lead {
    display: inline-flex;
  }

  .chips {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .chips::-webkit-scrollbar {
    display: none;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    gap: var(--space-1);
    height: var(--chip-height);
    padding-inline: var(--space-2) var(--space-1);
    border-radius: var(--radius-control);
    background: var(--selection-bg);
    color: var(--text-primary);
    font-size: var(--fz-meta);
    white-space: nowrap;
    animation: chip-in var(--motion-base) var(--ease-out);
  }

  @keyframes chip-in {
    from {
      opacity: 0;
      transform: scale(0.86);
    }
  }

  .chip-x,
  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out), background-color var(--motion-fast) var(--ease-out);
  }

  .chip-x:hover,
  .clear:hover {
    color: var(--text-primary);
    background: var(--surface-hover);
  }

  input {
    flex: 1;
    min-width: calc(var(--space-6) * 3);
    height: 100%;
    padding: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
  }

  input::placeholder {
    color: var(--text-tertiary);
  }

  kbd {
    flex-shrink: 0;
    padding-inline-end: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .list {
    position: absolute;
    top: calc(100% + var(--space-2));
    inset-inline: 0;
    z-index: 60;
    max-height: var(--suggest-max-height);
    overflow-y: auto;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: rise var(--motion-base) var(--ease-out);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(calc(var(--space-2) * -1));
    }
  }

  .band {
    position: absolute;
    top: 0;
    inset-inline: var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-hover);
    opacity: 0;
    pointer-events: none;
    transition:
      transform var(--motion-fast) var(--ease-out),
      height var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .band.shown {
    opacity: 1;
  }

  .heading {
    padding: var(--space-3) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .heading:first-of-type {
    padding-top: var(--space-2);
  }

  .divider {
    height: var(--hairline);
    margin: var(--space-1) var(--space-3);
    background: var(--border-subtle);
  }

  .option {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) - var(--space-2));
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-control);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    outline: none;
  }

  .option.danger {
    color: var(--text-secondary);
  }

  .icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .main {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .op {
    color: var(--text-tertiary);
  }

  .meta {
    flex-shrink: 0;
    max-width: 50%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .mono {
    font-family: var(--font-mono);
  }

  .pick {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-1);
    padding: var(--space-2) var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .pick-label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .pick-field {
    flex: 1;
    min-width: 0;
  }

  .apply {
    padding: var(--space-1) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .apply:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
