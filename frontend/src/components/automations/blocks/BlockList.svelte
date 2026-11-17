<script lang="ts">
  // one list of blocks: the top level, a branch of an if, an else or a for
  // each body. it is a drop target, shows where a dragged block would land,
  // and offers to add a block at its end.
  import plusIcon from '@phosphor-icons/core/assets/duotone/plus-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import BlockCard from './BlockCard.svelte'
  import { t } from '../../../lib/i18n'
  import type { Block } from '../../../lib/scripting'
  import { getEditor } from './context'
  import { sameSlot, slotKey, type Slot } from './tree'

  /** Which list this is. */
  export let slot: Slot
  /** Its blocks. */
  export let blocks: Block[]
  /** Accessible name of the list. */
  export let label: string

  const ed = getEditor()
  const { dropAt } = ed

  let addButton: HTMLButtonElement

  $: drop = $dropAt?.kind === 'block' && sameSlot($dropAt.place.slot, slot) ? $dropAt.place.index : -1
  $: nested = slot.parent !== null
</script>

<div class="slot" class:nested>
  <ul class="blocks" class:drop-target={drop >= 0 && blocks.length === 0} class:empty={blocks.length === 0} data-block-slot={slotKey(slot)} aria-label={label}>
    {#each blocks as block, i (block.id)}
      <li class="item" data-block-index={i} class:drop-before={drop === i} class:drop-after={drop === blocks.length && i === blocks.length - 1}>
        <BlockCard {block} />
      </li>
    {/each}
  </ul>
  {#if nested || blocks.length > 0}
    <button type="button" class="add" bind:this={addButton} on:click={() => ed.openInsert(addButton, { kind: 'block', place: { slot, index: blocks.length } })}>
      <Ph svg={plusIcon} size={13} />{$t(blocks.length === 0 && nested ? 'scripts.blocks.addHere' : 'scripts.blocks.add')}
    </button>
  {/if}
</div>

<style>
  .slot {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .slot.nested {
    padding-inline-start: var(--space-3);
    border-inline-start: 2px solid var(--border-subtle);
  }

  .blocks {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    border-radius: var(--radius-control);
  }

  .blocks.empty {
    min-height: var(--space-2);
  }

  .blocks.drop-target {
    min-height: var(--control-height);
    border: var(--hairline) dashed var(--accent);
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }

  .item {
    position: relative;
  }

  .item.drop-before::before,
  .item.drop-after::after {
    content: '';
    position: absolute;
    inset-inline: 0;
    height: calc(var(--hairline) * 2);
    background: var(--accent);
    z-index: 1;
  }

  .item.drop-before::before {
    top: calc(-1 * var(--space-1));
  }

  .item.drop-after::after {
    bottom: calc(-1 * var(--space-1));
  }

  .add {
    display: inline-flex;
    align-items: center;
    align-self: flex-start;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .add:hover {
    background: var(--surface-hover);
    color: var(--accent);
  }

  .add:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
