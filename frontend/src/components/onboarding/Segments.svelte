<script lang="ts">
  // a row of small options for a choice that needs no picture, such as the
  // interface size: a radio group the arrow keys move through. it wraps instead
  // of overflowing when the labels run long.
  import { createEventDispatcher } from 'svelte'

  /** Accessible name of the group. */
  export let label: string
  /** Key of the selected option. */
  export let value: string
  export let options: { key: string; label: string }[]
  /** Sets the labels in the mono face, for numbers. */
  export let mono = false

  const dispatch = createEventDispatcher<{ change: string }>()

  function onKeydown(event: KeyboardEvent, index: number): void {
    const forward = event.key === 'ArrowRight' || event.key === 'ArrowDown'
    const backward = event.key === 'ArrowLeft' || event.key === 'ArrowUp'
    if (!forward && !backward) {
      return
    }
    event.preventDefault()
    const rtl = document.documentElement.dir === 'rtl' && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')
    const step = (forward ? 1 : -1) * (rtl ? -1 : 1)
    const next = options[(index + step + options.length) % options.length]
    dispatch('change', next.key)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-key="${next.key}"]`)?.focus()
  }
</script>

<div class="segments" class:mono role="radiogroup" aria-label={label}>
  {#each options as option, index (option.key)}
    <button
      type="button"
      role="radio"
      class:on={value === option.key}
      aria-checked={value === option.key}
      tabindex={value === option.key ? 0 : -1}
      data-key={option.key}
      on:click={() => dispatch('change', option.key)}
      on:keydown={(e) => onKeydown(e, index)}
    >
      {option.label}
    </button>
  {/each}
</div>

<style>
  .segments {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  button {
    min-height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .mono button {
    font-family: var(--font-mono);
  }

  button:hover {
    border-color: var(--border-strong);
  }

  button:active {
    transform: scale(0.96);
  }

  button.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--accent);
    color: var(--text-primary);
  }
</style>
