<script lang="ts">
  // unlocks a vault with its passphrase, or with the recovery code, which
  // then sets a new passphrase. The backend checks and rate limits; this only
  // asks and says what happened.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import { recoverMailVault, unlockMailVault } from '../../lib/mailvaultapi'
  import { mailVaults, refreshMailVaults } from '../../stores/mailvault'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MailVaultAuth } from '../../lib/types'

  /** The vault to unlock. */
  export let vaultId: number

  const dispatch = createEventDispatcher<{ close: void }>()

  let recovering = false
  let passphrase = ''
  let code = ''
  let next = ''
  let repeat = ''
  let busy = false
  let error = ''

  $: vault = $mailVaults.find((v) => v.id === vaultId)

  /** Says why an attempt failed. */
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

  async function submit(): Promise<void> {
    if (busy) {
      return
    }
    error = ''
    if (recovering && next !== repeat) {
      error = $t('mailvault.mismatch')
      return
    }
    busy = true
    try {
      const res = recovering ? await recoverMailVault(vaultId, code, next) : await unlockMailVault(vaultId, passphrase)
      if (res.ok) {
        await refreshMailVaults()
        dispatch('close')
        return
      }
      error = reason(res)
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
      passphrase = ''
    }
  }
</script>

<Modal title={vault?.name || $t('mailvault.unlock')} hint={recovering ? $t('mailvault.unlock.recoveryHint') : $t('mailvault.unlock.hint')} size="small" on:close={() => dispatch('close')}>
  <svelte:fragment slot="icon"><Ph svg={lockIcon} size={18} /></svelte:fragment>
  <form class="form" on:submit|preventDefault={submit}>
    {#if recovering}
      <input class="cal-field" autocomplete="off" spellcheck="false" bind:value={code} placeholder={$t('mailvault.recoveryCode')} aria-label={$t('mailvault.recoveryCode')} />
      <input class="cal-field" type="password" autocomplete="new-password" bind:value={next} placeholder={$t('mailvault.newPassphrase')} aria-label={$t('mailvault.newPassphrase')} />
      <input class="cal-field" type="password" autocomplete="new-password" bind:value={repeat} placeholder={$t('mailvault.repeatPassphrase')} aria-label={$t('mailvault.repeatPassphrase')} />
    {:else}
      <input class="cal-field" type="password" autocomplete="off" bind:value={passphrase} placeholder={$t('mailvault.passphrase')} aria-label={$t('mailvault.passphrase')} />
    {/if}
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
    <button type="button" class="switch" on:click={() => ((recovering = !recovering), (error = ''))}>
      {recovering ? $t('mailvault.unlock.usePassphrase') : $t('mailvault.unlock.useRecovery')}
    </button>
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('settingsPanel.button.cancel')}</button>
      <button type="submit" class="cal-btn primary" disabled={busy}>{$t('mailvault.unlock.submit')}</button>
    </div>
  </form>
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .error {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .switch {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
  }
</style>
