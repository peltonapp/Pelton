<script lang="ts">
  // one selectable row in the sidebar: an optional disclosure caret, an icon, a
  // label, and an unread count. active rows use the accent selection background;
  // unread is shown by weight and the count, never by accent color.
  import { createEventDispatcher } from 'svelte'
  import { IconChevronRight } from '@tabler/icons-svelte'
  import DragGrip from './DragGrip.svelte'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'

  export let label: string
  // hover text for the row, when the label alone is ambiguous (two accounts can
  // both have a folder called Archive in the Pinned group). empty means none.
  export let title: string = ''
  export let count: number = 0
  export let active: boolean = false
  export let depth: number = 0
  export let expandable: boolean = false
  export let expanded: boolean = false
  // show the drag handle. the row only becomes draggable while the pointer is
  // over that handle, so an ordinary click still selects instead of dragging.
  export let reorderable: boolean = false
  // identifies this row to the reorder action on the enclosing container. null
  // when the row is not part of a reorderable group.
  export let reorderId: string | number | null = null

  const dispatch = createEventDispatcher<{ select: void; toggle: void; contextmenu: MouseEvent }>()

  // indent nested folders by depth. the base inset keeps the caret aligned.
  $: indent = `calc(var(--space-3) + ${depth} * var(--space-4))`
  // one vertical guide line per ancestor level when the user enables them, each
  // aligned under that ancestor's caret. top-level rows (inbox, bin, and so on)
  // have no ancestor, but still draw their own level-0 guide so the tree reads
  // consistently from the root down.
  $: guides = $prefs.sidebarIndentGuides ? Array.from({ length: Math.max(depth, 1) }, (_, i) => i) : []
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="row"
  class:active
  data-reorder-id={reorderId}
  style={`padding-inline-start:${indent}`}
  on:contextmenu|preventDefault={(e) => dispatch('contextmenu', e)}
>
  {#each guides as level (level)}
    <span
      class="guide"
      aria-hidden="true"
      style={`left:calc(var(--space-3) + ${level} * var(--space-4) + 9px)`}
    ></span>
  {/each}
  {#if expandable}
    <button
      type="button"
      class="caret"
      class:open={expanded}
      aria-label={(expanded ? $t('a11y.collapseNamed') : $t('a11y.expandNamed')).replace('{name}', label)}
      aria-expanded={expanded}
      on:click|stopPropagation={() => dispatch('toggle')}
    >
      <IconChevronRight size={14} stroke={1.8} />
    </button>
  {:else}
    <span class="caret-spacer" aria-hidden="true"></span>
  {/if}

  <button
    type="button"
    class="main"
    class:unread={count > 0}
    aria-current={active ? 'true' : undefined}
    title={title || null}
    on:click={() => dispatch('select')}
  >
    <span class="icon" aria-hidden="true"><slot /></span>
    <span class="label">{label}</span>
    <slot name="badge" />
    {#if count > 0}
      {#key count}
        <span class="count" role="img" aria-label={`${count} ${$t('sidebar.unreadSuffix')}`}>{count}</span>
      {/key}
    {/if}
  </button>

  {#if reorderable}
    <DragGrip />
  {/if}
</div>

<style>
  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding-inline-end: var(--space-3);
    border-radius: var(--radius-control);
  }

  /* vertical indent guide for a nested folder's ancestor level. a fixed 1px keeps
     it crisp at the fractional x-position the calc lands on (a sub-pixel hairline
     there renders nearly invisible), and border-default reads clearly in both
     themes. */
  .guide {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--border-default);
    pointer-events: none;
  }

  .row:not(.active):hover {
    background: var(--surface-hover);
  }

  .row.active {
    background: var(--selection-bg);
  }

  .caret,
  .caret-spacer {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 22px;
    flex-shrink: 0;
  }

  .caret {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  .caret :global(svg) {
    transition: transform 0.12s ease;
  }

  .caret.open :global(svg) {
    transform: rotate(90deg);
  }

  .main {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    padding: var(--row-pad-y) var(--space-2);
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
    color: var(--text-tertiary);
    flex-shrink: 0;
  }

  .row.active .icon {
    color: var(--accent);
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
    font-variant-numeric: tabular-nums;
  }

  /* the new design: rows ease between states, and a count that changes ticks
     into place instead of swapping. */
  :global(:root[data-design='new']) .row {
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  :global(:root[data-design='new']) .caret :global(svg) {
    transition: transform var(--motion-base) var(--ease-out);
  }

  :global(:root[data-design='new']) .count {
    animation: tick var(--motion-base) var(--ease-out);
  }

  @keyframes tick {
    from {
      opacity: 0;
      transform: translateY(-3px);
    }
  }
</style>
