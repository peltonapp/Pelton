<script lang="ts">
  // the message list header of the new design: what the list shows and how
  // many, with the sort and display menu and the quick filter toggle. while a
  // search is showing it names the results and offers a way back.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import slidersIcon from '@phosphor-icons/core/assets/duotone/sliders-horizontal-duotone.svg?raw'
  import funnelIcon from '@phosphor-icons/core/assets/duotone/funnel-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-duotone.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { float } from '../../lib/floating'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import type { RowTemplate, SearchSortPref } from '../../lib/types'
  import { prefs, setQuickFilterBar, setRowTemplate, setRowShowSnippet } from '../../stores/prefs'
  import { searchSortKind } from '../../stores/messages'
  import SortOptions from './SortOptions.svelte'
  import { resetQuickFilter } from '../../stores/quickfilter'
  import { clearSearch } from '../../stores/search'

  /** The folder, view or results name. */
  export let title: string
  /** The message or result count, already worded. */
  export let count: string = ''
  /** Unread messages in the folder or view; 0 hides it. */
  export let unread: number = 0
  /** True while the list shows search results. */
  export let searching: boolean = false
  /** Names the folder a closed search returns to. */
  export let backLabel: string = ''

  const dispatch = createEventDispatcher<{ sort: SearchSortPref }>()

  const templates: RowTemplate[] = ['relaxed', 'comfortable', 'compact', 'single']

  let optionsBtn: HTMLButtonElement
  let menuEl: HTMLDivElement | undefined
  let menuOpen = false

  function toggleQuick(): void {
    const next = !$prefs.quickFilterBar
    if (!next) {
      resetQuickFilter()
    }
    setQuickFilterBar(next)
  }

  function pickSort(pref: SearchSortPref): void {
    dispatch('sort', pref)
    menuOpen = false
  }

  function onOutside(event: PointerEvent): void {
    const target = event.target as Node
    if (menuOpen && !menuEl?.contains(target) && !optionsBtn.contains(target)) {
      menuOpen = false
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (menuOpen && event.key === 'Escape') {
      event.stopPropagation()
      menuOpen = false
      optionsBtn.focus()
    }
  }

  document.addEventListener('pointerdown', onOutside, true)
  onDestroy(() => document.removeEventListener('pointerdown', onOutside, true))
</script>

<svelte:window on:keydown={onKey} />

<div class="header">
  <slot name="lead" />
  {#if searching}
    <button
      type="button"
      class="icon-btn"
      aria-label={$t('search.results.close')}
      use:tip={{ text: backLabel ? $t('search.results.backTo').replace('{name}', backLabel) : $t('search.results.close'), side: 'bottom' }}
      on:click={clearSearch}
    >
      <Ph svg={backIcon} size={15} />
    </button>
  {/if}
  <div class="titles">
    <span class="title">{title}</span>
    <span class="counts">
      {#if count}<span>{count}</span>{/if}
      {#if unread > 0 && !searching}
        <span class="dot" aria-hidden="true"></span>
        <span class="unread">{$t('list.header.unread').replace('{count}', unread.toLocaleString())}</span>
      {/if}
    </span>
  </div>
  <button
    type="button"
    class="icon-btn"
    class:on={$prefs.quickFilterBar}
    aria-label={$t('list.header.quickFilter')}
    aria-pressed={$prefs.quickFilterBar}
    use:tip={{ text: $t('list.header.quickFilter'), side: 'bottom' }}
    on:click={toggleQuick}
  >
    <Ph svg={funnelIcon} size={15} />
  </button>
  <button
    type="button"
    class="icon-btn"
    class:on={menuOpen}
    aria-label={$t('list.header.options')}
    aria-haspopup="menu"
    aria-expanded={menuOpen}
    bind:this={optionsBtn}
    use:tip={{ text: $t('list.header.options'), side: 'bottom' }}
    on:click={() => (menuOpen = !menuOpen)}
  >
    <Ph svg={slidersIcon} size={15} />
  </button>
</div>

{#if menuOpen}
  <div class="menu" role="menu" aria-label={$t('list.header.options')} bind:this={menuEl} use:float={{ anchor: optionsBtn, align: 'end' }}>
    {#if $searchSortKind}
      <SortOptions on:pick={(e) => pickSort(e.detail)} />
      <div class="sep" role="separator"></div>
    {/if}
    <span class="menu-label">{$t('settingsPanel.label.rowLayout')}</span>
    {#each templates as template (template)}
      <button
        type="button"
        class="opt"
        role="menuitemradio"
        aria-checked={$prefs.rowTemplate === template}
        on:click={() => setRowTemplate(template)}
      >
        <span class="check">{#if $prefs.rowTemplate === template}<Ph svg={checkIcon} size={13} />{/if}</span>
        <span class="name">{$t(`onboarding.row.${template}`)}</span>
      </button>
    {/each}
    <div class="sep" role="separator"></div>
    <button
      type="button"
      class="opt"
      role="menuitemcheckbox"
      aria-checked={$prefs.rowShowSnippet}
      on:click={() => setRowShowSnippet(!$prefs.rowShowSnippet)}
    >
      <span class="check">{#if $prefs.rowShowSnippet}<Ph svg={checkIcon} size={13} />{/if}</span>
      <span class="name">{$t('settingsPanel.toggle.showMessagePreview')}</span>
    </button>
  </div>
{/if}

<style>
  .header {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .titles {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .counts {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .dot {
    width: var(--space-1);
    height: var(--space-1);
    border-radius: 50%;
    background: var(--text-tertiary);
  }

  .unread {
    color: var(--text-secondary);
  }

  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
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

  .icon-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-btn.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .menu {
    z-index: 70;
    width: var(--popover-width);
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

  .menu-label {
    display: block;
    padding: var(--space-2) var(--space-3) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
  }

  .opt {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .opt:hover,
  .opt:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .check {
    display: inline-flex;
    justify-content: center;
    width: var(--space-5);
    flex-shrink: 0;
    color: var(--accent);
  }

  .name {
    flex: 1;
  }

  .sep {
    height: var(--hairline);
    margin: var(--space-1) var(--space-3);
    background: var(--border-subtle);
  }
</style>
