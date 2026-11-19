<script lang="ts">
  // makes a vault in three steps: what it protects and what it does not, the
  // passphrase, and the recovery code, which is shown once and cannot be left
  // behind without saying so. Sealing then runs in the background; the sidebar
  // and the settings show how far it has got.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import { createMailVault } from '../../lib/api'
  import { passphraseStrength } from '../../lib/vaultstrength'
  import { refreshMailVaults } from '../../stores/mailvault'
  import { sidebar } from '../../stores/accounts'
  import { errorMessage } from '../../stores/toast'
  import { accountLabel } from '../../lib/format'
  import { t } from '../../lib/i18n'
  import type { MailVaultMember } from '../../lib/types'

  /** The folder or account to protect; null lets the user pick one. */
  export let member: MailVaultMember | null

  const dispatch = createEventDispatcher<{ close: void }>()
  const minPassphrase = 8

  let step: 'explain' | 'passphrase' | 'recovery' = 'explain'
  let name = ''
  let passphrase = ''
  let repeat = ''
  let busy = false
  let error = ''
  let recoveryCode = ''
  let saved = false
  let accepted = false
  let picked = member ? `${member.accountId}:${member.folderId}` : ''

  $: data = $sidebar.data
  $: targets = (data?.accounts ?? [])
    .filter((a) => !a.local)
    .flatMap((a) => [
      { value: `${a.id}:0`, label: $t('mailvault.settings.wholeAccount').replace('{account}', accountLabel(a)) },
      ...(data?.foldersByAccount[a.id] ?? []).map((f) => ({ value: `${a.id}:${f.id}`, label: `${accountLabel(a)} / ${f.name}` })),
    ])
  $: if (!picked && targets.length > 0) {
    picked = targets[0].value
  }
  $: strength = passphraseStrength(passphrase)
  $: canLeave = saved || accepted

  function chosen(): MailVaultMember | null {
    const [accountId, folderId] = picked.split(':').map(Number)
    return accountId ? { accountId, folderId } : null
  }

  async function create(): Promise<void> {
    error = ''
    if ([...passphrase].length < minPassphrase) {
      error = $t('mailvault.weak')
      return
    }
    if (passphrase !== repeat) {
      error = $t('mailvault.mismatch')
      return
    }
    const target = chosen()
    if (!target || busy) {
      return
    }
    busy = true
    try {
      const res = await createMailVault({ name: name.trim(), passphrase, members: [target], autoLockMinutes: 15, notify: 'generic', hideLocked: false })
      recoveryCode = res.recoveryCode
      step = 'recovery'
      await refreshMailVaults()
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
      passphrase = ''
      repeat = ''
    }
  }

  function finish(): void {
    recoveryCode = ''
    dispatch('close')
  }
</script>

<Modal
  title={step === 'recovery' ? $t('mailvault.recovery.title') : $t('mailvault.create.title')}
  size="medium"
  closable={step !== 'recovery' || canLeave}
  on:close={() => (step === 'recovery' ? canLeave && finish() : dispatch('close'))}
>
  {#if step === 'explain'}
    <section class="text">
      <h3>{$t('mailvault.create.protectsTitle')}</h3>
      <p>{$t('mailvault.create.protects')}</p>
      <h3>{$t('mailvault.create.limitsTitle')}</h3>
      <ul>
        <li>{$t('mailvault.create.limitServer')}</li>
        <li>{$t('mailvault.create.limitExported')}</li>
        <li>{$t('mailvault.create.limitUnlocked')}</li>
        <li>{$t('mailvault.create.limitDisk')}</li>
        <li>{$t('mailvault.create.limitMeta')}</li>
      </ul>
    </section>
  {:else if step === 'passphrase'}
    <form class="form" id="vault-create" on:submit|preventDefault={create}>
      <p class="hint">{$t('mailvault.create.passphraseHint')}</p>
      {#if !member}
        <Select value={picked} items={targets} ariaLabel={$t('mailvault.settings.title')} on:change={(e) => (picked = e.detail)} />
      {/if}
      <input class="cal-field" bind:value={name} placeholder={$t('mailvault.defaultName')} aria-label={$t('mailvault.create.name')} />
      <input class="cal-field" type="password" autocomplete="new-password" bind:value={passphrase} placeholder={$t('mailvault.passphrase')} aria-label={$t('mailvault.passphrase')} />
      <p class="strength {strength}" aria-live="polite">{$t('mailvault.strength').replace('{level}', $t(`mailvault.strength.${strength}`))}</p>
      <input class="cal-field" type="password" autocomplete="new-password" bind:value={repeat} placeholder={$t('mailvault.repeatPassphrase')} aria-label={$t('mailvault.repeatPassphrase')} />
      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}
    </form>
  {:else}
    <section class="text">
      <p>{$t('mailvault.recovery.hint')}</p>
      <output class="code" aria-label={$t('mailvault.recoveryCode')}>{recoveryCode}</output>
      <label class="check"><input type="checkbox" checked={saved} on:change={(e) => (saved = e.currentTarget.checked)} /> {$t('mailvault.recovery.saved')}</label>
      <label class="check"><input type="checkbox" checked={accepted} on:change={(e) => (accepted = e.currentTarget.checked)} /> {$t('mailvault.recovery.skip')}</label>
    </section>
  {/if}

  <svelte:fragment slot="footer">
    {#if step === 'explain'}
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('settingsPanel.button.cancel')}</button>
      <button type="button" class="cal-btn primary" on:click={() => (step = 'passphrase')}>{$t('mailvault.create.continue')}</button>
    {:else if step === 'passphrase'}
      <button type="button" class="cal-btn" on:click={() => (step = 'explain')}>{$t('wizard.back')}</button>
      <button type="submit" form="vault-create" class="cal-btn primary" disabled={busy}>{$t('mailvault.create.make')}</button>
    {:else}
      <button type="button" class="cal-btn primary" disabled={!canLeave} on:click={finish}>{$t('mailvault.recovery.done')}</button>
    {/if}
  </svelte:fragment>
</Modal>

<style>
  .text,
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    font-size: var(--fz-body);
    line-height: 1.5;
    color: var(--text-secondary);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  p,
  ul {
    margin: 0;
  }

  ul {
    padding-inline-start: var(--space-5);
  }

  .hint {
    font-size: var(--fz-label);
  }

  .strength {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .strength.weak {
    color: var(--danger);
  }

  .strength.fair {
    color: var(--warning);
  }

  .strength.good,
  .strength.strong {
    color: var(--success);
  }

  .error {
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .code {
    display: block;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-heading);
    letter-spacing: 0.08em;
    text-align: center;
    color: var(--text-primary);
    user-select: all;
  }

  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }
</style>
