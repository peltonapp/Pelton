<script lang="ts">
  // the shared confirmation prompt, driven by askConfirm. It lives once at the
  // app root: the action that needs the answer is somewhere in a store or a
  // menu callback, not in a component that could host a dialog of its own.
  import Modal from './Modal.svelte'
  import { confirmRequest } from '../../stores/confirm'
  import { t } from '../../lib/i18n'

  $: request = $confirmRequest
</script>

{#if request}
  <Modal title={request.title} hint={request.body} size="small" on:close={() => request.resolve(false)}>
    <svelte:fragment slot="footer">
      <button type="button" class="btn" on:click={() => request.resolve(false)}>{request.cancelLabel || $t('confirm.cancel')}</button>
      <button type="button" class="btn primary" class:danger={request.danger} on:click={() => request.resolve(true)}>
        {request.confirmLabel}
      </button>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .btn {
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .btn:hover {
    background: var(--surface-hover);
  }

  .btn.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary.danger {
    border-color: var(--danger);
    background: var(--danger);
  }
</style>
