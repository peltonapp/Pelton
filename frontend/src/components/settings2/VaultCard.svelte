<script lang="ts">
  // the mail vault settings: what the vault holds, lock and unlock, when it
  // locks by itself, what notifications say, whether scripts and MCP clients
  // may read it, the passphrase, and removing it. The backend checks the
  // passphrase and enforces every switch; this page only shows and asks.
  import Card from './ui/Card.svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Select from '../common/Select.svelte'
  import { changeMailVaultPassphrase, lockAllMailVaults, removeMailVault, updateMailVaultSettings } from '../../lib/mailvaultapi'
  import { mailVaults, refreshMailVaults } from '../../stores/mailvault'
  import { openVaultCreate, openVaultUnlock } from '../../stores/vaultdialog'
  import { onMount } from 'svelte'
  import { sidebar } from '../../stores/accounts'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError, toastInfo } from '../../stores/toast'
  import { accountLabel } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { MailVault, MailVaultAuth } from '../../lib/types'
  import '../../theme/settings2-sieve.css'

  $: vault = $mailVaults[0] as MailVault | undefined

  onMount(() => void refreshMailVaults())

  let changing = false
  let current = ''
  let next = ''
  let repeat = ''
  let error = ''
  let busy = false

  $: idleOptions = [
    { value: '0', label: $t('applock.settings.idleNever') },
    ...[1, 5, 15, 30, 60].map((m) => ({ value: String(m), label: m === 1 ? $t('applock.settings.minute') : $t('applock.settings.minutes').replace('{count}', String(m)) })),
  ]
  $: notifyOptions = [
    { value: 'generic', label: $t('mailvault.settings.notifyGeneric') },
    { value: 'none', label: $t('mailvault.settings.notifyNone') },
  ]

  $: holds = vault ? describeMembers(vault) : ''

  function describeMembers(v: MailVault): string {
    const data = $sidebar.data
    return v.members
      .map((m) => {
        const account = data?.accounts.find((a) => a.id === m.accountId)
        const label = account ? accountLabel(account) : String(m.accountId)
        if (m.folderId === 0) {
          return $t('mailvault.settings.wholeAccount').replace('{account}', label)
        }
        const folder = data?.foldersByAccount[m.accountId]?.find((f) => f.id === m.folderId)
        return `${label} / ${folder?.name ?? m.folderId}`
      })
      .join(', ')
  }

  async function save(change: Partial<MailVault>): Promise<void> {
    if (!vault) {
      return
    }
    const v = { ...vault, ...change }
    try {
      await updateMailVaultSettings({ id: v.id, name: v.name, autoLockMinutes: v.autoLockMinutes, notify: v.notify, hideLocked: v.hideLocked, allowScripts: v.allowScripts, allowMcp: v.allowMcp })
      await refreshMailVaults()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function reason(res: MailVaultAuth): string {
    switch (res.reason) {
      case 'wrong':
        return $t('mailvault.wrong')
      case 'wait':
        return $t('mailvault.wait').replace('{seconds}', String(Math.ceil((res.retryAfterMs ?? 0) / 1000)))
      case 'weak':
        return $t('mailvault.weak')
      default:
        return $t('mailvault.damaged')
    }
  }

  async function changePassphrase(): Promise<void> {
    if (!vault || busy) {
      return
    }
    error = ''
    if (next !== repeat) {
      error = $t('mailvault.mismatch')
      return
    }
    busy = true
    try {
      const res = await changeMailVaultPassphrase(vault.id, current, next)
      if (res.ok) {
        changing = false
        toastInfo($t('mailvault.settings.changed'))
      } else {
        error = reason(res)
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
      current = ''
      next = ''
      repeat = ''
    }
  }

  async function remove(): Promise<void> {
    if (!vault) {
      return
    }
    const ok = await askConfirm({ title: $t('mailvault.settings.remove'), body: $t('mailvault.settings.removeHint'), confirmLabel: $t('mailvault.settings.remove'), danger: true })
    if (!ok) {
      return
    }
    try {
      await removeMailVault(vault.id)
      await refreshMailVaults()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<Card title={$t('mailvault.settings.title')} description={$t('mailvault.settings.description')} id="mail-vault">
  {#if !vault}
    <p class="note">{$t('mailvault.settings.none')}</p>
    <div class="buttons pad">
      <button type="button" class="s2-btn" on:click={() => openVaultCreate(null)}>{$t('mailvault.create.make')}</button>
    </div>
  {:else}
    <Row id="mail-vault-state" label={vault.name || $t('mailvault.defaultName')} hint={$t('mailvault.settings.holds').replace('{list}', holds)}>
      <div class="buttons">
        <span class="state">{vault.locked ? $t('mailvault.settings.locked') : $t('mailvault.settings.unlocked')}</span>
        {#if vault.locked}
          <button type="button" class="s2-btn" on:click={() => vault && openVaultUnlock(vault.id)}>{$t('mailvault.unlock')}</button>
        {:else}
          <button type="button" class="s2-btn" on:click={() => void lockAllMailVaults()}>{$t('mailvault.lockNow')}</button>
        {/if}
      </div>
    </Row>
    {#if vault.state === 'sealing' && vault.pending > 0}
      <p class="note" aria-live="polite">{$t('mailvault.sealing').replace('{done}', String(vault.sealed)).replace('{total}', String(vault.sealed + vault.pending))}</p>
    {:else if vault.state === 'unsealing'}
      <p class="note" aria-live="polite">{$t('mailvault.unsealing').replace('{left}', String(vault.sealed))}</p>
    {/if}
    <Row id="mail-vault-autolock" label={$t('mailvault.settings.autoLock')} hint={$t('mailvault.settings.autoLockHint')}>
      <Select value={String(vault.autoLockMinutes)} items={idleOptions} ariaLabel={$t('mailvault.settings.autoLock')} on:change={(e) => void save({ autoLockMinutes: Number(e.detail) })} />
    </Row>
    <Row id="mail-vault-notify" label={$t('mailvault.settings.notify')}>
      <Select value={vault.notify} items={notifyOptions} ariaLabel={$t('mailvault.settings.notify')} on:change={(e) => void save({ notify: e.detail === 'none' ? 'none' : 'generic' })} />
    </Row>
    <Row id="mail-vault-hide" label={$t('mailvault.settings.hide')} hint={$t('mailvault.settings.hideHint')}>
      <ToggleSwitch checked={vault.hideLocked} label={$t('mailvault.settings.hide')} on:change={(e) => void save({ hideLocked: e.detail })} />
    </Row>
    <Row id="mail-vault-scripts" label={$t('mailvault.settings.scripts')} hint={$t('mailvault.settings.switchHint')}>
      <ToggleSwitch checked={vault.allowScripts} label={$t('mailvault.settings.scripts')} on:change={(e) => void save({ allowScripts: e.detail })} />
    </Row>
    <Row id="mail-vault-mcp" label={$t('mailvault.settings.mcp')} hint={$t('mailvault.settings.switchHint')}>
      <ToggleSwitch checked={vault.allowMcp} label={$t('mailvault.settings.mcp')} on:change={(e) => void save({ allowMcp: e.detail })} />
    </Row>
    {#if changing}
      <form class="lock-form" on:submit|preventDefault={changePassphrase}>
        <input class="sv-input" type="password" autocomplete="off" bind:value={current} placeholder={$t('mailvault.settings.current')} aria-label={$t('mailvault.settings.current')} />
        <input class="sv-input" type="password" autocomplete="new-password" bind:value={next} placeholder={$t('mailvault.newPassphrase')} aria-label={$t('mailvault.newPassphrase')} />
        <input class="sv-input" type="password" autocomplete="new-password" bind:value={repeat} placeholder={$t('mailvault.repeatPassphrase')} aria-label={$t('mailvault.repeatPassphrase')} />
        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}
        <div class="buttons">
          <button type="button" class="s2-btn quiet" on:click={() => (changing = false)}>{$t('settingsPanel.button.cancel')}</button>
          <button type="submit" class="s2-btn primary" disabled={busy}>{$t('mailvault.settings.change')}</button>
        </div>
      </form>
    {/if}
    <Row id="mail-vault-passphrase" label={$t('mailvault.settings.remove')} hint={$t('mailvault.settings.removeHint')}>
      <div class="buttons">
        {#if !changing}
          <button type="button" class="s2-btn" on:click={() => ((changing = true), (error = ''))}>{$t('mailvault.settings.change')}</button>
        {/if}
        <button type="button" class="s2-btn danger" disabled={vault.locked || vault.state === 'unsealing'} on:click={remove}>{$t('mailvault.settings.remove')}</button>
      </div>
    </Row>
  {/if}
  <p class="note">{$t('mailvault.create.limitServer')} {$t('mailvault.create.limitDisk')}</p>
</Card>

<style>
  .note,
  .error {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .error {
    padding: 0;
    color: var(--danger);
  }

  .state {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .lock-form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .buttons {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-3);
  }

  .pad {
    padding: 0 var(--space-4) var(--space-3);
  }
</style>
