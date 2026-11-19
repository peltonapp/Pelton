<script lang="ts">
  // a plain line in the new reader saying whether a message was encrypted and
  // what its signature is worth, in words rather than key ids. A key sent
  // from the user's other device gets a box to type its setup code into.
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import sealIcon from '@phosphor-icons/core/assets/duotone/seal-duotone.svg?raw'
  import sealCheckIcon from '@phosphor-icons/core/assets/duotone/seal-check-duotone.svg?raw'
  import sealWarnIcon from '@phosphor-icons/core/assets/duotone/seal-warning-duotone.svg?raw'
  import sealQuestionIcon from '@phosphor-icons/core/assets/duotone/seal-question-duotone.svg?raw'
  import keyIcon from '@phosphor-icons/core/assets/duotone/key-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { importKeyFromSetupMessage } from '../../lib/api'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MessageEncryption } from '../../lib/types'
  import '../../theme/controls.css'

  /** The message's encryption, as the backend described it. */
  export let encryption: MessageEncryption
  /** The message, for importing a key it carries. */
  export let messageId: number

  let code = ''
  let busy = false
  let result = ''
  let failed = false

  $: sig = encryption.signature
  $: signer = encryption.signerEmail || $t('autoEncrypt.reader.someone')
  $: sigText = sig === '' ? '' : $t(`autoEncrypt.reader.sig.${sig}`).replace('{email}', signer)
  $: sigIcon = sig === 'known' || sig === 'verified' ? sealCheckIcon : sig === 'invalid' ? sealWarnIcon : sig === 'unchecked' ? sealQuestionIcon : sealIcon

  async function importKey(): Promise<void> {
    busy = true
    failed = false
    try {
      await importKeyFromSetupMessage(messageId, code)
      result = $t('autoEncrypt.setup.done')
      code = ''
    } catch (err) {
      failed = true
      result = errorMessage(err)
    } finally {
      busy = false
    }
  }
</script>

<section class="crypto" aria-label={$t('autoEncrypt.reader.label')}>
  {#if encryption.encrypted}
    <p class="line ok"><Ph svg={lockIcon} size={14} /><span>{$t('autoEncrypt.reader.encrypted')}</span></p>
  {/if}
  {#if sigText}
    <p class="line {sig}"><Ph svg={sigIcon} size={14} /><span>{sigText}</span></p>
  {/if}
  {#if encryption.subject}
    <p class="line subject">{encryption.subject}</p>
  {/if}
  {#if encryption.setupMessage}
    <form class="setup" on:submit|preventDefault={importKey}>
      <p class="line"><Ph svg={keyIcon} size={14} /><strong>{$t('autoEncrypt.setup.title')}</strong></p>
      <p class="text">{$t('autoEncrypt.setup.text')}</p>
      <div class="field">
        <input
          class="cal-field"
          type="text"
          inputmode="numeric"
          autocomplete="off"
          spellcheck="false"
          aria-label={$t('autoEncrypt.setup.code')}
          placeholder="1234-5678-..."
          bind:value={code}
        />
        <button type="submit" class="cal-btn primary" disabled={busy || code.trim() === ''}>{$t('autoEncrypt.setup.import')}</button>
      </div>
    </form>
  {/if}
  <p class="result" class:failed role="status" aria-live="polite">{result}</p>
</section>

<style>
  .crypto {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0 0 var(--space-3);
    padding-inline-start: var(--space-3);
    border-inline-start: var(--hairline) solid var(--border-default);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .line {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
  }

  .ok,
  .known,
  .verified {
    color: var(--success);
  }

  .invalid {
    color: var(--danger);
  }

  .unchecked {
    color: var(--text-tertiary);
  }

  .subject {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .setup {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }

  .text {
    margin: 0;
  }

  .field {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  .field input {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
  }

  .result {
    margin: 0;
  }

  .failed {
    color: var(--danger);
  }
</style>
