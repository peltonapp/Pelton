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
  import Ph from '../common/Ph.svelte'
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

  $: void open(active)
  async function open(key: string): Promise<void> {
    if (key === loadedKey) {
      return
    }
    loadedKey = key
    component = null
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
  }

  let query = ''
  $: q = query.trim().toLowerCase()
  $: pageLabel = (key: string): string => $t(pages.find((p) => p.key === key)?.labelKey ?? '')
  $: shownPages =
    q === ''
      ? pages
      : pages.filter((p) => $t(p.labelKey).toLowerCase().includes(q) || results.some((r) => r.page === p.key))
  $: results =
    q === ''
      ? []
      : settingsIndex2.filter(
          (entry) =>
            pageExists(entry.page) &&
            ($t(entry.labelKey).toLowerCase().includes(q) || (entry.keywords ?? '').toLowerCase().includes(q)),
        )

  // a search result opens its page and then points at the setting itself.
  async function openResult(entry: SettingsIndexEntry): Promise<void> {
    query = ''
    active = entry.page
    await open(entry.page)
    await tick()
    const row = contentEl?.querySelector<HTMLElement>(`[data-setting="${entry.id}"]`)
    if (row) {
      row.scrollIntoView({ block: 'center' })
      row.classList.add('found')
      row.addEventListener('animationend', () => row.classList.remove('found'), { once: true })
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
      <input type="text" bind:value={query} placeholder={$t('settings.search')} aria-label={$t('settings.search')} />
    </label>

    <div class="pages">
      {#each ['', ...settingsGroups2.map((g) => g.key)] as groupKey (groupKey)}
        {@const items = shownPages.filter((p) => p.group === groupKey)}
        {#if items.length}
          {#if groupKey}
            <div class="group">{$t(settingsGroups2.find((g) => g.key === groupKey)?.labelKey ?? '')}</div>
          {/if}
          {#each items as page (page.key)}
            <button
              type="button"
              class="item"
              class:active={active === page.key && q === ''}
              aria-current={active === page.key ? 'page' : undefined}
              on:click={() => {
                active = page.key
                query = ''
              }}
            >
              <Ph svg={page.icon} size={17} />
              <span>{$t(page.labelKey)}</span>
            </button>
          {/each}
        {/if}
      {/each}
      {#if shownPages.length === 0}
        <p class="empty">{$t('settings.searchEmpty')}</p>
      {/if}
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
            <div class="results">
              {#each results as entry (entry.page + entry.id)}
                <button type="button" class="result" on:click={() => openResult(entry)}>
                  <span>{$t(entry.labelKey)}</span>
                  <span class="where">{pageLabel(entry.page)}</span>
                </button>
              {/each}
            </div>
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
  }

  .result + .result {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .result:hover {
    background: var(--surface-hover);
  }

  .where {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .empty {
    margin: var(--space-4) var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
