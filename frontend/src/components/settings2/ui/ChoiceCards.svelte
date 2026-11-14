<script lang="ts">
  // a setting chosen by picture instead of by name: one card per option, each
  // showing a small skeleton of what it does. the parent draws the skeleton in
  // the default slot and owns the value.
  import { createEventDispatcher } from 'svelte'

  /** One option of the choice. */
  interface Choice {
    key: string
    label: string
    /** One line under the label. */
    hint?: string
  }

  /** Accessible name of the group. */
  export let label: string
  /** Key of the selected option. */
  export let value: string
  export let options: Choice[]
  /** Narrowest a card may get before the row wraps, in px. */
  export let minWidth: number = 150
  export let disabled: boolean = false

  const dispatch = createEventDispatcher<{ change: string }>()

  function pick(key: string): void {
    if (!disabled && key !== value) {
      dispatch('change', key)
    }
  }

  // arrow keys move the choice, as they do in a native radio group.
  function onKeydown(event: KeyboardEvent, index: number): void {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0
    if (step === 0) {
      return
    }
    event.preventDefault()
    const next = options[(index + step + options.length) % options.length]
    pick(next.key)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-key="${next.key}"]`)?.focus()
  }
</script>

<div class="cards" role="radiogroup" aria-label={label} style={`--min:${minWidth}px`}>
  {#each options as option, index (option.key)}
    <button
      type="button"
      role="radio"
      class="card"
      class:selected={value === option.key}
      aria-checked={value === option.key}
      tabindex={value === option.key ? 0 : -1}
      data-key={option.key}
      {disabled}
      on:click={() => pick(option.key)}
      on:keydown={(e) => onKeydown(e, index)}
    >
      <span class="art">
        <slot {option} selected={value === option.key} />
      </span>
      <span class="caption">
        <span class="mark" aria-hidden="true">
          <svg viewBox="0 0 12 12" width="10" height="10">
            <path d="M2.5 6.4 5 8.8 9.6 3.4" />
          </svg>
        </span>
        <span class="text">
          <span class="name">{option.label}</span>
          {#if option.hint}
            <span class="hint">{option.hint}</span>
          {/if}
        </span>
      </span>
    </button>
  {/each}
</div>

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(var(--min), 1fr));
    gap: var(--space-4);
  }

  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .card:hover:not(:disabled) {
    border-color: var(--border-strong);
  }

  .card:active:not(:disabled) {
    transform: scale(0.985);
  }

  .card.selected {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .card:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .art {
    display: block;
    border-radius: calc(var(--radius-card) - 2px);
    overflow: hidden;
    pointer-events: none;
  }

  .caption {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }

  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 14px;
    height: 14px;
    margin-top: 2px;
    border: var(--hairline) solid var(--border-strong);
    border-radius: 50%;
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  /* the tick is drawn, not faded: the path's dash runs out as it is selected. */
  .mark path {
    fill: none;
    stroke: var(--accent-fg);
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 12;
    stroke-dashoffset: 12;
    transition: stroke-dashoffset var(--motion-base) var(--ease-out);
  }

  .selected .mark {
    background: var(--accent);
    border-color: var(--accent);
  }

  .selected .mark path {
    stroke-dashoffset: 0;
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .hint {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 1.35;
  }
</style>
