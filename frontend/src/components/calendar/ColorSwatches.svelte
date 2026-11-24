<script lang="ts">
  // the calendar palette as a row of swatches to pick a calendar's color from.
  // the chosen one is named either by its palette index or by a stored
  // #rrggbb, which is matched against the palette.
  import { createEventDispatcher } from 'svelte'
  import { palette, paletteHex } from '../../lib/calformat'
  import { t } from '../../lib/i18n'

  /** The chosen palette index, when the caller tracks one. */
  export let index: number = -1
  /** The chosen color as #rrggbb, when the caller tracks that instead. */
  export let value: string = ''

  const dispatch = createEventDispatcher<{ pick: number }>()

  $: chosen = index >= 0 ? index : palette.findIndex((_, i) => value !== '' && paletteHex(i).toLowerCase() === value.toLowerCase())
</script>

<div class="swatches" role="radiogroup" aria-label={$t('calendar.color')}>
  {#each palette as color, i (i)}
    <button
      type="button"
      role="radio"
      class="swatch"
      class:on={chosen === i}
      style={`--swatch:${color}`}
      aria-checked={chosen === i}
      aria-label={$t('calendar.colorN').replace('{n}', String(i + 1))}
      on:click={() => dispatch('pick', i)}
    ></button>
  {/each}
</div>

<style>
  .swatches {
    display: grid;
    grid-template-columns: repeat(8, var(--space-5));
    gap: var(--space-3);
  }

  .swatch {
    width: var(--space-5);
    height: var(--space-5);
    padding: 0;
    border: none;
    border-radius: 50%;
    background: var(--swatch);
    cursor: var(--cursor-action);
    transition: transform var(--motion-fast) var(--ease-out);
  }

  .swatch:hover {
    transform: scale(1.12);
  }

  .swatch.on {
    box-shadow:
      0 0 0 2px var(--surface-overlay),
      0 0 0 3.5px var(--swatch);
  }

  .swatch:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 3px;
  }
</style>
