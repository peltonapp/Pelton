<script lang="ts">
  // an address field with one letter typed. with autocomplete on, a list of
  // matching people drops under it; off, the field waits for the full address.
  import Frame from './Frame.svelte'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Shows the suggestion list. */
  export let enabled: boolean = true

  const typed = 'a'
  const matches = sampleMessages.filter((p) => p.email.startsWith(typed)).slice(0, 2)
</script>

<Frame>
  <div class="line">
    <span class="label">{$t('compose.field.to')}</span>
    <span class="field">{typed}<span class="caret"></span></span>
  </div>
  <div class="slot">
    {#if enabled}
      <ul class="list">
        {#each matches as p, i (p.email)}
          <li class:on={i === 0}>
            <span class="name">{p.name}</span>
            <span class="addr">{p.email}</span>
          </li>
        {/each}
      </ul>
    {/if}
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

  .label {
    width: 44px;
    flex-shrink: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .field {
    display: inline-flex;
    align-items: center;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .caret {
    width: 2px;
    height: calc(var(--fz-label) + var(--space-2));
    margin-inline-start: 1px;
    border-radius: 1px;
    background: var(--accent);
  }

  .slot {
    min-height: calc(2 * (var(--fz-label) + var(--space-4)) + var(--space-3));
    padding-top: var(--space-2);
  }

  .list {
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  li {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
  }

  li.on {
    background: var(--selection-bg);
  }

  .name {
    color: var(--text-primary);
    white-space: nowrap;
  }

  .addr {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
  }
</style>
