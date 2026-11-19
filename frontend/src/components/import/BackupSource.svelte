<script lang="ts">
  // restoring from a Pelton backup file: pick the file, see what it holds, tick
  // what to bring in. Import writes straight into the live config, so the
  // button asks first and names what is about to be replaced. Pelton only reads
  // the file the user chose; nothing leaves the machine.
  import { get } from 'svelte/store'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-arrow-down-duotone.svg?raw'
  import { inspectBackupFile, importData, type BackupInfo } from '../../lib/api'
  import { initPrefs, prefs } from '../../stores/prefs'
  import { toastError, errorMessage } from '../../stores/toast'
  import { formatFullDate, type TimeFormat } from '../../lib/format'
  import { t } from '../../lib/i18n'

  let file: BackupInfo | null = null
  let importSettings = true
  let importWhitelist = true
  let importMailboxes = false
  let importSignatures = true
  let importing = false
  let imported = false

  // only relevant when the file carries encrypted mailbox credentials and the
  // user wants them restored too.
  let importPasswords = false
  let credentialPassword = ''

  let confirming = false

  $: selectedLabels = file
    ? [
        ...(importSettings && file.hasSettings ? [$t('importExport.category.settings')] : []),
        ...(importWhitelist && file.hasWhitelist ? [$t('importExport.category.whitelist')] : []),
        ...(importMailboxes && file.hasMailboxes ? [$t('importExport.category.mailboxes')] : []),
        ...(importSignatures && file.hasSignatures ? [$t('importExport.category.signatures')] : []),
        ...(importPasswords && file.hasEncryptedCredentials ? [$t('importExport.restorePasswords')] : []),
      ]
    : []

  async function chooseFile(): Promise<void> {
    try {
      const info = await inspectBackupFile()
      if (!info.path) {
        return
      }
      file = info
      importSettings = info.hasSettings
      importWhitelist = info.hasWhitelist
      importMailboxes = false
      importSignatures = info.hasSignatures
      importPasswords = false
      confirming = false
      imported = false
      credentialPassword = ''
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function runImport(): Promise<void> {
    if (!file) {
      return
    }
    const categories = [
      ...(importSettings && file.hasSettings ? ['settings'] : []),
      ...(importWhitelist && file.hasWhitelist ? ['whitelist'] : []),
      ...(importMailboxes && file.hasMailboxes ? ['mailboxes'] : []),
      ...(importSignatures && file.hasSignatures ? ['signatures'] : []),
    ]
    if (categories.length === 0) {
      toastError(get(t)('importExport.nothingSelected'))
      return
    }
    if (importPasswords && credentialPassword.length === 0) {
      toastError(get(t)('importExport.passwordInvalid'))
      return
    }
    importing = true
    confirming = false
    try {
      await importData(file.path, categories, importPasswords ? credentialPassword : '')
      // the settings just written drive the ui; reload them so it updates live.
      await initPrefs()
      imported = true
      file = null
      credentialPassword = ''
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      importing = false
    }
  }
</script>

<div class="source">
  <p class="hint">{$t('importExport.importHint')}</p>
  <div class="pick">
    <FlowButton disabled={importing} on:click={chooseFile}>
      <Ph svg={fileIcon} size={16} />
      {$t('importExport.chooseFile')}
    </FlowButton>
    {#if file}
      <span class="file-meta">
        {#if file.createdAt}{$t('importExport.created')} {formatFullDate(file.createdAt, $prefs.timeFormat as TimeFormat)}{/if}
        {#if file.appVersion}<span class="version">{file.appVersion}</span>{/if}
      </span>
    {/if}
  </div>

  {#if imported}
    <p class="done">{$t('importExport.imported')}</p>
  {/if}

  {#if file}
    <div class="checks">
      <label class="check" class:disabled={!file.hasSettings}>
        <input type="checkbox" bind:checked={importSettings} disabled={!file.hasSettings} />
        <span>{$t('importExport.category.settings')}{#if file.hasSettings}&nbsp;({file.settingCount}){/if}</span>
      </label>
      <label class="check" class:disabled={!file.hasWhitelist}>
        <input type="checkbox" bind:checked={importWhitelist} disabled={!file.hasWhitelist} />
        <span>{$t('importExport.category.whitelist')}</span>
      </label>
      <label class="check" class:disabled={!file.hasMailboxes}>
        <input type="checkbox" bind:checked={importMailboxes} disabled={!file.hasMailboxes} />
        <span>{$t('importExport.category.mailboxes')}{#if file.hasMailboxes}&nbsp;({file.mailboxCount}){/if}</span>
      </label>
      {#if importMailboxes && file.hasMailboxes}
        <div class="sub">
          <p class="hint">{$t('importExport.mailboxesHint')}</p>
          {#if file.hasEncryptedCredentials}
            <label class="check">
              <input type="checkbox" bind:checked={importPasswords} />
              <span>{$t('importExport.restorePasswords')}</span>
            </label>
            {#if importPasswords}
              <input
                type="password"
                class="pw-input"
                placeholder={$t('importExport.passwordPlaceholder')}
                aria-label={$t('importExport.passwordPlaceholder')}
                autocomplete="current-password"
                bind:value={credentialPassword}
              />
            {/if}
          {/if}
        </div>
      {/if}
      <label class="check" class:disabled={!file.hasSignatures}>
        <input type="checkbox" bind:checked={importSignatures} disabled={!file.hasSignatures} />
        <span>{$t('importExport.category.signatures')}{#if file.hasSignatures}&nbsp;({file.signatureCount}){/if}</span>
      </label>
    </div>

    {#if confirming}
      <div class="confirm" role="alert">
        <span class="confirm-title">{$t('importExport.confirmTitle')}</span>
        <ul>
          {#each selectedLabels as label}
            <li>{label}</li>
          {/each}
        </ul>
        <p class="confirm-warn">{$t('importExport.confirmWarning')}</p>
        <div class="actions">
          <FlowButton on:click={() => (confirming = false)}>{$t('importExport.cancel')}</FlowButton>
          <FlowButton variant="primary" disabled={importing} on:click={runImport}>{$t('importExport.confirmButton')}</FlowButton>
        </div>
      </div>
    {:else}
      <div class="actions">
        <FlowButton
          variant="primary"
          disabled={importing || (importPasswords && credentialPassword.length === 0) || selectedLabels.length === 0}
          on:click={() => (confirming = true)}
        >
          {#if importing}<PieProgress size={12} label={$t('import.importing')} />{/if}
          {$t('importExport.importButton')}
        </FlowButton>
      </div>
    {/if}
  {/if}
</div>

<style>
  .source {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-tertiary);
  }

  .pick {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .file-meta {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .version {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .done {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--success);
    background: var(--success-bg);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .checks {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .check {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
  }

  .check.disabled {
    color: var(--text-tertiary);
    cursor: default;
  }

  .check input {
    accent-color: var(--accent);
  }

  .sub {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding-inline-start: var(--space-6);
  }

  .pw-input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-list);
    outline: none;
  }

  .pw-input:focus {
    border-color: var(--accent);
  }

  .confirm {
    padding: var(--space-4);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
  }

  .confirm-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .confirm ul {
    margin: var(--space-3) 0;
    padding-inline-start: var(--space-6);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .confirm-warn {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
</style>
