<script lang="ts">
  // a short list of mutually exclusive text options, shown as segments with a
  // highlight that slides to the chosen one.
  import { createEventDispatcher } from 'svelte'

  /** Key of the chosen option. */
  export let value: string
  /** The options, in display order. */
  export let options: { key: string; label: string }[]
  /** Accessible name of the group. */
  export let label: string = ''
  /** Makes the segments inert. */
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ change: string }>()

  $: index = Math.max(0, options.findIndex((o) => o.key === value))

  function pick(key: string): void {
    if (!disabled && key !== value) {
      dispatch('change', key)
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    const forward = document.documentElement.dir === 'rtl' ? 'ArrowLeft' : 'ArrowRight'
    const back = document.documentElement.dir === 'rtl' ? 'ArrowRight' : 'ArrowLeft'
    let next = index
    if (event.key === forward || event.key === 'ArrowDown') {
      next = (index + 1) % options.length
    } else if (event.key === back || event.key === 'ArrowUp') {
      next = (index - 1 + options.length) % options.length
    } else {
      return
    }
    event.preventDefault()
    pick(options[next].key)
    const group = (event.currentTarget as HTMLElement).parentElement
    const buttons = group?.querySelectorAll<HTMLButtonElement>('button') ?? []
    buttons[next]?.focus()
  }
</script>

<div
  class="segmented"
  class:disabled
  role="radiogroup"
  aria-label={label || undefined}
  style={`--count:${options.length};--index:${index}`}
>
  <span class="highlight" aria-hidden="true"></span>
  {#each options as option (option.key)}
    <button
      type="button"
      role="radio"
      aria-checked={value === option.key}
      tabindex={value === option.key ? 0 : -1}
      {disabled}
      class:on={value === option.key}
      on:click={() => pick(option.key)}
      on:keydown={onKeydown}
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
    padding: 2px;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .segmented.disabled {
    opacity: 0.5;
  }

  .highlight {
    position: absolute;
    top: 2px;
    bottom: 2px;
    inset-inline-start: calc(2px + var(--index) * (100% - 4px) / var(--count));
    width: calc((100% - 4px) / var(--count));
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-control) - 1px);
    background: var(--surface-raised);
    transition: inset-inline-start var(--motion-base) var(--ease-out);
  }

  button {
    position: relative;
    height: calc(var(--control-height) - 6px);
    padding: 0 var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition: color var(--motion-fast) var(--ease-out);
  }

  button:hover {
    color: var(--text-primary);
  }

  button.on {
    color: var(--text-primary);
  }

  button:disabled {
    cursor: default;
  }
</style>
