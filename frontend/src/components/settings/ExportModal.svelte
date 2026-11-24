<script lang="ts">
  // Export modal: lets the user pick which categories to include before
  // writing a Pelton backup file. Styled after ImageAllowlistModal so all
  // settings dialogs share the same overlay/card look.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import { IconDownload } from '@tabler/icons-svelte'
  import { exportData } from '../../lib/api'
  import { toastError, toastSuccess, errorMessage } from '../../stores/toast'
  import PieProgress from '../common/PieProgress.svelte'
  import { get } from 'svelte/store'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void }>()

  let includeSettings = true
  let includeWhitelist = true
  let includeMailboxes = false
  let includeSignatures = true
  let exporting = false

  // password-encrypted mailbox credentials are opt-in and only offered once
  // mailboxes are actually being exported; the two password fields must match
  // before export is allowed, the same way a "set a new password" form works
  // anywhere else.
  let includePasswords = false
  let credentialPassword = ''
  let credentialPasswordConfirm = ''
  $: passwordsMismatch = includePasswords && credentialPassword !== credentialPasswordConfirm
  $: passwordTooShort = includePasswords && credentialPassword.length > 0 && credentialPassword.length < 8

  $: if (!includeMailboxes) {
    includePasswords = false
  }

  $: categories = [
    ...(includeSettings ? ['settings'] : []),
    ...(includeWhitelist ? ['whitelist'] : []),
    ...(includeMailboxes ? ['mailboxes'] : []),
    ...(includeSignatures ? ['signatures'] : []),
  ]

  async function runExport(): Promise<void> {
    if (categories.length === 0) {
      toastError(get(t)('importExport.nothingSelected'))
      return
    }
    if (includePasswords && (credentialPassword.length < 8 || passwordsMismatch)) {
      toastError(get(t)('importExport.passwordInvalid'))
      return
    }
    exporting = true
    try {
      const path = await exportData(categories, includePasswords ? credentialPassword : '')
      if (path) {
        toastSuccess(get(t)('importExport.exported'))
        dispatch('close')
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      exporting = false
    }
  }

</script>

<Modal
  title={$t('importExport.exportTitle')}
  hint={$t('importExport.exportHint')}
  size="small"
  on:close={() => dispatch('close')}
>
  <div class="options">
    <label class="check">
      <input type="checkbox" bind:checked={includeSettings} />
      <span>{$t('importExport.category.settings')}</span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={includeWhitelist} />
      <span>{$t('importExport.category.whitelist')}</span>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={includeMailboxes} />
      <span>{$t('importExport.category.mailboxes')}</span>
    </label>
    {#if includeMailboxes}
      <p class="sub-hint">{$t('importExport.mailboxesHint')}</p>
      <label class="check sub">
        <input type="checkbox" bind:checked={includePasswords} />
        <span>{$t('importExport.includePasswords')}</span>
      </label>
      {#if includePasswords}
        <div class="password-fields">
          <p class="sub-hint">{$t('importExport.passwordHint')}</p>
          <input
            type="password"
            class="pw-input"
            placeholder={$t('importExport.passwordPlaceholder')}
            autocomplete="new-password"
            bind:value={credentialPassword}
          />
          <input
            type="password"
            class="pw-input"
            placeholder={$t('importExport.passwordConfirmPlaceholder')}
            autocomplete="new-password"
            bind:value={credentialPasswordConfirm}
          />
          {#if passwordTooShort}
            <p class="pw-warn">{$t('importExport.passwordTooShort')}</p>
          {:else if passwordsMismatch && credentialPasswordConfirm.length > 0}
            <p class="pw-warn">{$t('importExport.passwordMismatch')}</p>
          {/if}
        </div>
      {/if}
    {/if}
    <label class="check">
      <input type="checkbox" bind:checked={includeSignatures} />
      <span>{$t('importExport.category.signatures')}</span>
    </label>
  </div>

  <svelte:fragment slot="footer">
    <button type="button" class="action-btn" on:click={() => dispatch('close')}>
      {$t('modal.close')}
    </button>
    <button
      type="button"
      class="action-btn primary"
      disabled={exporting || (includePasswords && (passwordTooShort || passwordsMismatch || credentialPassword.length === 0))}
      on:click={runExport}
    >
      {#if exporting}
        <PieProgress size={14} label={$t('importExport.exportButton')} />
      {:else}
        <IconDownload size={14} stroke={1.8} />
      {/if}
      {$t('importExport.exportButton')}
    </button>
  </svelte:fragment>
</Modal>

<style>

  .options {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }
  .check input {
    accent-color: var(--accent);
  }

  .sub-hint {
    margin: 0 0 0 calc(var(--space-2) + 14px);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 1.4;
  }

  .check.sub {
    margin-inline-start: calc(var(--space-2) + 14px);
  }

  .password-fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-inline-start: calc(var(--space-2) + 14px);
  }

  .pw-input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-list);
  }
  .pw-input:focus {
    border-color: var(--accent);
    outline: none;
  }

  .pw-warn {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--danger, var(--warning));
  }

  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }
  .action-btn:hover {
    background: var(--surface-hover);
  }
  .action-btn:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .action-btn.primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }
</style>
