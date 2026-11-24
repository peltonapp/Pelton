<script lang="ts">
  // the add menu, opened with "/" or a plus button: the palette's items in a
  // searchable list under the caret, picked with the arrow keys and Enter.
  // inside a condition group it lists only conditions.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import { t } from '../../../lib/i18n'
  import { paletteGroups, type PaletteItem } from './palette'
  import '../../../theme/controls.css'

  /** The control or block the menu hangs off. */
  export let anchor: HTMLElement
  /** Everything that can be added. */
  export let items: PaletteItem[]
  /** Limits the menu to conditions. */
  export let conditionsOnly = false

  const dispatch = createEventDispatcher<{ pick: PaletteItem; close: void }>()

  let query = ''
  let active = 0
  let input: HTMLInputElement
  let list: HTMLUListElement

  $: groups = paletteGroups(items, query, $t, conditionsOnly ? 'conditions' : undefined)
  $: flat = groups.flatMap((g) => g.items).filter((i) => !i.unavailable)
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
      dispatch('pick', flat[active])
    }
  }
</script>

<AnchoredPopover {anchor} label={$t('scripts.blocks.insert')} align="start" on:close={() => dispatch('close')}>
  <div class="menu">
    <input
      bind:this={input}
      bind:value={query}
      class="cal-field"
      type="text"
      role="combobox"
      aria-expanded="true"
      aria-controls="block-insert-list"
      aria-activedescendant={flat[active] ? `insert-${flat[active].key}` : undefined}
      aria-label={$t('scripts.blocks.palette.search')}
      placeholder={$t(conditionsOnly ? 'scripts.blocks.insert.conditions' : 'scripts.blocks.insert.placeholder')}
      on:keydown={onKey}
    />
    {#if groups.length === 0}
      <p class="none">{$t('scripts.blocks.palette.none')}</p>
    {:else}
      <ul class="list" id="block-insert-list" role="listbox" aria-label={$t('scripts.blocks.insert')} bind:this={list}>
        {#each groups as g (g.key)}
          <li class="head" role="presentation">{g.label}</li>
          {#each g.items as item (item.key)}
            {@const i = flat.indexOf(item)}
            <li
              id={`insert-${item.key}`}
              class="item"
              class:active={i === active && i >= 0}
              class:off={!!item.unavailable}
              role="option"
              aria-selected={i === active}
              aria-disabled={!!item.unavailable}
              aria-describedby={item.unavailable || item.description ? `insert-desc-${item.key}` : undefined}
              title={item.unavailable || undefined}
              on:pointerdown|preventDefault={() => !item.unavailable && dispatch('pick', item)}
              on:pointermove={() => i >= 0 && (active = i)}
            >
              <span class="label">{item.label}</span>
              {#if item.unavailable || item.description}<span class="desc" id={`insert-desc-${item.key}`}>{item.unavailable || item.description}</span>{/if}
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
    flex-direction: column;
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .item.active {
    background: var(--selection-bg);
  }

  .item.off {
    color: var(--text-tertiary);
    cursor: default;
  }

  .label {
    font-size: var(--fz-label);
  }

  .desc {
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
