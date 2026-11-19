<script lang="ts">
  // what automatic encryption needs the user to decide before the message can
  // go: a recipient whose key changed, and a reply to encrypted mail that
  // would go out unencrypted. The region stays mounted, so a screen reader
  // hears a notice when it appears.
  import { createEventDispatcher } from 'svelte'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import { changedRecipients, needsPlaintextConfirm, canEncrypt } from '../../../lib/autoencrypt'
  import { t } from '../../../lib/i18n'
  import type { AutoEncryptStatus, RecipientEncryption } from '../../../lib/types'

  /** What the backend knows about these recipients. */
  export let status: AutoEncryptStatus | null = null
  /** Whether the message goes out encrypted. */
  export let encrypt = false
  /** The user already chose to send this reply unencrypted. */
  export let plaintextConfirmed = false

  const dispatch = createEventDispatcher<{
    accept: RecipientEncryption
    plain: void
    encrypt: void
    confirmPlain: void
  }>()

  $: changed = encrypt ? changedRecipients(status) : []
  $: downgrade = needsPlaintextConfirm(status, encrypt, plaintextConfirmed)
  $: possible = canEncrypt(status)

  function changedText(r: RecipientEncryption): string {
    if (!r.changedAt) {
      return $t('autoEncrypt.changed.textNoDate').replace('{email}', r.email)
    }
    const date = new Date(r.changedAt).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })
    return $t('autoEncrypt.changed.text').replace('{email}', r.email).replace('{date}', date)
  }
</script>

<div class="notices" role="status" aria-live="polite" aria-label={$t('autoEncrypt.notices')}>
  {#each changed as r (r.email)}
    <div class="bar">
      <span class="icon"><Ph svg={warnIcon} size={16} /></span>
      <span class="text">{changedText(r)}</span>
      <span class="actions">
        <button type="button" class="c2-tool" on:click={() => dispatch('accept', r)}>{$t('autoEncrypt.changed.accept')}</button>
        <button type="button" class="c2-tool" on:click={() => dispatch('plain')}>{$t('autoEncrypt.changed.plain')}</button>
      </span>
    </div>
  {/each}
  {#if downgrade}
    <div class="bar">
      <span class="icon"><Ph svg={warnIcon} size={16} /></span>
      <span class="text">{$t('autoEncrypt.downgrade.text')}</span>
      <span class="actions">
        {#if possible}
          <button type="button" class="c2-tool" on:click={() => dispatch('encrypt')}>{$t('autoEncrypt.downgrade.encrypt')}</button>
        {/if}
        <button type="button" class="c2-tool" on:click={() => dispatch('confirmPlain')}>{$t('autoEncrypt.downgrade.plain')}</button>
      </span>
    </div>
  {/if}
</div>

<style>
  .notices {
    display: flex;
    flex-direction: column;
  }

  .bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-3);
    margin: var(--space-2) var(--pane-pad) 0;
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--warning);
    border-radius: var(--radius-control);
    background: var(--warning-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  .icon {
    display: inline-flex;
    color: var(--warning);
  }

  .text {
    flex: 1;
    min-width: 0;
  }

  .actions {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-wrap: wrap;
  }
</style>
