<script lang="ts">
  // the reading view's layer: the conversation alone over the whole window,
  // on the theme's base surface. It takes the focus when it opens and gives it
  // back when it closes, and Escape leaves it unless a dialog or menu above it
  // wants the key.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fade } from 'svelte/transition'
  import { portal } from '../../lib/portal'
  import { dur, motion } from '../../lib/motion'
  import { closeReadingView } from '../../stores/readingview'

  /** What a screen reader calls the layer. */
  export let label: string

  let layer: HTMLDivElement
  let restoreTo: HTMLElement | null = null

  onMount(async () => {
    restoreTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await tick()
    layer?.querySelector<HTMLElement>('[tabindex="-1"]')?.focus({ preventScroll: true })
  })

  onDestroy(() => {
    if (restoreTo?.isConnected) {
      restoreTo.focus({ preventScroll: true })
    }
  })

  // a dialog or menu opened over the reading view takes Escape first.
  function somethingAbove(): boolean {
    return Array.from(document.querySelectorAll('[aria-modal="true"], [role="menu"]')).some((el) => !layer.contains(el) && el !== layer)
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key !== 'Escape' || event.defaultPrevented || somethingAbove()) {
      return
    }
    event.preventDefault()
    closeReadingView()
  }
</script>

<svelte:window on:keydown={onKey} />

<div
  class="reading-layer"
  role="dialog"
  aria-modal="true"
  aria-label={label}
  bind:this={layer}
  use:portal
  in:fade={{ duration: dur(motion.base) }}
>
  <slot />
</div>

<style>
  .reading-layer {
    position: fixed;
    inset: 0;
    z-index: 180;
    display: flex;
    background: var(--surface-base);
  }
</style>
