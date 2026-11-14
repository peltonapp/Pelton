<script lang="ts">
  // a day picked from a small month that opens under a button showing it.
  import { createEventDispatcher } from 'svelte'
  import caretDown from '@phosphor-icons/core/assets/duotone/caret-down-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import MiniMonth from './MiniMonth.svelte'

  /** The chosen day. */
  export let value: Date
  export let weekStart: number = 1
  /** Accessible name. */
  export let label: string
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ change: Date }>()

  let button: HTMLButtonElement
  let open = false

  function pick(day: Date): void {
    open = false
    dispatch('change', day)
    button?.focus()
  }
</script>

<button
  type="button"
  class="cal-field date"
  bind:this={button}
  aria-label={`${label}: ${value.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })}`}
  aria-haspopup="dialog"
  aria-expanded={open}
  {disabled}
  on:click={() => (open = !open)}
>
  <span>{value.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' })}</span>
  <Ph svg={caretDown} size={12} />
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
    gap: var(--space-3);
    min-width: calc(var(--cal-gutter) * 2.6);
    cursor: var(--cursor-action);
    text-align: start;
    font-variant-numeric: tabular-nums;
  }

  .date:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .picker {
    width: calc(var(--cal-sidebar-width) - var(--space-4));
    padding: var(--space-3);
  }
</style>
