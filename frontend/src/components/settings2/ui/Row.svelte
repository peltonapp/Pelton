<script lang="ts">
  // one setting: its name and a line about it on the left, its control on the
  // right. `stacked` puts the control underneath, for controls too wide to sit
  // beside the text. a `preview` slot holds a picture of what the setting does,
  // shown in a bubble while the pointer rests on the row.
  import { onDestroy } from 'svelte'
  import PreviewTip from './PreviewTip.svelte'

  /** Name of the setting. */
  export let label: string
  /** One or two sentences about what it does. */
  export let hint: string = ''
  /** Anchor settings search scrolls to; unique within the page. */
  export let id: string = ''
  /** Puts the control under the text instead of beside it. */
  export let stacked: boolean = false
  /** Dims the row for a setting that does not apply right now. */
  export let disabled: boolean = false

  const restMs = 320

  let row: HTMLDivElement
  let tipOpen = false
  let tipX = 0
  let tipY = 0
  let timer: ReturnType<typeof setTimeout> | undefined

  function onEnter(event: PointerEvent): void {
    tipX = event.clientX
    tipY = event.clientY
    clearTimeout(timer)
    timer = setTimeout(() => (tipOpen = true), restMs)
  }

  function onMove(event: PointerEvent): void {
    tipX = event.clientX
    tipY = event.clientY
  }

  function onLeave(): void {
    clearTimeout(timer)
    tipOpen = false
  }

  // keyboard users get the picture under the row they tabbed into.
  function onFocusIn(): void {
    const rect = row.getBoundingClientRect()
    tipX = rect.left
    tipY = rect.bottom
    tipOpen = true
  }

  onDestroy(() => clearTimeout(timer))
</script>

{#if $$slots.preview}
  <div
    class="row"
    class:stacked
    class:disabled
    data-setting={id || undefined}
    role="group"
    aria-label={label}
    bind:this={row}
    on:pointerenter={onEnter}
    on:pointermove={onMove}
    on:pointerleave={onLeave}
    on:focusin={onFocusIn}
    on:focusout={onLeave}
  >
    <div class="text">
      <span class="label">{label}</span>
      {#if hint}
        <span class="hint">{hint}</span>
      {/if}
    </div>
    <div class="control">
      <slot />
    </div>
    <PreviewTip open={tipOpen} x={tipX} y={tipY}>
      <slot name="preview" />
    </PreviewTip>
  </div>
{:else}
  <div class="row" class:stacked class:disabled data-setting={id || undefined}>
    <div class="text">
      <span class="label">{label}</span>
      {#if hint}
        <span class="hint">{hint}</span>
      {/if}
    </div>
    <div class="control">
      <slot />
    </div>
  </div>
{/if}

<style>
  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-5);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--space-4);
  }

  .row.stacked {
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-3);
  }

  .row.disabled {
    opacity: 0.5;
    pointer-events: none;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .label {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .hint {
    max-width: 58ch;
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .control {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-shrink: 0;
  }

  /* stretch, so a grid of cards or a mounted section fills the row's width
     instead of shrinking to its narrowest content. */
  .stacked .control {
    flex-direction: column;
    align-items: stretch;
    flex-shrink: 1;
    min-width: 0;
  }

</style>
