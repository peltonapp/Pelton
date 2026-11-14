<script lang="ts">
  // the settings screen of the new design. it takes over the window: a
  // navigation of pages on the left with a search over single settings, the
  // open page on the right. pages are separate components loaded on first use.
  import { createEventDispatcher, tick, type ComponentType } from 'svelte'
  import { get } from 'svelte/store'
  // the duotone x draws a tinted square behind the cross, which reads as a
  // checkbox rather than as close.
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import goIcon from '@phosphor-icons/core/assets/regular/arrow-right.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Highlight from '../common/Highlight.svelte'
  import Spinner from '../common/Spinner.svelte'
  import PassphraseDialog from '../settings/PassphraseDialog.svelte'
  import { settingsPages2, settingsGroups2, settingsIndex2, pageExists, loadPage, resolvePage, type SettingsIndexEntry } from '../../lib/settings2'
  import { popupIsOpen } from '../../lib/popups'
  import { recording } from '../../stores/shortcuts'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** Page to open on, as a page key or a classic category key. */
  export let initialCategory: string | null = null

  const dispatch = createEventDispatcher<{ close: void; rerunOnboarding: void }>()

  const pages = settingsPages2.filter((p) => pageExists(p.key))
  let active = resolvePage(initialCategory)
  let component: ComponentType | null = null
  let contentEl: HTMLDivElement | undefined
  let loadedKey = ''

  // the load in flight, so a caller that asks for the page already being
  // loaded waits for it instead of returning early.
  let loading: Promise<void> = Promise.resolve()

  $: void open(active)
  function open(key: string): Promise<void> {
    if (key === loadedKey) {
      return loading
    }
    loadedKey = key
    component = null
    loading = (async () => {
      try {
        const loaded = await loadPage(key)
        if (loadedKey === key) {
          component = loaded
          await tick()
          contentEl?.scrollTo({ top: 0 })
        }
      } catch (err) {
        toastError(errorMessage(err))
      }
    })()
    return loading
  }

  let query = ''
  $: q = query.trim().toLowerCase()
  $: terms = q === '' ? [] : q.split(/\s+/)
  $: pageLabel = (key: string): string => $t(pages.find((p) => p.key === key)?.labelKey ?? '')

  // every word typed has to appear in the setting's name, its synonyms or the
  // name of its page. names that start with the search come first.
  $: results =
    terms.length === 0
      ? []
      : settingsIndex2
          .filter((entry) => {
            if (!pageExists(entry.page)) {
              return false
            }
            const hay = `${$t(entry.labelKey)} ${entry.keywords ?? ''} ${pageLabel(entry.page)}`.toLowerCase()
            return terms.every((term) => hay.includes(term))
          })
          .sort((a, b) => rank(a) - rank(b))
  $: rank = (entry: SettingsIndexEntry): number => {
    const label = $t(entry.labelKey).toLowerCase()
    return label.startsWith(q) ? 0 : terms.every((term) => label.includes(term)) ? 1 : 2
  }
  $: groups = pages
    .map((page) => ({ page, entries: results.filter((r) => r.page === page.key) }))
    .filter((g) => g.entries.length > 0)
  $: flat = groups.flatMap((g) => g.entries)

  // the result the arrow keys are on; Enter opens it.
  let cursor = 0
  $: if (q !== undefined) {
    cursor = 0
  }

  const markMs = 2600
  const findFrames = 30

  // a search result opens its page, scrolls to the setting and marks it. the
  // page may still be laying out its rows, so the row is looked for over a
  // few frames.
  async function openResult(entry: SettingsIndexEntry): Promise<void> {
    query = ''
    active = entry.page
    await open(entry.page)
    await tick()
    let row: HTMLElement | null = null
    for (let i = 0; i < findFrames && !row; i += 1) {
      row = contentEl?.querySelector<HTMLElement>(`[data-setting="${entry.id}"]`) ?? null
      if (!row) {
        await new Promise((resolve) => requestAnimationFrame(resolve))
      }
    }
    if (!row) {
      return
    }
    const found = row
    found.scrollIntoView({ block: 'center', behavior: 'smooth' })
    found.classList.remove('s2-found')
    void found.offsetWidth
    found.classList.add('s2-found')
    setTimeout(() => found.classList.remove('s2-found'), markMs)
  }

  function onSearchKeydown(event: KeyboardEvent): void {
    if (flat.length === 0) {
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const step = event.key === 'ArrowDown' ? 1 : -1
      cursor = (cursor + step + flat.length) % flat.length
      void tick().then(() => contentEl?.querySelector('.result.on')?.scrollIntoView({ block: 'nearest' }))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      void openResult(flat[cursor])
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || popupIsOpen() || get(recording)) {
      return
    }
    if (query) {
      query = ''
    } else {
      dispatch('close')
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

<div class="screen" role="dialog" aria-modal="true" aria-label={$t('settings.title')}>
  <nav class="nav" aria-label={$t('settingsPanel.navAria')}>
    <h2>{$t('settings.title')}</h2>
    <label class="search">
      <Ph svg={searchIcon} size={14} />
      <input
        type="text"
        bind:value={query}
        placeholder={$t('settings.search')}
        aria-label={$t('settings.search')}
        on:keydown={onSearchKeydown}
      />
    </label>

    <div class="pages">
      {#each ['', ...settingsGroups2.map((g) => g.key)] as groupKey (groupKey)}
        {@const items = pages.filter((p) => p.group === groupKey)}
        {#if items.length}
          {#if groupKey}
            <div class="group">{$t(settingsGroups2.find((g) => g.key === groupKey)?.labelKey ?? '')}</div>
          {/if}
          {#each items as page (page.key)}
            <button
              type="button"
              class="item"
              class:active={active === page.key && q === ''}
              class:dim={q !== '' && !groups.some((g) => g.page.key === page.key)}
              aria-current={active === page.key ? 'page' : undefined}
              on:click={() => {
                active = page.key
                query = ''
              }}
            >
              <Ph svg={page.icon} size={17} />
              <span class="item-name">{$t(page.labelKey)}</span>
              {#if q !== ''}
                {@const hits = groups.find((g) => g.page.key === page.key)?.entries.length ?? 0}
                {#if hits > 0}
                  <span class="hits">{hits}</span>
                {/if}
              {/if}
            </button>
          {/each}
        {/if}
      {/each}
    </div>
  </nav>

  <div class="main">
    <header class="head">
      <h1>{q === '' ? pageLabel(active) : $t('settings.search')}</h1>
      <button type="button" class="close" aria-label={$t('settingsPanel.closeAria')} on:click={() => dispatch('close')}>
        <Ph svg={closeIcon} size={18} />
      </button>
    </header>

    <div class="content" bind:this={contentEl}>
      <div class="column">
        {#if q !== ''}
          {#if results.length === 0}
            <p class="empty">{$t('settings.searchEmpty')}</p>
          {:else}
            <p class="found-note">{$t('settings2.search.hint').replace('{count}', String(flat.length))}</p>
            {#each groups as group (group.page.key)}
              <div class="result-group">
                <div class="result-head">
                  <Ph svg={group.page.icon} size={15} />
                  <span>{$t(group.page.labelKey)}</span>
                </div>
                <div class="results">
                  {#each group.entries as entry (entry.id)}
                    <!-- svelte-ignore a11y-mouse-events-have-key-events -->
                    <button
                      type="button"
                      class="result"
                      class:on={flat[cursor] === entry}
                      on:mousemove={() => (cursor = flat.indexOf(entry))}
                      on:click={() => openResult(entry)}
                    >
                      <span><Highlight text={$t(entry.labelKey)} {terms} /></span>
                      <span class="go" aria-hidden="true"><Ph svg={goIcon} size={14} /></span>
                    </button>
                  {/each}
                </div>
              </div>
            {/each}
          {/if}
        {:else if component}
          {#key active}
            <div class="page">
              <svelte:component this={component} on:close={() => dispatch('close')} on:rerunOnboarding={() => dispatch('rerunOnboarding')} />
            </div>
          {/key}
        {:else}
          <Spinner />
        {/if}
      </div>
    </div>
  </div>
</div>

<PassphraseDialog />

<style>
  .screen {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: grid;
    grid-template-columns: 232px minmax(0, 1fr);
    background: var(--chrome-bg);
    animation: arrive var(--motion-base) var(--ease-out);
  }

  @keyframes arrive {
    from {
      opacity: 0;
      transform: scale(0.992);
    }
  }

  .nav {
    display: flex;
    flex-direction: column;
    min-height: 0;
    /* the navigation starts under the macOS traffic lights. */
    padding: calc(var(--titlebar-lights) + var(--space-4)) var(--space-3) var(--space-3);
  }

  h2 {
    margin: 0 var(--space-3) var(--space-4);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .search {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: var(--control-height);
    margin-bottom: var(--space-4);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .search:focus-within {
    border-color: var(--accent);
  }

  .search input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    font-size: var(--fz-label);
  }

  .pages {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .group {
    margin: var(--space-5) var(--space-3) var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-list);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .item:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .item.active {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .item.active :global(.ph) {
    color: var(--accent);
  }

  /* the page sits on a raised surface beside the navigation, the way the list
     and reader do in the main window. */
  .main {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    margin: var(--pane-gap) var(--pane-gap) var(--pane-gap) 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--pane-radius);
    background: var(--surface-base);
    overflow: hidden;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-5) var(--space-3) var(--space-6);
    flex-shrink: 0;
    --wails-draggable: drag;
  }

  h1 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.01em;
  }

  .close {
    display: inline-flex;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    --wails-draggable: no-drag;
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .content {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-3) var(--space-6) var(--space-6);
  }

  .column {
    max-width: 760px;
  }

  .page {
    animation: turn var(--motion-base) var(--ease-out);
  }

  @keyframes turn {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }

  .results {
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .result {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: none;
    background: transparent;
    font-size: var(--fz-list);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .result + .result {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .result.on {
    background: var(--selection-bg);
  }

  .go {
    display: inline-flex;
    color: var(--accent);
    opacity: 0;
    transform: translateX(-4px);
    transition:
      opacity var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-out);
  }

  .result.on .go {
    opacity: 1;
    transform: none;
  }

  .result-group + .result-group {
    margin-top: var(--space-5);
  }

  .result-head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 var(--space-1) var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }

  /* the setting a search result led to: an accent outline and tint that hold
     long enough to be found, then fade. */
  .content :global(.s2-found) {
    border-radius: var(--radius-control);
    animation: s2-mark 2600ms var(--ease-out);
  }

  @keyframes s2-mark {
    0%,
    70% {
      background: var(--selection-bg);
      box-shadow: inset 0 0 0 2px var(--accent);
    }
    100% {
      background: transparent;
      box-shadow: inset 0 0 0 2px transparent;
    }
  }

  .found-note {
    margin: 0 var(--space-1) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .item-name {
    flex: 1;
    min-width: 0;
  }

  .item.dim {
    opacity: 0.45;
  }

  .hits {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .empty {
    margin: var(--space-4) var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
