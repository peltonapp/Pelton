<script lang="ts">
  // back and forward in the top bar. a click steps one place; a long press or
  // a right-click lists the places in that direction to jump to.
  import { onDestroy } from 'svelte'
  import backIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import forwardIcon from '@phosphor-icons/core/assets/duotone/arrow-right-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { backVisits, canGoBack, canGoForward, forwardVisits, goBack, goForward, goToVisit, visitLabel } from '../../stores/navhistory'
  import { openContextMenu, type MenuEntry } from '../../stores/contextmenu'
  import { bindings } from '../../stores/shortcuts'
  import { tip } from '../../lib/tip'
  import { t, shortcutLabel } from '../../lib/i18n'

  type Way = 'back' | 'forward'

  const pressMs = 450
  const menuLimit = 12

  let timer: ReturnType<typeof setTimeout> | undefined
  // a long press opens the menu, and the click that ends it is not a step.
  let held = false

  $: keys = (action: 'nav-back' | 'nav-forward'): string | undefined => {
    const combo = $bindings[action]
    return combo ? shortcutLabel(combo) : undefined
  }

  function step(way: Way): void {
    if (held) {
      held = false
      return
    }
    void (way === 'back' ? goBack() : goForward())
  }

  function menu(way: Way, anchor: HTMLElement): void {
    const list = way === 'back' ? $backVisits : $forwardVisits
    if (list.length === 0) {
      return
    }
    const rect = anchor.getBoundingClientRect()
    const entries: MenuEntry[] = list.slice(0, menuLimit).map(({ visit, index }) => ({
      label: visitLabel(visit, $t),
      action: () => void goToVisit(index),
    }))
    openContextMenu(rect.left, rect.bottom, entries)
  }

  function onPointerDown(event: PointerEvent, way: Way): void {
    if (event.button !== 0) {
      return
    }
    held = false
    const anchor = event.currentTarget as HTMLElement
    clearTimeout(timer)
    timer = setTimeout(() => {
      held = true
      menu(way, anchor)
    }, pressMs)
  }

  function cancelPress(): void {
    clearTimeout(timer)
  }

  function onContext(event: MouseEvent, way: Way): void {
    cancelPress()
    menu(way, event.currentTarget as HTMLElement)
  }

  onDestroy(cancelPress)
</script>

<div class="nav" role="group" aria-label={$t('navHistory.label')} data-tip-anchor="nav">
  <button
    type="button"
    class="step"
    disabled={!$canGoBack}
    aria-label={$t('navHistory.back')}
    use:tip={{ text: $t('navHistory.back'), shortcut: keys('nav-back'), side: 'bottom' }}
    on:click={() => step('back')}
    on:pointerdown={(e) => onPointerDown(e, 'back')}
    on:pointerup={cancelPress}
    on:pointerleave={cancelPress}
    on:contextmenu|preventDefault|stopPropagation={(e) => onContext(e, 'back')}
  >
    <Ph svg={backIcon} size={15} />
  </button>
  <button
    type="button"
    class="step"
    disabled={!$canGoForward}
    aria-label={$t('navHistory.forward')}
    use:tip={{ text: $t('navHistory.forward'), shortcut: keys('nav-forward'), side: 'bottom' }}
    on:click={() => step('forward')}
    on:pointerdown={(e) => onPointerDown(e, 'forward')}
    on:pointerup={cancelPress}
    on:pointerleave={cancelPress}
    on:contextmenu|preventDefault|stopPropagation={(e) => onContext(e, 'forward')}
  >
    <Ph svg={forwardIcon} size={15} />
  </button>
</div>

<style>
  .nav {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    --wails-draggable: no-drag;
  }

  .step {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - var(--space-2));
    height: calc(var(--control-height) - var(--space-2));
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .step:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .step:active:not(:disabled) {
    transform: scale(var(--press-scale));
  }

  .step:disabled {
    color: var(--text-tertiary);
    opacity: 0.5;
    cursor: default;
  }

  /* the arrows point the way the reading goes. */
  :global([dir='rtl']) .step :global(svg) {
    transform: scaleX(-1);
  }
</style>
