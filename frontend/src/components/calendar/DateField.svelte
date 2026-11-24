<script lang="ts">
  // a day picked from a small month that opens under a button showing it. it
  // wears the same chevron as the shared select, so a row of the two reads as
  // one kind of control.
  import { createEventDispatcher } from 'svelte'
  import { IconChevronDown } from '@tabler/icons-svelte'
  import '../../theme/controls.css'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import MiniMonth from './MiniMonth.svelte'

  /** The chosen day. */
  export let value: Date
  export let weekStart: number = 1
  /** Accessible name. */
  export let label: string
  export let disabled: boolean = false
  /** Extra classes on the button, so a form can style it with its selects. */
  let extraClass = ''
  export { extraClass as class }

  const dispatch = createEventDispatcher<{ change: Date }>()

  let button: HTMLButtonElement
  let open = false

  $: thisYear = value.getFullYear() === new Date().getFullYear()
  $: shown = value.toLocaleDateString(undefined, thisYear ? { weekday: 'short', day: 'numeric', month: 'short' } : { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })

  function pick(day: Date): void {
    open = false
    dispatch('change', day)
    button?.focus()
  }
</script>

<button
  type="button"
  class="date {extraClass}"
  class:open
  bind:this={button}
  aria-label={`${label}: ${value.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })}`}
  aria-haspopup="dialog"
  aria-expanded={open}
  {disabled}
  on:click={() => (open = !open)}
>
  <span>{shown}</span>
  <IconChevronDown size={14} stroke={1.8} class="chevron" />
</button>

{#if open}
  <AnchoredPopover anchor={button} align="start" {label} on:close={() => (open = false)}>
    <div class="picker">
      <MiniMonth {value} {weekStart} on:pick={(e) => pick(e.detail)} />
    </div>
  </AnchoredPopover>
{/if}

<style>
  .date {
    display: inline-flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    white-space: nowrap;
    text-align: start;
    font-variant-numeric: tabular-nums;
    cursor: var(--cursor-action);
  }

  .date:not(:disabled):hover,
  .date.open {
    border-color: var(--accent);
  }

  .date:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .date:disabled {
    opacity: 0.55;
    cursor: default;
  }

  .picker {
    width: calc(var(--cal-sidebar-width) - var(--space-4));
    padding: var(--space-3);
  }
</style>
