<script lang="ts">
  // the checkbox of a task: a small square that fills with the list's color
  // and draws its tick when the task is done. the tick is a stroked path whose
  // dash runs out over the base motion duration, so it stands still where
  // reduced motion is asked for.
  import { createEventDispatcher } from 'svelte'

  /** Whether the task is done. */
  export let checked: boolean
  /** Accessible name, such as the task's title. */
  export let label: string
  /** The list's color; empty uses the accent. */
  export let color: string = ''
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ toggle: boolean }>()
</script>

<button
  type="button"
  class="check"
  class:checked
  role="checkbox"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  style={color ? `--task-color:${color}` : undefined}
  on:click|stopPropagation={() => dispatch('toggle', !checked)}
>
  <svg viewBox="0 0 16 16" aria-hidden="true">
    <path d="M4 8.4l2.6 2.6L12 5.6" pathLength="1" />
  </svg>
</button>

<style>
  .check {
    --task-color: var(--accent);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--task-check);
    height: var(--task-check);
    padding: 0;
    border: calc(var(--hairline) * 1.5) solid color-mix(in srgb, var(--task-color) 70%, var(--border-strong));
    border-radius: var(--radius-control);
    background: transparent;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .check:hover:not(:disabled) {
    background: color-mix(in srgb, var(--task-color) 14%, transparent);
  }

  .check:active:not(:disabled) {
    transform: scale(var(--press-scale));
  }

  .check:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .check:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .check.checked {
    border-color: var(--task-color);
    background: var(--task-color);
  }

  svg {
    width: 100%;
    height: 100%;
    overflow: visible;
  }

  path {
    fill: none;
    stroke: var(--surface-raised);
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 1;
    stroke-dashoffset: 1;
    transition: stroke-dashoffset var(--motion-base) var(--ease-out);
  }

  .checked path {
    stroke-dashoffset: 0;
  }
</style>
