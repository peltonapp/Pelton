<script lang="ts">
  // the palette beside the canvas: every condition, action and control block
  // the catalog offers, grouped and searchable. an item is dragged onto the
  // canvas or added where the caret is with a click or Enter.
  import { createEventDispatcher } from 'svelte'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import riskIcon from '@phosphor-icons/core/assets/duotone/shield-warning-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'
  import { getEditor } from './context'
  import { paletteGroups, type PaletteItem } from './palette'
  import '../../../theme/controls.css'

  /** Everything the palette can offer. */
  export let items: PaletteItem[]

  const dispatch = createEventDispatcher<{ add: PaletteItem }>()
  const ed = getEditor()

  let query = ''
  let why = ''
  let listEl: HTMLDivElement

  $: groups = paletteGroups(items, query, $t)

  function onListKey(e: KeyboardEvent): void {
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') {
      return
    }
    const buttons = [...listEl.querySelectorAll<HTMLButtonElement>('button.item')]
    const at = buttons.indexOf(document.activeElement as HTMLButtonElement)
    const next = buttons[at + (e.key === 'ArrowDown' ? 1 : -1)]
    if (next) {
      e.preventDefault()
      next.focus()
    }
  }

  function onSearchKey(e: KeyboardEvent): void {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      listEl.querySelector<HTMLButtonElement>('button.item')?.focus()
    } else if (e.key === 'Enter') {
      const first = groups.flatMap((g) => g.items).find((i) => !i.unavailable)
      if (first) {
        e.preventDefault()
        dispatch('add', first)
      }
    }
  }

  function press(e: PointerEvent, item: PaletteItem): void {
    if (!item.unavailable) {
      ed.startDrag(e, { kind: 'palette', item }, item.label)
    }
  }
</script>

<aside class="palette" aria-label={$t('scripts.blocks.palette')}>
  <label class="search">
    <Ph svg={searchIcon} size={14} />
    <input class="find" type="search" bind:value={query} placeholder={$t('scripts.blocks.palette.search')} aria-label={$t('scripts.blocks.palette.search')} on:keydown={onSearchKey} />
  </label>
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <div class="groups" bind:this={listEl} on:keydown={onListKey}>
    {#each groups as g (g.key)}
      <section class="group" aria-label={g.label}>
        <h4 class="head">{g.label}</h4>
        {#each g.items as item (item.key)}
          <button
            type="button"
            class="item {item.section}"
            aria-disabled={!!item.unavailable}
            aria-describedby={item.unavailable ? `palette-why-${item.key}` : undefined}
            title={item.unavailable || item.description || undefined}
            on:pointerdown={(e) => press(e, item)}
            on:click={() => !item.unavailable && dispatch('add', item)}
            on:focus={() => (why = item.unavailable || '')}
            on:blur={() => (why = '')}
            on:mouseenter={() => (why = item.unavailable || '')}
            on:mouseleave={() => (why = '')}
          >
            <span class="dot" aria-hidden="true"></span>
            <span class="label">{item.label}</span>
            {#if item.risky}<span class="risk" aria-label={$t('scripts.blocks.risky')}><Ph svg={riskIcon} size={12} /></span>{/if}
          </button>
          {#if item.unavailable}<span class="sr-only" id={`palette-why-${item.key}`}>{item.unavailable}</span>{/if}
        {/each}
      </section>
    {:else}
      <p class="none">{$t('scripts.blocks.palette.none')}</p>
    {/each}
  </div>
  {#if why}<p class="why" aria-hidden="true">{why}</p>{/if}
</aside>

<style>
  .palette {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    gap: var(--space-2);
  }

  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 var(--space-2);
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-tertiary);
  }

  .search:focus-within {
    border-color: var(--accent);
  }

  .find {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
  }

  .groups {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .group {
    display: flex;
    flex-direction: column;
  }

  .head {
    margin: 0 0 var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding: 0 var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: grab;
    touch-action: none;
  }

  .item:hover {
    background: var(--surface-hover);
  }

  .item:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .item[aria-disabled='true'] {
    color: var(--text-tertiary);
    cursor: default;
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    border-radius: var(--radius-control);
    background: var(--border-strong);
  }

  .item.actions .dot {
    background: var(--accent);
  }

  .item.conditions .dot {
    background: color-mix(in srgb, var(--accent) 45%, var(--border-strong));
  }

  .label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .risk {
    display: inline-flex;
    color: var(--danger);
  }

  .why {
    margin: 0;
    padding: var(--space-1) var(--space-2);
    border-top: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .none {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
