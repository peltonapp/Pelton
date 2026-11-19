<script lang="ts">
  // other people's keys: where each came from and when, the user's choice for
  // each contact, comparing a fingerprint for those who want to, and
  // forgetting a key.
  import { onDestroy, onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import Segmented from './ui/Segmented.svelte'
  import Modal from '../common/Modal.svelte'
  import { forgetContactKey, listContactKeys, setContactEncryption, verifyContactKey } from '../../lib/api'
  import { onAutoEncryptChanged } from '../../lib/events'
  import { groupFingerprint, sourceWords } from '../../lib/autoencrypt'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ContactEncryptPref, ContactKey } from '../../lib/types'
  import '../../theme/controls.css'

  let keys: ContactKey[] = []
  let loaded = false
  let comparing: ContactKey | null = null

  async function load(): Promise<void> {
    try {
      keys = await listContactKeys()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  onMount(() => void load())
  const stop = onAutoEncryptChanged(() => void load())
  onDestroy(stop)

  $: prefOptions = [
    { key: '', label: $t('autoEncrypt.pref.default') },
    { key: 'always', label: $t('autoEncrypt.pref.always') },
    { key: 'never', label: $t('autoEncrypt.pref.never') },
  ]

  function from(key: ContactKey): string {
    const s = sourceWords(key.source)
    return $t(s.key).replace('{server}', s.server)
  }

  function line(key: ContactKey): string {
    const parts = [from(key)]
    if (key.firstSeen) {
      parts.push($t('autoEncrypt.contacts.firstSeen').replace('{date}', new Date(key.firstSeen).toLocaleDateString()))
    }
    if (key.state === 'verified') {
      parts.push($t('autoEncrypt.lock.verified'))
    } else if (key.state === 'changed') {
      parts.push($t('autoEncrypt.recipient.changed'))
    } else if (key.problem) {
      parts.push($t(`autoEncrypt.problem.${key.problem}`))
    }
    return parts.join(', ')
  }

  async function setPref(key: ContactKey, pref: ContactEncryptPref): Promise<void> {
    try {
      await setContactEncryption(key.email, pref)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function matched(): Promise<void> {
    if (!comparing) {
      return
    }
    const key = comparing
    comparing = null
    try {
      await verifyContactKey(key.email, key.fingerprint)
      toastSuccess($t('autoEncrypt.verify.done').replace('{email}', key.email))
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function forget(key: ContactKey): Promise<void> {
    const ok = await askConfirm({
      title: $t('autoEncrypt.contacts.forgetLabel').replace('{email}', key.email),
      body: $t('autoEncrypt.contacts.forgetBody').replace('{email}', key.email),
      confirmLabel: $t('autoEncrypt.contacts.forget'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await forgetContactKey(key.email)
      await load()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<Card id="contact-keys" title={$t('autoEncrypt.contacts.title')} description={$t('autoEncrypt.contacts.hint')}>
  {#if loaded && keys.length === 0}
    <p class="empty">{$t('autoEncrypt.contacts.empty')}</p>
  {:else}
    <ul class="keys">
      {#each keys as key (key.email)}
        <li>
          <div class="text">
            <span class="email">{key.email}</span>
            <span class="meta" class:warn={key.state === 'changed' || key.problem !== ''}>{line(key)}</span>
          </div>
          <div class="controls">
            <Segmented
              value={key.preference}
              options={prefOptions}
              label={$t('autoEncrypt.pref.label').replace('{email}', key.email)}
              on:change={(e) => void setPref(key, e.detail as ContactEncryptPref)}
            />
            {#if key.fingerprint && key.state !== 'changed'}
              <button type="button" class="cal-btn" aria-label={$t('autoEncrypt.contacts.verifyLabel').replace('{email}', key.email)} on:click={() => (comparing = key)}>
                {$t('autoEncrypt.contacts.verify')}
              </button>
            {/if}
            <button type="button" class="cal-btn" aria-label={$t('autoEncrypt.contacts.forgetLabel').replace('{email}', key.email)} on:click={() => void forget(key)}>
              {$t('autoEncrypt.contacts.forget')}
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</Card>

{#if comparing}
  <Modal title={$t('autoEncrypt.verify.title').replace('{email}', comparing.email)} size="small" on:close={() => (comparing = null)}>
    <div class="dialog">
      <p>{$t('autoEncrypt.verify.text').replace('{email}', comparing.email)}</p>
      <code class="fp">{groupFingerprint(comparing.fingerprint)}</code>
    </div>
    <svelte:fragment slot="footer">
      <button type="button" class="cal-btn primary" on:click={() => void matched()}>{$t('autoEncrypt.verify.match')}</button>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .empty {
    margin: 0;
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .keys {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-4);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  li:last-child {
    border-bottom: none;
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  .email {
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .meta {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .warn {
    color: var(--warning);
  }

  .controls {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  .dialog {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .dialog p {
    margin: 0;
    line-height: 1.5;
  }

  .fp {
    font-family: var(--font-mono);
    font-size: var(--fz-body);
    color: var(--text-primary);
    word-spacing: 0.4em;
    line-height: 1.8;
  }
</style>
