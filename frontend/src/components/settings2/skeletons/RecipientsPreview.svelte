<script lang="ts">
  // the To line of a message with two recipients entered: each one a chip with
  // a remove mark, or the addresses as plain comma separated text.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Draws recipients as chips instead of plain text. */
  export let chips: boolean = true

  const people = sampleMessages.slice(0, 2)
</script>

<Frame>
  <div class="line">
    <span class="label">{$t('compose.field.to')}</span>
    <span class="field">
      {#if chips}
        {#each people as p (p.email)}
          <span class="chip">{p.name}<span class="x">&times;</span></span>
        {/each}
      {:else}
        <span class="plain">{people.map((p) => p.email).join(', ')},</span>
      {/if}
      <span class="caret"></span>
    </span>
  </div>
  <div class="line quiet">
    <span class="label">{$t('compose.field.subject')}</span>
    <span class="bar"></span>
  </div>
</Frame>

<style>
  .line {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-height: calc(var(--chip-height) + var(--space-3));
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .line.quiet {
    border-bottom: none;
  }

  .label {
    width: 44px;
    flex-shrink: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .field {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--chip-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    white-space: nowrap;
  }

  .x {
    color: var(--text-tertiary);
  }

  .plain {
    overflow-wrap: anywhere;
  }

  .caret {
    width: 2px;
    height: calc(var(--fz-label) + var(--space-2));
    border-radius: 1px;
    background: var(--accent);
  }

  .bar {
    width: 50%;
    height: 4px;
    border-radius: 2px;
    background: var(--border-strong);
  }
</style>
