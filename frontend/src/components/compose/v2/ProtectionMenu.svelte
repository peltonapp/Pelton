<script lang="ts">
  // signing and encryption for one message, as a toolbar button that opens a
  // small menu of two switches. like the classic picker it only offers what
  // the keys allow, and says which recipient holds encryption back.
  import { createEventDispatcher } from 'svelte'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/lock-simple-open-duotone.svg?raw'
  import signIcon from '@phosphor-icons/core/assets/duotone/seal-check-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import { combineProtection, isEncrypting, isSigning } from '../../../lib/composeactions'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import type { ProtectionStatus } from '../../../lib/types'

  /** The chosen protection: none, sign, encrypt or signencrypt. */
  export let protection = 'none'
  /** What is possible for this account and these recipients; null until known. */
  export let status: ProtectionStatus | null = null
  /** Shows the button as an icon only. */
  export let compact = false
  /** Opens the menu above the button rather than below. */
  export let menuAbove = false

  const dispatch = createEventDispatcher<{ change: string }>()

  let button: HTMLButtonElement
  let open = false

  $: canSign = status?.canSign ?? false
  $: canEncrypt = status?.canEncrypt ?? false
  $: signing = isSigning(protection)
  $: encrypting = isEncrypting(protection)
  $: missing = (status?.recipients ?? []).filter((r) => !r.hasKey).map((r) => r.email)

  $: encryptNote = canEncrypt
    ? $t('compose.protection.encryptTitle')
    : missing.length > 0
      ? $t('compose.protection.noKeyFor').replace('{emails}', missing.join(', '))
      : $t('compose.protection.noRecipients')

  $: signNote = canSign
    ? status?.signerLocked
      ? $t('compose.protection.signLockedTitle')
      : $t('compose.protection.signTitle')
    : $t('compose.protection.noSigningKey')

  $: summary = encrypting && signing
    ? $t('compose2.protection.signEncrypt')
    : encrypting
      ? $t('compose.protection.encrypt')
      : signing
        ? $t('compose.protection.sign')
        : $t('compose.protection.label')
</script>

<button
  type="button"
  class="c2-tool"
  class:active={signing || encrypting}
  class:compact
  bind:this={button}
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-label={$t('compose.protection.label')}
  use:tip={{ text: compact ? summary : '', side: menuAbove ? 'top' : 'bottom' }}
  on:click={() => (open = !open)}
>
  <Ph svg={encrypting ? lockIcon : signing ? signIcon : openIcon} size={16} />
  {#if !compact}<span>{summary}</span>{/if}
</button>

{#if open}
  <AnchoredPopover anchor={button} side={menuAbove ? 'above' : 'below'} align="start" label={$t('compose.protection.label')} on:close={() => (open = false)}>
    <div class="menu">
      <div class="row" class:off={!canSign}>
        <span class="text">
          <span class="name">{$t('compose.protection.sign')}</span>
          <span class="note">{signNote}</span>
        </span>
        <ToggleSwitch
          checked={signing}
          disabled={!canSign}
          label={$t('compose.protection.sign')}
          on:change={(e) => dispatch('change', combineProtection(e.detail, encrypting))}
        />
      </div>
      <div class="row" class:off={!canEncrypt}>
        <span class="text">
          <span class="name">{$t('compose.protection.encrypt')}</span>
          <span class="note">{encryptNote}</span>
        </span>
        <ToggleSwitch
          checked={encrypting}
          disabled={!canEncrypt}
          label={$t('compose.protection.encrypt')}
          on:change={(e) => dispatch('change', combineProtection(signing, e.detail))}
        />
      </div>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .active {
    color: var(--accent);
  }

  .menu {
    display: flex;
    flex-direction: column;
    width: calc(var(--popover-width) - var(--space-6));
    padding: var(--space-2);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3);
  }

  .row + .row {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .note {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .row.off .name {
    color: var(--text-tertiary);
  }
</style>
