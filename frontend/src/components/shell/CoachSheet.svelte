<script lang="ts">
  // the sheet of keyboard shortcuts: groups of what each key does, for what is
  // on screen. held open by the modifier it is a passing glance that takes no
  // focus and no clicks; pinned by its shortcut it is a dialog with a filter.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { fade } from 'svelte/transition'
  import keyboardIcon from '@phosphor-icons/core/assets/duotone/keyboard-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { SheetGroup } from '../../lib/coach'

  /** The groups to show. */
  export let groups: SheetGroup[]
  /** Pinned open by its shortcut, with a filter field, rather than held. */
  export let pinned: boolean = false
  /** The filter text, while pinned. */
  export let query: string = ''
  /** The key that pins the sheet open, as shown, or empty when unbound. */
  export let pinKey: string = ''

  const dispatch = createEventDispatcher<{ close: void }>()

  let panel: HTMLDivElement
  let input: HTMLInputElement

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      dispatch('close')
    }
  }

  function onPointerDown(event: PointerEvent): void {
    const target = event.target as Node | null
    if (target && !panel?.contains(target)) {
      dispatch('close')
    }
  }

  onMount(() => {
    if (!pinned) {
      return
    }
    pushPopup()
    window.addEventListener('pointerdown', onPointerDown, true)
    void tick().then(() => input?.focus())
  })

  onDestroy(() => {
    if (pinned) {
      popPopup()
      window.removeEventListener('pointerdown', onPointerDown, true)
    }
  })
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<div
  class="sheet"
  class:pinned
  bind:this={panel}
  role={pinned ? 'dialog' : 'region'}
  aria-modal={pinned ? 'true' : undefined}
  aria-label={$t('coach.sheet.title')}
  on:keydown={pinned ? onKeydown : undefined}
  transition:fade={{ duration: dur(motion.base) }}
>
  <header>
    <span class="title">
      <Ph svg={keyboardIcon} size={15} />
      {$t('coach.sheet.title')}
    </span>
    {#if pinned}
      <label class="filter">
        <Ph svg={searchIcon} size={13} />
        <input
          bind:this={input}
          bind:value={query}
          type="search"
          placeholder={$t('coach.sheet.filter')}
          aria-label={$t('coach.sheet.filter')}
        />
      </label>
      <button type="button" class="c2-tool compact" aria-label={$t('coach.sheet.close')} on:click={() => dispatch('close')}>
        <Ph svg={xIcon} size={13} />
      </button>
    {/if}
  </header>

  <div class="groups">
    {#each groups as group (group.id)}
      <section aria-label={$t(group.titleKey)}>
        <h3>{$t(group.titleKey)}</h3>
        <ul>
          {#each group.items as item (item.id)}
            <li>
              <span class="label">{$t(item.labelKey)}</span>
              <span class="keys">
                {#each item.keys as key}
                  <kbd>{key}</kbd>
                {/each}
              </span>
            </li>
          {/each}
        </ul>
      </section>
    {:else}
      <p class="empty">{$t('coach.sheet.none')}</p>
    {/each}
  </div>

  {#if !pinned}
    <footer>
      {$t('coach.sheet.holdHint')}
      {#if pinKey}
        {$t('coach.sheet.pinHint').replace('{key}', pinKey)}
      {/if}
    </footer>
  {/if}
</div>

<style>
  .sheet {
    position: fixed;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    z-index: 900;
    display: flex;
    flex-direction: column;
    width: var(--coach-sheet-width);
    max-width: calc(100% - 2 * var(--space-6));
    max-height: calc(100% - 2 * var(--space-6));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }

  .sheet:not(.pinned) {
    pointer-events: none;
  }

  header {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-height);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }

  .filter {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    width: var(--coach-filter-width);
    max-width: 50%;
    height: calc(var(--control-height) - var(--space-2));
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
  }

  .filter:focus-within {
    border-color: var(--border-strong);
  }

  .filter input {
    flex: 1;
    min-width: 0;
    border: none;
    outline: none;
    background: transparent;
    color: var(--text-primary);
    font-family: inherit;
    font-size: var(--fz-label);
  }

  .groups {
    columns: var(--coach-sheet-columns);
    column-gap: var(--space-6);
    padding: var(--space-4) var(--space-5);
    overflow-y: auto;
  }

  section {
    break-inside: avoid;
    margin-block-end: var(--space-5);
  }

  h3 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-1) 0;
    font-size: var(--fz-label);
  }

  .label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
  }

  .keys {
    display: inline-flex;
    gap: var(--space-1);
    flex-shrink: 0;
  }

  kbd {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: var(--chip-height);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-primary);
  }

  .empty {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  footer {
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
