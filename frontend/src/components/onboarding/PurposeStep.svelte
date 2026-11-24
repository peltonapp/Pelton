<script lang="ts">
  // the first step of the setup: what Pelton is for. three set choices and a
  // custom one with a checklist of spaces; mail is always part of it. nothing
  // is stored here, the host writes the choice when the step is confirmed.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import { spaces } from '../../lib/spaces'
  import { purposes, spacesFor, type Purpose } from '../../lib/setupplan'
  import { t } from '../../lib/i18n'
  import type { Space } from '../../lib/types'

  /** The purpose picked. */
  export let purpose: Purpose
  /** The spaces ticked for the custom purpose. */
  export let custom: Space[]

  // scene names the space a pointer or focus rests on, for the preview.
  const dispatch = createEventDispatcher<{ scene: Space | null }>()

  const iconFor = new Map(spaces.map((s) => [s.key, s.icon]))

  function pick(next: Purpose): void {
    if (next === 'custom' && purpose !== 'custom') {
      custom = spacesFor(purpose, custom)
    }
    purpose = next
  }

  function onKeydown(event: KeyboardEvent, index: number): void {
    const step = event.key === 'ArrowDown' || event.key === 'ArrowRight' ? 1 : event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 0
    if (step === 0) {
      return
    }
    event.preventDefault()
    const next = purposes[(index + step + purposes.length) % purposes.length]
    pick(next)
    const group = (event.currentTarget as HTMLElement).parentElement
    group?.querySelector<HTMLElement>(`[data-purpose="${next}"]`)?.focus()
  }

  function toggle(key: Space, on: boolean): void {
    custom = on ? [...custom.filter((k) => k !== key), key] : custom.filter((k) => k !== key)
  }
</script>

<div class="options" role="radiogroup" aria-label={$t('setup.purpose.title')}>
  {#each purposes as key, index (key)}
    {@const on = purpose === key}
    {@const shown = key === 'custom' ? [] : spacesFor(key, [])}
    <button
      type="button"
      role="radio"
      class="option"
      class:on
      aria-checked={on}
      tabindex={on ? 0 : -1}
      data-purpose={key}
      on:click={() => pick(key)}
      on:keydown={(e) => onKeydown(e, index)}
    >
      <span class="mark" aria-hidden="true"></span>
      <span class="text">
        <span class="name">{$t(`setup.purpose.${key}`)}</span>
        <span class="hint">{$t(`setup.purpose.${key}.hint`)}</span>
      </span>
      {#if shown.length > 0}
        <span class="icons" aria-hidden="true">
          {#each shown as s (s)}
            <Ph svg={iconFor.get(s) ?? ''} size={15} />
          {/each}
        </span>
      {/if}
    </button>
  {/each}
</div>

{#if purpose === 'custom'}
  <fieldset class="checklist">
    <legend>{$t('setup.purpose.pick')}</legend>
    {#each spaces as s (s.key)}
      {@const checked = s.key === 'mail' || custom.includes(s.key)}
      <label
        class="space"
        class:fixed={s.key === 'mail'}
        on:pointerenter={() => dispatch('scene', s.key)}
        on:pointerleave={() => dispatch('scene', null)}
      >
        <input
          type="checkbox"
          {checked}
          disabled={s.key === 'mail'}
          on:change={(e) => toggle(s.key, e.currentTarget.checked)}
          on:focus={() => dispatch('scene', s.key)}
          on:blur={() => dispatch('scene', null)}
        />
        <Ph svg={s.icon} size={16} />
        <span class="space-name">{$t(s.labelKey)}</span>
        <span class="space-hint">{$t(`setup.space.${s.key}`)}</span>
      </label>
    {/each}
  </fieldset>
{/if}

<style>
  .options {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .option {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out);
  }

  .option:hover {
    border-color: var(--border-strong);
  }

  .option.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .option:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .mark {
    flex-shrink: 0;
    width: var(--space-4);
    height: var(--space-4);
    border: var(--hairline) solid var(--border-strong);
    border-radius: 50%;
    background: var(--surface-base);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out);
  }

  .option.on .mark {
    border-color: var(--accent);
    box-shadow: inset 0 0 0 calc(var(--space-1) - var(--hairline)) var(--accent);
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  .hint {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }

  .icons {
    display: inline-flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: var(--space-1);
    max-width: 40%;
    color: var(--text-tertiary);
  }

  .option.on .icons {
    color: var(--accent);
  }

  .checklist {
    display: flex;
    flex-direction: column;
    margin: var(--space-5) 0 0;
    padding: 0;
    border: 0;
    border-top: var(--hairline) solid var(--border-default);
  }

  legend {
    padding: var(--space-4) 0 var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .space {
    display: grid;
    grid-template-columns: auto auto minmax(0, 8em) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .space.fixed {
    cursor: default;
  }

  .space input {
    accent-color: var(--accent);
  }

  .space-name {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .space-hint {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
