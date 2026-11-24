<script lang="ts">
  // the spaces of the navigation as a list: drag a row by its grip, or move it
  // with the buttons or alt and an arrow on the grip, and switch each space in
  // or out of the navigation. mail can move but always stays.
  import { tick } from 'svelte'
  import { flip } from 'svelte/animate'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import upIcon from '@phosphor-icons/core/assets/duotone/caret-up-duotone.svg?raw'
  import downIcon from '@phosphor-icons/core/assets/duotone/caret-down-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { orderedSpaces, moveSpaceListed, reorderListedSpaces, setSpaceHidden, type NavEntry } from '../../lib/spaces'
  import { canHide } from '../../lib/spaceorder'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { dur, motion } from '../../lib/motion'
  import { tip } from '../../lib/tip'
  import { t, isMac } from '../../lib/i18n'
  import type { Space } from '../../lib/types'

  let list: HTMLDivElement

  async function move(key: Space, delta: number, refocus: string): Promise<void> {
    moveSpaceListed(key, delta)
    await tick()
    const row = list.querySelector<HTMLElement>(`[data-reorder-id="${key}"]`)
    const target = row?.querySelector<HTMLElement>(refocus)
    // a move button that just went disabled at the end of the list hands
    // focus to the grip, so the keyboard keeps its place.
    if (target && !(target as HTMLButtonElement).disabled) {
      target.focus()
    } else {
      row?.querySelector<HTMLElement>('.grip')?.focus()
    }
  }

  function onGripKeydown(event: KeyboardEvent, entry: NavEntry): void {
    if (!event.altKey || (event.key !== 'ArrowUp' && event.key !== 'ArrowDown')) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    void move(entry.key, event.key === 'ArrowUp' ? -1 : 1, '.grip')
  }

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    reorderListedSpaces(event.detail.ids)
  }

  $: last = $orderedSpaces.length - 1
</script>

<div class="spaces" bind:this={list} use:reorder on:reorder={onReorder}>
  {#each $orderedSpaces as entry, i (entry.key)}
    {@const name = $t(entry.labelKey)}
    <div class="space" class:off={entry.hidden} data-reorder-id={entry.key} animate:flip={{ duration: dur(motion.base) }}>
      <button
        type="button"
        class="grip"
        data-reorder-handle
        aria-label={$t('spaces.settings.reorder').replace('{name}', name)}
        use:tip={{ text: $t('spaces.settings.dragHint').replace('{alt}', isMac ? 'Option' : 'Alt'), side: 'top' }}
        on:keydown={(e) => onGripKeydown(e, entry)}
      >
        <Ph svg={gripIcon} size={14} />
      </button>
      <span class="icon"><Ph svg={entry.icon} size={16} /></span>
      <span class="name">{name}</span>
      {#if !canHide(entry.key)}
        <span class="meta">{$t('spaces.settings.alwaysShown')}</span>
      {:else if entry.hidden}
        <span class="meta">{$t('spaces.settings.hiddenHint')}</span>
      {/if}
      <div class="moves">
        <button
          type="button"
          class="move up"
          disabled={i === 0}
          aria-label={$t('spaces.settings.moveUp').replace('{name}', name)}
          on:click={() => move(entry.key, -1, '.up')}
        >
          <Ph svg={upIcon} size={14} />
        </button>
        <button
          type="button"
          class="move down"
          disabled={i === last}
          aria-label={$t('spaces.settings.moveDown').replace('{name}', name)}
          on:click={() => move(entry.key, 1, '.down')}
        >
          <Ph svg={downIcon} size={14} />
        </button>
      </div>
      <ToggleSwitch
        checked={!entry.hidden}
        disabled={!canHide(entry.key)}
        label={$t('spaces.settings.show').replace('{name}', name)}
        on:change={(e) => setSpaceHidden(entry.key, !e.detail)}
      />
    </div>
  {/each}
</div>

<style>
  .spaces {
    display: flex;
    flex-direction: column;
  }

  .space {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + 2 * var(--space-2));
    padding: var(--space-2) var(--space-4) var(--space-2) var(--space-2);
  }

  .space + .space {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .grip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--space-5);
    height: var(--control-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: grab;
  }

  .grip:hover {
    background: var(--surface-hover);
    color: var(--text-secondary);
  }

  .grip:active {
    cursor: grabbing;
  }

  .icon {
    display: inline-flex;
    color: var(--text-secondary);
  }

  .name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .off .icon,
  .off .name {
    color: var(--text-tertiary);
  }

  .meta {
    min-width: 0;
    overflow: hidden;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .moves {
    display: inline-flex;
    gap: var(--space-1);
    margin-inline-start: auto;
  }

  .move {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .move:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .move:disabled {
    opacity: 0.4;
    cursor: default;
  }
</style>
