<script lang="ts">
  // the colour a new mailbox's mail is marked with in mixed views, picked from
  // the same palette as the mailbox settings. empty means no colour.
  import { viewColors } from '../../lib/viewicons'
  import { t } from '../../lib/i18n'

  /** The stored colour value, '' for none. */
  export let value = ''
</script>

<div class="pick" role="radiogroup" aria-label={$t('views.colorLabel')}>
  {#each viewColors as color (color.value)}
    <button
      type="button"
      role="radio"
      class="swatch"
      class:on={value === color.value}
      class:none={color.value === ''}
      style={`--sw:${color.css}`}
      aria-checked={value === color.value}
      aria-label={color.value || $t('views.colorNone')}
      on:click={() => (value = color.value)}
    ></button>
  {/each}
</div>

<style>
  .pick {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .swatch {
    width: calc(var(--control-height) * 0.7);
    height: calc(var(--control-height) * 0.7);
    padding: 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: 50%;
    background: var(--sw);
    cursor: var(--cursor-action);
    transition:
      box-shadow var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .swatch.none {
    background: var(--surface-raised);
    box-shadow: inset 0 0 0 var(--space-1) var(--surface-raised), inset 0 0 0 calc(var(--space-1) + var(--hairline)) var(--border-strong);
  }

  .swatch:active {
    transform: scale(0.92);
  }

  .swatch.on {
    box-shadow: 0 0 0 var(--space-1) var(--surface-base), 0 0 0 calc(var(--space-1) * 2) var(--sw);
  }

  .swatch.none.on {
    box-shadow: 0 0 0 var(--space-1) var(--surface-base), 0 0 0 calc(var(--space-1) * 2) var(--border-strong);
  }
</style>
