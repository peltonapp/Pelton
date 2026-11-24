<script lang="ts">
  // the view-source modal: the raw RFC 822 source of the open message, exactly
  // as fetched from the server. the source is not cached locally (only parsed
  // bodies are), so it loads on demand over imap when the modal opens.
  import { onMount, createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ErrorState from '../common/ErrorState.svelte'
  import { getMessageSource } from '../../lib/api'
  import { toastError, toastSuccess } from '../../stores/toast'
  import { friendlyError } from '../../lib/errors'
  import { t } from '../../lib/i18n'
  import '../../theme/controls.css'

  export let messageId: number

  const dispatch = createEventDispatcher<{ close: void }>()

  let source = ''
  let loading = true
  let error = ''

  async function load(): Promise<void> {
    loading = true
    error = ''
    try {
      source = await getMessageSource(messageId)
    } catch (err) {
      error = friendlyError(err)
    } finally {
      loading = false
    }
  }

  onMount(load)

  async function copyAll(): Promise<void> {
    try {
      await navigator.clipboard.writeText(source)
      toastSuccess($t('detail.source.copied'))
    } catch {
      toastError($t('detail.source.copyFailed'))
    }
  }
</script>

<Modal title={$t('detail.source.title')} size="large" on:close={() => dispatch('close')}>
  {#if loading}
    <div class="state"><PieProgress size={20} label={$t('detail.source.loading')} /></div>
  {:else if error}
    <ErrorState message={error} onRetry={() => void load()} />
  {:else}
    <pre class="source selectable">{source}</pre>
  {/if}
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn primary" disabled={!source} on:click={copyAll}>{$t('detail.source.copy')}</button>
  </svelte:fragment>
</Modal>

<style>
  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-6);
  }

  .source {
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    line-height: 1.5;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-all;
  }
</style>
