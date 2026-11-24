<script lang="ts">
  // the menu of values a field can refer to instead of holding a literal:
  // the event's fields, variables, earlier steps' results and the script's
  // settings, each group only with what is in scope where the block stands.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import { t } from '../../../lib/i18n'
  import { entryText, type ScopeEntry, type ScopeGroup } from './scope'
  import '../../../theme/controls.css'

  /** The control the menu hangs off. */
  export let anchor: HTMLElement
  /** The entries that fit the field. */
  export let entries: ScopeEntry[]
  /** The reference chosen now, if any. */
  export let current = ''

  const dispatch = createEventDispatcher<{ pick: string; close: void }>()
  const order: ScopeGroup[] = ['event', 'var', 'step', 'setting']

  let query = ''
  let active = 0
  let input: HTMLInputElement
  let list: HTMLUListElement

  $: shown = entries.filter((e) => query.trim() === '' || entryText(e, $t).toLowerCase().includes(query.trim().toLowerCase()) || e.ref.includes(query.trim()))
  $: grouped = order.map((g) => ({ group: g, items: shown.filter((e) => e.group === g) })).filter((g) => g.items.length > 0)
  $: flat = grouped.flatMap((g) => g.items)
  $: if (active >= flat.length) {
    active = Math.max(0, flat.length - 1)
  }

  onMount(async () => {
    await tick()
    input?.focus()
  })

  function onKey(e: KeyboardEvent): void {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      active = (active + (e.key === 'ArrowDown' ? 1 : -1) + flat.length) % Math.max(1, flat.length)
      void tick().then(() => list?.querySelector('.active')?.scrollIntoView({ block: 'nearest' }))
    } else if (e.key === 'Enter' && flat[active]) {
      e.preventDefault()
      dispatch('pick', flat[active].ref)
    }
  }
</script>

<AnchoredPopover {anchor} label={$t('scripts.blocks.ref.menu')} align="start" on:close={() => dispatch('close')}>
  <div class="menu">
    <input
      bind:this={input}
      bind:value={query}
      class="cal-field find"
      type="text"
      role="combobox"
      aria-expanded="true"
      aria-controls="ref-menu-list"
      aria-activedescendant={flat[active] ? `ref-${flat[active].ref}` : undefined}
      aria-label={$t('scripts.blocks.ref.search')}
      placeholder={$t('scripts.blocks.ref.search')}
      on:keydown={onKey}
    />
    {#if flat.length === 0}
      <p class="none">{$t('scripts.blocks.ref.none')}</p>
    {:else}
      <ul class="list" id="ref-menu-list" role="listbox" aria-label={$t('scripts.blocks.ref.menu')} bind:this={list}>
        {#each grouped as g (g.group)}
          <li class="head" role="presentation">{$t(`scripts.blocks.ref.group.${g.group}`)}</li>
          {#each g.items as e (e.ref)}
            {@const i = flat.indexOf(e)}
            <li
              id={`ref-${e.ref}`}
              class="item"
              class:active={i === active}
              role="option"
              aria-selected={e.ref === current}
              on:pointerdown|preventDefault={() => dispatch('pick', e.ref)}
              on:pointermove={() => (active = i)}
            >
              <span class="text">{entryText(e, $t)}</span>
              {#if e.type}<span class="type">{e.type}</span>{/if}
            </li>
          {/each}
        {/each}
      </ul>
    {/if}
  </div>
</AnchoredPopover>

<style>
  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    width: var(--popover-width);
    min-height: 0;
    padding: var(--space-2);
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    overflow-y: auto;
    min-height: 0;
  }

  .head {
    padding: var(--space-2) var(--space-2) var(--space-1);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .item {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .item.active {
    background: var(--selection-bg);
  }

  .type {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .none {
    margin: 0;
    padding: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
