<script lang="ts">
  // unlocks the account's secure backup with its recovery key or, when the
  // backup was made with one, its passphrase.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { chatUnlock } from '../../lib/api'
  import { chatError, refreshStatus } from '../../stores/chat'
  import { t } from '../../lib/i18n'

  /** True when the backup also opens with a passphrase, which adds the toggle. */
  export let passphraseAvailable = false

  const dispatch = createEventDispatcher<{ close: void; unlocked: void }>()

  let usePassphrase = false
  let secret = ''
  let busy = false
  let error = ''

  $: mode = usePassphrase && passphraseAvailable
  $: fieldLabel = mode ? $t('chat.unlock.passphrase') : $t('chat.unlock.recoveryKey')

  async function unlock(): Promise<void> {
    if (!secret.trim() || busy) {
      return
    }
    busy = true
    error = ''
    try {
      await chatUnlock(secret.trim(), mode)
      await refreshStatus()
      dispatch('unlocked')
      dispatch('close')
    } catch (err) {
      error = chatError(err)
    } finally {
      busy = false
    }
  }

  function toggle(): void {
    usePassphrase = !usePassphrase
    secret = ''
    error = ''
  }
</script>

<Modal title={$t('chat.unlock.title')} hint={$t('chat.unlock.hint')} size="small" closable={!busy} on:close={() => dispatch('close')}>
  <form id="chat-unlock" on:submit|preventDefault={unlock}>
    <label for="chat-unlock-secret">{fieldLabel}</label>
    <!-- svelte-ignore a11y-autofocus -->
    <input
      id="chat-unlock-secret"
      type={mode ? 'password' : 'text'}
      bind:value={secret}
      autocomplete="off"
      autocapitalize="off"
      spellcheck="false"
      class:mono={!mode}
      disabled={busy}
      autofocus
    />
    {#if passphraseAvailable}
      <button type="button" class="link" disabled={busy} on:click={toggle}>
        {mode ? $t('chat.unlock.useKey') : $t('chat.unlock.usePassphrase')}
      </button>
    {/if}
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
  </form>
  <svelte:fragment slot="footer">
    <button type="button" class="action" disabled={busy} on:click={() => dispatch('close')}>{$t('chat.unlock.cancel')}</button>
    <button type="submit" form="chat-unlock" class="primary" disabled={busy || secret.trim() === ''}>
      {#if busy}
        <PieProgress size={14} label={$t('chat.unlock.unlocking')} />
      {/if}
      {$t('chat.unlock.unlock')}
    </button>
  </svelte:fragment>
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  label {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  input {
    width: 100%;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    outline: none;
    box-sizing: border-box;
  }

  input.mono {
    font-family: var(--font-mono);
  }

  input:focus {
    border-color: var(--accent);
  }

  .error {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .primary,
  .action {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-4);
    border-radius: var(--radius-control);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .primary {
    border: none;
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: var(--fw-semibold);
  }

  .action {
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .action:hover:not(:disabled) {
    background: var(--surface-hover);
  }

  .primary:disabled,
  .action:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .link {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: transparent;
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    color: var(--link);
    cursor: var(--cursor-action);
  }

  .link:hover:not(:disabled) {
    text-decoration: underline;
  }
</style>
