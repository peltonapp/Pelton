<script lang="ts">
  // picks the quick buttons a message row shows and their order. The shown
  // ones come first in their order and move by their handle, with the pointer
  // or the arrow keys; the rest follow and switch on until five are shown.
  import { createEventDispatcher, tick } from 'svelte'
  import gripIcon from '@phosphor-icons/core/assets/duotone/dots-six-vertical-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { reorder, reorderHandleAttr, type ReorderDetail } from '../../lib/reorder'
  import {
    isRowAction,
    maxRowActions,
    moveRowAction,
    parseRowActions,
    rowActionIds,
    rowActionSettingKey,
    rowActionView,
    serializeRowActions,
    toggleRowAction,
    type RowActionId,
  } from '../../lib/rowactions'
  import { t } from '../../lib/i18n'

  /** The stored list, comma separated. */
  export let value: string
  /** Dims the editor while quick actions are off. */
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ change: string }>()

  const plainState = { seen: true, flagged: false, pinned: false, muted: false, inSpam: false }

  $: shown = parseRowActions(value)
  $: hidden = rowActionIds.filter((id) => !shown.includes(id))
  $: full = shown.length >= maxRowActions

  let listEl: HTMLOListElement

  function save(ids: RowActionId[]): void {
    dispatch('change', serializeRowActions(ids))
  }

  function nameOf(id: RowActionId): string {
    return $t(rowActionSettingKey(id))
  }

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    save(event.detail.ids.filter(isRowAction))
  }

  // the handle moves its button with the arrow keys and keeps the focus on
  // it, so a run of presses walks it through the list.
  async function onGripKey(event: KeyboardEvent, index: number, id: RowActionId): Promise<void> {
    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') {
      return
    }
    event.preventDefault()
    const by = event.key === 'ArrowUp' ? -1 : 1
    const next = moveRowAction(shown, index, by)
    if (next.join() === shown.join()) {
      return
    }
    save(next)
    await tick()
    listEl?.querySelector<HTMLElement>(`[data-reorder-id="${id}"] [${reorderHandleAttr}]`)?.focus()
  }
</script>

<div class="editor" class:disabled>
  <ol class="list" bind:this={listEl} use:reorder on:reorder={onReorder} aria-label={$t('rowActions.choose')}>
    {#each shown as id, i (id)}
      {@const view = rowActionView(id, plainState)}
      <li class="item" data-reorder-id={id}>
        <button
          type="button"
          class="grip"
          {...{ [reorderHandleAttr]: '' }}
          aria-label={$t('rowActions.grip').replace('{name}', nameOf(id))}
          title={$t('rowActions.grip').replace('{name}', nameOf(id))}
          {disabled}
          on:keydown={(e) => void onGripKey(e, i, id)}
        >
          <Ph svg={gripIcon} size={14} />
        </button>
        <span class="glyph" aria-hidden="true"><Ph svg={view.icon} size={15} /></span>
        <span class="name">{nameOf(id)}</span>
        <ToggleSwitch checked label={nameOf(id)} {disabled} on:change={() => save(toggleRowAction(shown, id, false))} />
      </li>
    {/each}
  </ol>
  {#if full}
    <p class="note">{$t('rowActions.full')}</p>
  {/if}
  <ul class="list">
    {#each hidden as id (id)}
      {@const view = rowActionView(id, plainState)}
      <li class="item off">
        <span class="glyph" aria-hidden="true"><Ph svg={view.icon} size={15} /></span>
        <span class="name">{nameOf(id)}</span>
        <ToggleSwitch checked={false} label={nameOf(id)} disabled={disabled || full} on:change={() => save(toggleRowAction(shown, id, true))} />
      </li>
    {/each}
  </ul>
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    width: 100%;
  }

  .editor.disabled {
    opacity: 0.5;
  }

  .list {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .list:empty {
    display: none;
  }

  .list > li + li {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
    padding: 0 var(--space-3) 0 var(--space-1);
    background: var(--surface-raised);
  }

  .item.off {
    padding-inline-start: var(--space-3);
  }

  .grip {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: grab;
  }

  .grip:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .grip:active {
    cursor: grabbing;
  }

  .grip:disabled {
    cursor: default;
  }

  .glyph {
    display: inline-flex;
    color: var(--text-secondary);
  }

  .item.off .glyph {
    color: var(--text-tertiary);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .item.off .name {
    color: var(--text-secondary);
  }

  .note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
