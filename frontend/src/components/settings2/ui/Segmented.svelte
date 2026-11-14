<script lang="ts">
  // two to four text options in one strip, one of them chosen. a highlight
  // slides to the chosen segment. the parent owns the value.
  import { createEventDispatcher } from 'svelte'

  /** Key of the chosen option. */
  export let value: string
  export let options: { key: string; label: string }[]
  /** Accessible name of the group. */
  export let label: string = ''
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

  function onKeydown(event: KeyboardEvent, i: number): void {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0
    if (step === 0) {
      return
    }
    event.preventDefault()
    const next = options[(i + step + options.length) % options.length]
    pick(next.key)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-key="${next.key}"]`)?.focus()
  }
</script>

<div class="segmented" role="radiogroup" aria-label={label || undefined} class:disabled style={`--count:${options.length};--index:${index}`}>
  <span class="highlight" aria-hidden="true"></span>
  {#each options as option, i (option.key)}
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
      on:keydown={(e) => onKeydown(e, i)}
    >
      {option.label}
    </button>
  {/each}
</div>

<style>
  .segmented {
    position: relative;
    display: inline-grid;
    grid-auto-flow: column;
    grid-auto-columns: 1fr;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .segmented.disabled {
    opacity: 0.5;
  }

  .highlight {
    position: absolute;
    inset-block: 0;
    inset-inline-start: calc(var(--index) * 100% / var(--count));
    width: calc(100% / var(--count));
    border: var(--hairline) solid var(--border-strong);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    box-sizing: border-box;
    transition: inset-inline-start var(--motion-base) var(--ease-out);
  }

  .segment {
    position: relative;
    min-height: var(--control-height);
    padding: 0 var(--space-4);
    border: none;
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
</style>
