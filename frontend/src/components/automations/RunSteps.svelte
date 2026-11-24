<script lang="ts">
  // the steps of one run in order: what it read, what it changed or (in a test
  // run) would have changed, how each block condition came out, what it logged,
  // and any call it was refused.
  import { t } from '../../lib/i18n'
  import type { RunStep } from '../../lib/scripting'

  /** The steps, in the order they happened. */
  export let steps: RunStep[]
</script>

{#if steps.length === 0}
  <p class="none">{$t('scripts.steps.none')}</p>
{:else}
  <ol class="steps">
    {#each steps as step, i (i)}
      <li class="step {step.kind}" class:failed={!!step.error}>
        <span class="kind">{$t(`scripts.step.${step.kind}`)}</span>
        <span class="what">
          {#if step.kind === 'check'}
            {$t(step.result ? 'scripts.step.checkTrue' : 'scripts.step.checkFalse').replace('{block}', step.block ?? '')}
          {:else}
            {step.summary}
          {/if}
          {#if step.error}
            <span class="error">{step.error}</span>
          {/if}
        </span>
        <span class="at">{step.atMs} ms</span>
      </li>
    {/each}
  </ol>
{/if}

<style>
  .steps {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    font-size: var(--fz-meta);
  }

  .step {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: var(--space-2);
    align-items: baseline;
    padding: var(--space-1) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .kind {
    min-width: 0;
    color: var(--text-tertiary);
    text-transform: lowercase;
  }

  .what {
    min-width: 0;
    font-family: var(--font-mono);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .planned .kind,
  .call .kind {
    color: var(--accent);
  }

  .denied .kind,
  .declined .kind,
  .failed .kind,
  .error {
    color: var(--danger);
  }

  .error {
    display: block;
    font-family: var(--font-ui);
  }

  .at {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .none {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
