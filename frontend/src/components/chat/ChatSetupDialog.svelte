<script lang="ts">
  // sets up secure backup for an account that has none, then shows the
  // recovery key once. the dialog cannot be dismissed until the key is saved.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { chatSetupEncryption } from '../../lib/api'
  import { chatError, refreshStatus } from '../../stores/chat'
  import { toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void; done: void }>()

  let passphrase = ''
  let confirmPassphrase = ''
  let password = ''
  let needPassword = false
  let busy = false
  let error = ''
  let recoveryKey = ''
  let saved = false

  $: mismatch = passphrase !== '' && confirmPassphrase !== '' && passphrase !== confirmPassphrase
  $: canSet = !busy && passphrase === confirmPassphrase

  async function setup(): Promise<void> {
    if (!canSet) {
      return
    }
    busy = true
    error = ''
    try {
      recoveryKey = await chatSetupEncryption(passphrase, password)
      await refreshStatus()
    } catch (err) {
      error = chatError(err)
      if (/password/i.test(error)) {
        needPassword = true
      }
    } finally {
      busy = false
    }
  }

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(recoveryKey)
      toastSuccess($t('chat.setup.copied'))
    } catch {
      toastError($t('chat.setup.copyFailed'))
    }
  }

  function done(): void {
    dispatch('done')
    dispatch('close')
  }
</script>

<Modal
  title={$t('chat.setup.title')}
  hint={recoveryKey ? $t('chat.setup.keyHint') : $t('chat.setup.hint')}
  size="small"
  closable={!recoveryKey && !busy}
  on:close={() => dispatch('close')}
>
  {#if recoveryKey}
    <div class="key-box">
      <code class="key" aria-label={$t('chat.setup.recoveryKey')}>{recoveryKey}</code>
      <button type="button" class="action" on:click={copy}>{$t('chat.setup.copy')}</button>
    </div>
    <label class="check">
      <input type="checkbox" bind:checked={saved} />
      <span>{$t('chat.setup.saved')}</span>
    </label>
  {:else}
    <form id="chat-setup" on:submit|preventDefault={setup}>
      <label for="chat-setup-pass">{$t('chat.setup.passphrase')}</label>
      <input id="chat-setup-pass" type="password" bind:value={passphrase} autocomplete="new-password" disabled={busy} />
      <label for="chat-setup-confirm">{$t('chat.setup.confirm')}</label>
      <input id="chat-setup-confirm" type="password" bind:value={confirmPassphrase} autocomplete="new-password" disabled={busy} />
      <p class="note">{mismatch ? $t('chat.setup.mismatch') : $t('chat.setup.passphraseHint')}</p>
      <label for="chat-setup-password">{needPassword ? $t('chat.setup.password') : $t('chat.setup.passwordOptional')}</label>
      <input id="chat-setup-password" type="password" bind:value={password} autocomplete="current-password" disabled={busy} />
      <p class="note">{$t('chat.setup.passwordHint')}</p>
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
    </form>
  {/if}
  <svelte:fragment slot="footer">
    {#if recoveryKey}
      <button type="button" class="primary" disabled={!saved} on:click={done}>{$t('chat.setup.done')}</button>
    {:else}
      <button type="button" class="action" disabled={busy} on:click={() => dispatch('close')}>{$t('chat.setup.cancel')}</button>
      <button type="submit" form="chat-setup" class="primary" disabled={!canSet}>
        {#if busy}
          <PieProgress size={14} label={$t('chat.setup.settingUp')} />
        {/if}
        {$t('chat.setup.setUp')}
      </button>
    {/if}
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

  input[type='password'] {
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

  input[type='password']:focus {
    border-color: var(--accent);
  }

  .note {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 1.4;
  }

  .error {
    margin: 0;
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .key-box {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    align-items: stretch;
  }

  .key {
    display: block;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    line-height: 1.6;
    color: var(--text-primary);
    overflow-wrap: anywhere;
    user-select: all;
  }

  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-3);
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }

  .primary,
  .action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
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
</style>
