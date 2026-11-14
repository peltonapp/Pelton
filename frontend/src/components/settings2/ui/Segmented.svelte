<script lang="ts">
  // a short row of mutually exclusive text options. the highlight slides from
  // one segment to the next. all segments share one width so the highlight
  // needs no measuring.
  import { createEventDispatcher } from 'svelte'

  /** One segment. */
  interface Option {
    key: string
    label: string
  }

  /** Key of the selected option. */
  export let value: string
  /** The segments, in order. */
  export let options: Option[]
  /** Accessible name of the group. */
  export let label: string = ''
  /** Greys the control out and ignores input. */
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ change: string }>()

  $: index = Math.max(
    0,
    options.findIndex((o) => o.key === value),
  )

  function pick(key: string): void {
    if (!disabled && key !== value) {
      dispatch('change', key)
    }
  }

  function onKeydown(event: KeyboardEvent, at: number): void {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0
    if (step === 0) {
      return
    }
    event.preventDefault()
    const rtl = getComputedStyle(event.currentTarget as HTMLElement).direction === 'rtl'
    const next = options[(at + (rtl ? -step : step) + options.length) % options.length]
    pick(next.key)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-key="${next.key}"]`)?.focus()
  }
</script>

<div
  class="segmented"
  class:disabled
  role="radiogroup"
  aria-label={label || undefined}
  style={`--count:${options.length};--at:${index}`}
>
  <span class="thumb" aria-hidden="true"></span>
  {#each options as option, at (option.key)}
    <button
      type="button"
      role="radio"
      class="segment"
      class:selected={value === option.key}
      aria-checked={value === option.key}
      tabindex={value === option.key ? 0 : -1}
      data-key={option.key}
      {disabled}
      on:click={() => pick(option.key)}
      on:keydown={(e) => onKeydown(e, at)}
    >
      {option.label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    position: relative;
    display: inline-grid;
    grid-template-columns: repeat(var(--count), 1fr);
    padding: 2px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .segmented.disabled {
    opacity: 0.5;
  }

  .thumb {
    position: absolute;
    top: 2px;
    bottom: 2px;
    inset-inline-start: calc(2px + (100% - 4px) / var(--count) * var(--at));
    width: calc((100% - 4px) / var(--count));
    border-radius: calc(var(--radius-control) - 2px);
    background: var(--surface-raised);
    box-shadow: 0 0 0 var(--hairline) var(--border-strong);
    transition: inset-inline-start var(--motion-base) var(--ease-out);
  }

  .segment {
    position: relative;
    min-height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-3);
    border: none;
    border-radius: calc(var(--radius-control) - 2px);
    background: transparent;
    font-size: var(--fz-label);
    color: var(--text-secondary);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  .segment:hover:not(:disabled) {
    color: var(--text-primary);
  }

  .segment.selected {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .segment:disabled {
    cursor: default;
  }

  .segment:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
