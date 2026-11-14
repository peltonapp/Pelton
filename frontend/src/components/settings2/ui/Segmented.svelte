<script lang="ts">
  // a setting picked from two to four short text options, drawn as one bar
  // with a highlight that slides to the chosen segment.
  import { createEventDispatcher } from 'svelte'

  /** Key of the selected option. */
  export let value: string
  /** The options, in display order. */
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

  // arrow keys move the choice, as they do in a native radio group.
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

<div class="segmented" role="radiogroup" aria-label={label || undefined} style={`--n:${options.length};--i:${index}`}>
  <span class="highlight" aria-hidden="true"></span>
  {#each options as option, i (option.key)}
    <button
      type="button"
      role="radio"
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
    display: grid;
    grid-auto-flow: column;
    grid-auto-columns: 1fr;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .highlight {
    position: absolute;
    inset-block: var(--space-1);
    inset-inline-start: calc(var(--space-1) + (100% - 2 * var(--space-1)) * var(--i) / var(--n));
    width: calc((100% - 2 * var(--space-1)) / var(--n));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    transition: inset-inline-start var(--motion-base) var(--ease-out);
  }

  button {
    position: relative;
    height: calc(var(--control-height) - 2 * var(--space-1));
    padding: 0 var(--space-4);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  button.selected {
    color: var(--text-primary);
  }

  button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
