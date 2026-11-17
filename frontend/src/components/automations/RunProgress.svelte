<script lang="ts">
  // the runs that take a moment, in a small stack at the window's corner:
  // which script, what it did last or that it waits for you, and a button that
  // cancels it. quick runs finish before they would show.
  import Spinner from '../common/Spinner.svelte'
  import { t } from '../../lib/i18n'
  import { cancelLive, liveRuns } from '../../stores/scripts'
  import '../../theme/controls.css'
</script>

{#if $liveRuns.length > 0}
  <div class="runs" role="status" aria-live="polite" aria-label={$t('scripts.progress.label')}>
    {#each $liveRuns as run (run.id)}
      <div class="run">
        <Spinner />
        <div class="text">
          <span class="name">{run.scriptName}{run.dry ? ` (${$t('scripts.progress.test')})` : ''}</span>
          <span class="last">{run.waiting ? $t('scripts.progress.waiting') : run.last || $t('scripts.progress.running')}</span>
        </div>
        <button type="button" class="cal-btn" on:click={() => void cancelLive(run.id)}>{$t('scripts.progress.cancel')}</button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .runs {
    position: fixed;
    inset-inline-end: var(--space-4);
    bottom: var(--space-4);
    z-index: 70;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    max-width: calc(var(--control-height) * 12);
  }

  .run {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-inline-start: 2px solid var(--accent);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }

  .name {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .last {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
