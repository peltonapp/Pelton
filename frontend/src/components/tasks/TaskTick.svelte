<script lang="ts">
  // the checkbox of a task drawn inside something that is already a button,
  // such as an entry in the calendar's time grid: the same square and drawn
  // tick as TaskCheck, as a checkbox role on a span, which keeps the pointer
  // to itself so pressing it never starts a drag of the entry around it.
  import { createEventDispatcher } from 'svelte'

  export let checked: boolean
  /** Accessible name. */
  export let label: string
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ toggle: void }>()

  function toggle(): void {
    if (!disabled) {
      dispatch('toggle')
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === ' ' || event.key === 'Enter') {
      event.preventDefault()
      event.stopPropagation()
      toggle()
    }
  }
</script>

<span
  class="tick"
  class:checked
  class:disabled
  role="checkbox"
  aria-checked={checked}
  aria-label={label}
  aria-disabled={disabled}
  tabindex="0"
  on:pointerdown|stopPropagation
  on:click|stopPropagation|preventDefault={toggle}
  on:keydown={onKey}
>
  <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 8.4l2.6 2.6L12 5.6" pathLength="1" /></svg>
</span>

<style>
  .tick {
    display: inline-flex;
    flex-shrink: 0;
    width: calc(var(--task-check) - var(--space-1));
    height: calc(var(--task-check) - var(--space-1));
    border: calc(var(--hairline) * 1.5) solid var(--ev, var(--accent));
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .tick.checked {
    background: var(--ev, var(--accent));
  }

  .tick.disabled {
    opacity: 0.5;
    cursor: default;
  }

  .tick:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  svg {
    width: 100%;
    height: 100%;
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
