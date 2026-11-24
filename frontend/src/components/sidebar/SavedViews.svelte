<script lang="ts">
  // the saved Views (preset searches) group in the sidebar. each row shows the
  // view's colored icon, name and match/unread count; hovering reveals edit and
  // delete. rows drag to reorder. a "new view" action sits under the list. the
  // group is only rendered when the user has enabled Views (placement != hidden).
  import { createEventDispatcher } from 'svelte'
  import { dndzone, type DndEvent } from 'svelte-dnd-action'
  import { IconPlus, IconPencil, IconTrash, IconPin, IconPinnedOff } from '@tabler/icons-svelte'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { prefs, railPinIds, toggleRailPin } from '../../stores/prefs'
  import type { View } from '../../lib/types'
  import { viewIconComponent, viewColorCss } from '../../lib/viewicons'
  import { selection, selectSavedView, leaveDeletedSavedView } from '../../stores/selection'
  import { reorderViews, deleteView } from '../../lib/api'
  import { loadViews } from '../../stores/views'
  import { askConfirm } from '../../stores/confirm'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  export let views: View[] = []

  const dispatch = createEventDispatcher<{ new: void; edit: View }>()
  const flipMs = 150

  // local drag copy so the list animates during a drag without mutating the store.
  let order: View[] = []
  $: order = views

  function choose(v: View): void {
    selectSavedView(v.id, v.name)
  }

  // right-click offers what the row's buttons do, and in the new design a
  // place on the rail.
  function onContext(event: MouseEvent, v: View): void {
    const entries: MenuEntry[] = []
    if ($prefs.design !== 'classic') {
      const id = `v:${v.id}`
      const pinned = railPinIds($prefs.railPins).includes(id)
      entries.push({
        label: pinned ? $t('rail.unpin') : $t('rail.pin'),
        icon: pinned ? IconPinnedOff : IconPin,
        action: () => toggleRailPin(id),
      })
    }
    entries.push(
      { label: $t('views.edit'), icon: IconPencil, action: () => dispatch('edit', v) },
      { label: $t('views.delete'), icon: IconTrash, danger: true, action: () => void remove(v) },
    )
    openContextMenu(event.clientX, event.clientY, entries)
  }

  function consider(e: CustomEvent<DndEvent<View>>): void {
    order = e.detail.items
  }

  async function finalize(e: CustomEvent<DndEvent<View>>): Promise<void> {
    order = e.detail.items
    try {
      await reorderViews(order.map((v) => v.id))
    } catch (err) {
      toastError(errorMessage(err))
      await loadViews()
    }
  }

  async function remove(v: View): Promise<void> {
    const ok = await askConfirm({
      title: $t('views.deleteConfirmTitle').replace('{name}', v.name),
      body: $t('views.deleteConfirmBody'),
      confirmLabel: $t('views.deleteConfirmAction'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await deleteView(v.id)
      leaveDeletedSavedView(v.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<nav class="views" aria-label={$t('views.ariaLabel')}>
  <header class="group-head">{$t('views.heading')}</header>

  <div
    class="list"
    use:dndzone={{ items: order, type: 'saved-views', flipDurationMs: flipMs, dropTargetStyle: {} }}
    on:consider={consider}
    on:finalize={finalize}
  >
    {#each order as view (view.id)}
      {@const active = $selection.kind === 'savedView' && $selection.viewId === view.id}
      <!-- svelte-ignore a11y-no-static-element-interactions -->
      <div class="row" class:active on:contextmenu|preventDefault={(e) => onContext(e, view)}>
        <button type="button" class="main" class:unread={view.unreadCount > 0} on:click={() => choose(view)}>
          <span class="icon" style={`color:${viewColorCss(view.color)}`} aria-hidden="true">
            <svelte:component this={viewIconComponent(view.icon)} size={15} stroke={1.7} />
          </span>
          <span class="label">{view.name}</span>
          {#if view.totalCount > 0}
            <span class="count">{view.unreadCount > 0 ? view.unreadCount : view.totalCount}</span>
          {/if}
        </button>
        <div class="actions">
          <button
            type="button"
            class="act"
            aria-label={$t('views.edit')}
            title={$t('views.edit')}
            on:click|stopPropagation={() => dispatch('edit', view)}
          >
            <IconPencil size={13} stroke={1.7} />
          </button>
          <button
            type="button"
            class="act"
            aria-label={$t('views.delete')}
            title={$t('views.delete')}
            on:click|stopPropagation={() => remove(view)}
          >
            <IconTrash size={13} stroke={1.7} />
          </button>
        </div>
      </div>
    {/each}
  </div>

  <button type="button" class="new" on:click={() => dispatch('new')}>
    <IconPlus size={14} stroke={1.8} />
    <span>{$t('views.new')}</span>
  </button>
</nav>

<style>
  .views {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .group-head {
    padding: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    border-radius: var(--radius-control);
  }

  .row:not(.active):hover {
    background: var(--surface-hover);
  }

  .row.active {
    background: var(--selection-bg);
  }

  .main {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    padding: var(--row-pad-y) var(--space-2) var(--row-pad-y) var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    text-align: start;
    font-size: var(--fz-list);
    line-height: 1.2;
  }

  .row.active .main {
    color: var(--text-primary);
  }

  .main.unread {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .icon {
    display: inline-flex;
    flex-shrink: 0;
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    color: var(--text-tertiary);
    background: var(--surface-sunken);
    border-radius: 999px;
    padding: 0 var(--space-2);
    min-width: 18px;
    text-align: center;
  }

  .actions {
    display: none;
    align-items: center;
    gap: 1px;
    padding-inline-end: var(--space-2);
  }

  .row:hover .actions {
    display: flex;
  }

  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .act:hover {
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .new {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-1) var(--space-2) 0;
    padding: var(--row-pad-y) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-list);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }

  .new:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
