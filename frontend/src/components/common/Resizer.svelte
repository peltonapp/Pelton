<script lang="ts">
  // a vertical drag handle between two columns. it reports horizontal movement
  // as it is dragged and signals when the drag ends so the parent can persist the
  // new widths. when disabled (panes locked in settings) it is inert. it is also
  // keyboard adjustable with the arrow keys for accessibility.
  import { createEventDispatcher } from 'svelte'
  import { t } from '../../lib/i18n'

  export let disabled: boolean = false
  export let label: string = ''

  const dispatch = createEventDispatcher<{ resize: number; end: void }>()
  $: resolvedLabel = label || $t('common.resizer.default')

  let dragging = false
  let lastX = 0

  function onPointerDown(event: PointerEvent): void {
    if (disabled) {
      return
    }
    dragging = true
    lastX = event.clientX
    ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
  }

  function onPointerMove(event: PointerEvent): void {
    if (!dragging) {
      return
    }
    const dx = event.clientX - lastX
    lastX = event.clientX
    if (dx !== 0) {
      dispatch('resize', dx)
    }
  }

  function onPointerUp(event: PointerEvent): void {
    if (!dragging) {
      return
    }
    dragging = false
    ;(event.currentTarget as HTMLElement).releasePointerCapture(event.pointerId)
    dispatch('end')
  }

  // keyboard resize: a fixed step per arrow press.
  function onKeydown(event: KeyboardEvent): void {
    if (disabled) {
      return
    }
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      dispatch('resize', -16)
      dispatch('end')
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      dispatch('resize', 16)
      dispatch('end')
    }
  }
</script>

<!-- the separator is intentionally focusable so it can be resized with the
     keyboard; that is an accessibility feature, not a violation. -->
<!-- svelte-ignore a11y-no-noninteractive-tabindex a11y-no-noninteractive-element-interactions -->
<div
  class="resizer"
  class:disabled
  class:dragging
  role="separator"
  aria-orientation="vertical"
  aria-label={resolvedLabel}
  tabindex={disabled ? -1 : 0}
  on:pointerdown={onPointerDown}
  on:pointermove={onPointerMove}
  on:pointerup={onPointerUp}
  on:pointercancel={onPointerUp}
  on:keydown={onKeydown}
></div>

<style>
  .resizer {
    width: 5px;
    margin: 0 -2px;
    cursor: col-resize;
    background: transparent;
    z-index: 5;
    flex-shrink: 0;
    position: relative;
  }

  /* a hairline appears on hover/drag so the handle is discoverable. */
  .resizer::after {
    content: '';
    position: absolute;
    inset: 0 2px;
    background: transparent;
  }

  .resizer:hover::after,
  .resizer.dragging::after {
    background: var(--accent);
  }

  .resizer.disabled {
    cursor: default;
  }

  .resizer.disabled:hover::after {
    background: transparent;
  }
</style>
