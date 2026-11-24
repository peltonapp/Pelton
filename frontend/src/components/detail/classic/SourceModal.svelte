<script lang="ts">
  // the classic design's view-source modal: the raw RFC 822 source of the open
  // message, exactly as fetched from the server. the source is not cached
  // locally (only parsed bodies are), so it loads on demand over imap when the
  // modal opens.
  import { onMount, createEventDispatcher } from 'svelte'
  import { IconX, IconCopy } from '@tabler/icons-svelte'
  import Spinner from '../../common/Spinner.svelte'
  import { getMessageSource } from '../../../lib/api'
  import { toastError, toastSuccess } from '../../../stores/toast'
  import { friendlyError } from '../../../lib/errors'
  import { t } from '../../../lib/i18n'

  /** The message whose source is shown. */
  export let messageId: number

  const dispatch = createEventDispatcher<{ close: void }>()

  let source = ''
  let loading = true
  let error = ''

  onMount(async () => {
    try {
      source = await getMessageSource(messageId)
    } catch (err) {
      error = friendlyError(err)
    } finally {
      loading = false
    }
  })

  async function copyAll(): Promise<void> {
    try {
      await navigator.clipboard.writeText(source)
      toastSuccess($t('detail.source.copied'))
    } catch {
      toastError($t('detail.source.copyFailed'))
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      dispatch('close')
    }
  }
</script>

<svelte:window on:keydown={onKeydown} />

<!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
<div class="overlay" on:click={() => dispatch('close')}>
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions a11y-no-noninteractive-element-interactions -->
  <div class="modal" role="dialog" aria-modal="true" aria-label={$t('detail.source.title')} tabindex="-1" on:click|stopPropagation>
    <header>
      <span class="title">{$t('detail.source.title')}</span>
      <div class="actions">
        <button type="button" class="tool" disabled={!source} aria-label={$t('detail.source.copy')} title={$t('detail.source.copy')} on:click={copyAll}>
          <IconCopy size={16} stroke={1.7} />
        </button>
        <button type="button" class="tool" aria-label={$t('detail.attachments.close')} on:click={() => dispatch('close')}>
          <IconX size={18} stroke={1.8} />
        </button>
      </div>
    </header>

    {#if loading}
      <div class="state"><Spinner label={$t('detail.source.loading')} /></div>
    {:else if error}
      <div class="state error">{error}</div>
    {:else}
      <pre class="source selectable">{source}</pre>
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 140;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-5);
    background: var(--scrim, rgba(0, 0, 0, 0.4));
  }

  .modal {
    width: 100%;
    max-width: 860px;
    height: 80vh;
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-4) var(--space-5);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .tool {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }

  .tool:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tool:disabled {
    color: var(--text-tertiary);
    cursor: default;
  }

  .state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-6);
  }

  .state.error {
    color: var(--danger);
    font-size: var(--fz-label);
    text-align: center;
  }

  .source {
    flex: 1;
    margin: 0;
    padding: var(--space-4) var(--space-5);
    overflow: auto;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
