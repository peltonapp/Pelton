<script lang="ts">
  // the split inbox tab strip above the message list. Each tab shows its
  // unread count; a thin indicator slides under the active one. The sliders
  // button at the end opens the split editor.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import slidersIcon from '@phosphor-icons/core/assets/duotone/sliders-horizontal-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import SplitEditor from './SplitEditor.svelte'
  import { float } from '../../lib/floating'
  import { t } from '../../lib/i18n'
  import type { Selection } from '../../lib/types'
  import { prefs } from '../../stores/prefs'
  import { sidebar } from '../../stores/accounts'
  import { views } from '../../stores/views'
  import { activeSplit, refreshSplitCounts, splitKeys, splitTabCounts, splitViewId } from '../../stores/splits'

  /** The inbox the tabs split. */
  export let selection: Selection

  const dispatch = createEventDispatcher<{ select: string }>()

  $: keys = splitKeys($prefs.splitTabs, $views)
  $: active = activeSplit($prefs.splitActive, keys)
  $: unreadByKey = Object.fromEntries($splitTabCounts.map((tab) => [tab.key, tab.unread]))

  // the counts follow the selection, the tab order and every sidebar refresh,
  // which is what runs after mail is read, moved or synced.
  $: refresh(selection, $prefs.splitTabs, $sidebar.data)

  function refresh(sel: Selection, _tabs: unknown, _sidebar: unknown): void {
    void refreshSplitCounts(sel)
  }

  function nameOf(key: string): string {
    const id = splitViewId(key)
    if (id !== null) {
      return $views.find((v) => v.id === id)?.name ?? key
    }
    return $t(`split.tab.${key}`)
  }

  let stripEl: HTMLDivElement | undefined
  let indicatorLeft = 0
  let indicatorWidth = 0
  // the first placement jumps into place; only later moves slide.
  let placed = false
  let sliding = false

  $: void placeIndicator(active, keys, $splitTabCounts)

  async function placeIndicator(key: string, _keys: unknown, _counts: unknown): Promise<void> {
    await tick()
    const el = [...(stripEl?.querySelectorAll<HTMLElement>('[data-split]') ?? [])].find((tab) => tab.dataset.split === key)
    if (!el) {
      return
    }
    sliding = placed
    indicatorLeft = el.offsetLeft
    indicatorWidth = el.offsetWidth
    placed = true
    el.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
  }

  let editBtn: HTMLButtonElement
  let editorEl: HTMLDivElement | undefined
  let editing = false

  function onOutside(event: PointerEvent): void {
    const target = event.target as Node
    if (editing && !editorEl?.contains(target) && !editBtn.contains(target)) {
      editing = false
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (editing && event.key === 'Escape') {
      event.stopPropagation()
      editing = false
      editBtn.focus()
    }
  }

  document.addEventListener('pointerdown', onOutside, true)
  onDestroy(() => document.removeEventListener('pointerdown', onOutside, true))
</script>

<svelte:window on:keydown={onKey} on:resize={() => void placeIndicator(active, keys, $splitTabCounts)} />

<div class="splits">
  <div class="strip" role="tablist" aria-label={$t('split.tabs')} bind:this={stripEl}>
    {#each keys as key (key)}
      {@const unread = unreadByKey[key] ?? 0}
      <button
        type="button"
        class="tab"
        class:active={key === active}
        role="tab"
        tabindex={key === active ? 0 : -1}
        aria-selected={key === active}
        data-split={key}
        on:click={() => dispatch('select', key)}
      >
        <span class="name">{nameOf(key)}</span>
        {#if unread > 0}
          <span class="count" aria-label={$t('split.unread').replace('{count}', String(unread))}>{unread.toLocaleString()}</span>
        {/if}
      </button>
    {/each}
    <span
      class="indicator"
      class:sliding
      class:shown={placed}
      style={`transform: translateX(${indicatorLeft}px); width: ${indicatorWidth}px`}
      aria-hidden="true"
    ></span>
  </div>
  <button
    type="button"
    class="edit"
    class:on={editing}
    aria-label={$t('split.edit')}
    title={$t('split.edit')}
    aria-haspopup="dialog"
    aria-expanded={editing}
    bind:this={editBtn}
    on:click={() => (editing = !editing)}
  >
    <Ph svg={slidersIcon} size={14} />
  </button>
</div>

{#if editing}
  <div class="popover" role="dialog" aria-label={$t('split.editor.title')} bind:this={editorEl} use:float={{ anchor: editBtn, align: 'end' }}>
    <span class="title">{$t('split.editor.title')}</span>
    <SplitEditor />
  </div>
{/if}

<style>
  .splits {
    display: flex;
    align-items: stretch;
    gap: var(--space-1);
    min-width: 0;
    padding-inline: var(--space-2) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .strip {
    position: relative;
    flex: 1;
    min-width: 0;
    display: flex;
    overflow-x: auto;
    scrollbar-width: none;
  }

  .strip::-webkit-scrollbar {
    display: none;
  }

  .tab {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  .tab:hover {
    color: var(--text-primary);
  }

  .tab.active {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .count {
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }

  /* offsetLeft counts from the left edge in either direction, so the
     indicator does too. */
  .indicator {
    position: absolute;
    left: 0;
    bottom: 0;
    height: calc(var(--hairline) * 4);
    border-radius: var(--radius-control);
    background: var(--accent);
    opacity: 0;
    pointer-events: none;
  }

  .indicator.shown {
    opacity: 1;
  }

  .indicator.sliding {
    transition:
      transform var(--motion-base) var(--ease-out),
      width var(--motion-base) var(--ease-out);
  }

  .edit {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    align-self: center;
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

  .edit:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .edit.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .popover {
    z-index: 70;
    width: var(--popover-width);
    padding: var(--space-3);
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

  .title {
    display: block;
    margin-bottom: var(--space-2);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }
</style>
