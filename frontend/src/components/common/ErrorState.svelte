<script lang="ts">
  // the error case of EmptyState with the generic title and an optional retry.
  // used wherever an async load can fail so the user sees the reason instead
  // of a blank column. the classic design keeps its own error pane.
  import EmptyState from './EmptyState.svelte'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'

  /** What failed, shown under the title. */
  export let message: string
  /** Reruns the failed load; no retry button without it. */
  export let onRetry: (() => void) | null = null
</script>

{#if $prefs.design === 'classic'}
  <div class="error" role="alert">
    <p class="title">{$t('common.errorState.title')}</p>
    <p class="detail">{message}</p>
    {#if onRetry}
      <button type="button" class="retry" on:click={onRetry}>{$t('common.errorState.retry')}</button>
    {/if}
  </div>
{:else}
  <EmptyState
    kind="error"
    title={$t('common.errorState.title')}
    detail={message}
    actionLabel={onRetry ? $t('common.errorState.retry') : ''}
    onAction={onRetry}
  />
{/if}

<style>
  .error {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    padding: var(--space-6);
    text-align: center;
  }

  .title {
    margin: 0;
    font-weight: var(--fw-semibold);
    color: var(--danger);
  }

  .detail {
    margin: 0;
    max-width: 40ch;
    font-size: var(--fz-label);
    color: var(--text-secondary);
    word-break: break-word;
  }

  .retry {
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }

  .retry:hover {
    background: var(--surface-hover);
  }
</style>
