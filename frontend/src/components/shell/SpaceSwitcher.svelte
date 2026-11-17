<script lang="ts">
  // the space switcher as a horizontal segmented control, used where there is
  // no rail: at the foot of the sidebar and as tabs in the top bar. a highlight
  // slides under the active segment, and the segments can be dragged into a
  // new order.
  import { onDestroy, onMount, tick } from 'svelte'
  import { flip } from 'svelte/animate'
  import Ph from '../common/Ph.svelte'
  import { navSpaces as spaces, spaceDrop, spaceMenu, moveSpaceShown, reorderShownSpaces, type NavEntry } from '../../lib/spaces'
  import { reorder, type ReorderDetail } from '../../lib/reorder'
  import { dur, motion } from '../../lib/motion'
  import { maildrop } from '../../stores/maildrag'
  import { space, openSpace } from '../../stores/space'
  import { openContextMenu } from '../../stores/contextmenu'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'

  /** Shows each entry's name beside its icon. */
  export let labels: boolean = false

  let switcher: HTMLDivElement
  let dragging = false

  // the highlight sits under the active segment where it actually is, so it
  // stays right while segments are dragged or moved.
  let thumbX = 0
  let thumbW = 0
  let thumbReady = false

  function placeThumb(): void {
    const el = switcher?.querySelector<HTMLElement>('.segment.active')
    thumbW = el?.offsetWidth ?? 0
    thumbX = el?.offsetLeft ?? 0
    if (thumbW > 0 && !thumbReady) {
      requestAnimationFrame(() => (thumbReady = true))
    }
  }

  function after(fn: () => void, ..._deps: unknown[]): void {
    void tick().then(fn)
  }

  $: after(placeThumb, $space, $spaces, labels)

  let observer: MutationObserver | undefined
  let resize: ResizeObserver | undefined

  onMount(() => {
    observer = new MutationObserver(placeThumb)
    observer.observe(switcher, { childList: true })
    if (typeof ResizeObserver === 'function') {
      resize = new ResizeObserver(placeThumb)
      resize.observe(switcher)
    }
    placeThumb()
  })

  onDestroy(() => {
    observer?.disconnect()
    resize?.disconnect()
  })

  function onReorder(event: CustomEvent<ReorderDetail>): void {
    reorderShownSpaces(event.detail.ids)
  }

  function onContext(event: MouseEvent, entry: NavEntry): void {
    openContextMenu(event.clientX, event.clientY, spaceMenu(entry, 'x'))
  }

  // alt with an arrow moves the focused segment, and focus goes with it.
  async function onKeydown(event: KeyboardEvent, entry: NavEntry): Promise<void> {
    if (!event.altKey || entry.transient || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    const rtl = getComputedStyle(switcher).direction === 'rtl'
    const forward = (event.key === 'ArrowRight') !== rtl
    moveSpaceShown(entry.key, forward ? 1 : -1)
    await tick()
    switcher.querySelector<HTMLElement>(`[data-space="${entry.key}"]`)?.focus()
  }

  function name(entry: NavEntry): string {
    const label = $t(entry.labelKey)
    return entry.transient ? `${label} (${$t('spaces.notPinned')})` : label
  }
</script>

<div
  class="switcher"
  class:labels
  class:dragging
  role="tablist"
  aria-label={$t('space.switcher')}
  bind:this={switcher}
  use:reorder={{ axis: 'x' }}
  on:reorder={onReorder}
  on:reorderstart={() => (dragging = true)}
  on:reorderend={() => (dragging = false)}
>
  <span
    class="thumb"
    class:ready={thumbReady}
    style={`--thumb-x:${thumbX}px;--thumb-w:${thumbW}px`}
    aria-hidden="true"
  ></span>
  {#each $spaces as entry (entry.key)}
    <button
      type="button"
      role="tab"
      class="segment"
      class:active={$space === entry.key}
      class:transient={entry.transient}
      data-space={entry.key}
      data-reorder-id={entry.transient ? undefined : entry.key}
      data-reorder-handle={entry.transient ? undefined : ''}
      aria-selected={$space === entry.key}
      aria-label={name(entry)}
      use:tip={{ text: labels && !entry.transient ? '' : name(entry), side: 'top' }}
      use:maildrop={spaceDrop(entry.key)}
      animate:flip={{ duration: dur(motion.base) }}
      on:click={() => openSpace(entry.key)}
      on:contextmenu|preventDefault|stopPropagation={(e) => onContext(e, entry)}
      on:keydown={(e) => onKeydown(e, entry)}
    >
      <Ph svg={entry.icon} size={16} />
      {#if labels}
        <span>{$t(entry.labelKey)}</span>
      {/if}
    </button>
  {/each}
</div>

<style>
  .switcher {
    position: relative;
    display: flex;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    --wails-draggable: no-drag;
  }

  .thumb {
    position: absolute;
    top: var(--space-1);
    bottom: var(--space-1);
    left: 0;
    width: var(--thumb-w);
    border-radius: calc(var(--radius-control) - 1px);
    background: var(--surface-raised);
    box-shadow: 0 0 0 var(--hairline) var(--border-default);
    transform: translateX(var(--thumb-x));
  }

  .thumb.ready {
    transition:
      transform var(--motion-base) var(--ease-out),
      width var(--motion-base) var(--ease-out);
  }

  .segment {
    position: relative;
    flex: 1 1 0;
    min-width: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-4);
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  .segment:hover,
  .segment.active {
    color: var(--text-primary);
  }

  .dragging .segment {
    cursor: grabbing;
  }

  /* an open space the user left out of the navigation, there for now. */
  .segment.transient {
    outline: var(--hairline) dashed var(--border-strong);
    outline-offset: calc(-1 * var(--space-1));
  }
</style>
