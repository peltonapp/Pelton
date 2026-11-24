<script lang="ts">
  // the classic design has no spaces, so it shows the Automations space as a
  // full window overlay with a close button, the way it shows contacts.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { IconX } from '@tabler/icons-svelte'
  import AutomationsSpace from './AutomationsSpace.svelte'
  import { t } from '../../lib/i18n'
  import { popupIsOpen } from '../../lib/popups'
  import '../../theme/controls.css'

  const dispatch = createEventDispatcher<{ close: void }>()

  let overlay: HTMLDivElement
  let restoreTo: HTMLElement | null = null

  function innerLayerOpen(): boolean {
    return popupIsOpen() || [...document.querySelectorAll('[role="dialog"]')].some((d) => d !== overlay)
  }

  function onKey(e: KeyboardEvent): void {
    if (e.key === 'Escape' && !innerLayerOpen()) {
      dispatch('close')
    }
  }

  function focusables(): HTMLElement[] {
    const found = overlay.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"]), [contenteditable="true"]',
    )
    return [...found].filter((el) => el.offsetParent !== null)
  }

  function onTab(e: KeyboardEvent): void {
    if (e.key !== 'Tab' || e.defaultPrevented || innerLayerOpen()) {
      return
    }
    const items = focusables()
    if (items.length === 0) {
      e.preventDefault()
      overlay.focus()
      return
    }
    const first = items[0]
    const last = items[items.length - 1]
    const current = document.activeElement as HTMLElement | null
    const outside = !current || !overlay.contains(current) || current === overlay
    if (e.shiftKey && (current === first || outside)) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && (current === last || outside)) {
      e.preventDefault()
      first.focus()
    }
  }

  onMount(async () => {
    restoreTo = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', onKey)
    await tick()
    overlay?.focus()
  })
  onDestroy(() => {
    window.removeEventListener('keydown', onKey)
    restoreTo?.focus?.()
  })
</script>

<svelte:window on:keydown={onTab} />

<div class="overlay" role="dialog" aria-modal="true" aria-labelledby="automations-overlay-title" tabindex="-1" bind:this={overlay}>
  <header class="head">
    <h1 class="title" id="automations-overlay-title">{$t('space.automations')}</h1>
    <button type="button" class="cal-icon" aria-label={$t('modal.close')} on:click={() => dispatch('close')}>
      <IconX size={16} />
    </button>
  </header>
  <div class="host">
    <AutomationsSpace />
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    color: var(--text-primary);
    font-family: var(--font-ui);
  }

  .overlay:focus {
    outline: none;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .host {
    position: relative;
    flex: 1;
    min-height: 0;
  }
</style>
