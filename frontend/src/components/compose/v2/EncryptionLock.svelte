<script lang="ts">
  // the lock of automatic encryption: a toolbar button that says in a word
  // whether the message goes out encrypted, and a menu that says why, lists
  // every recipient with where their key came from, and turns encryption on
  // or off for this message.
  import { createEventDispatcher } from 'svelte'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/lock-simple-open-duotone.svg?raw'
  import checkedIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import AnchoredPopover from '../../common/AnchoredPopover.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Segmented from '../../settings2/ui/Segmented.svelte'
  import { canEncrypt, lockSummary, sourceWords, type EncryptOverride } from '../../../lib/autoencrypt'
  import { tip } from '../../../lib/tip'
  import { t } from '../../../lib/i18n'
  import type { AutoEncryptStatus, ContactEncryptPref, RecipientEncryption } from '../../../lib/types'

  /** What the backend knows about these recipients; null until the first answer. */
  export let status: AutoEncryptStatus | null = null
  /** Whether the message goes out encrypted. */
  export let encrypt = false
  /** The user's choice for this message; unset follows the recommendation. */
  export let override: EncryptOverride = undefined
  /** Shows the button as an icon only. */
  export let compact = false
  /** Opens the menu above the button rather than below. */
  export let menuAbove = false

  // override is on or off for this message, or auto to follow the
  // recommendation again.
  const dispatch = createEventDispatcher<{
    override: 'on' | 'off' | 'auto'
    preference: { email: string; pref: ContactEncryptPref }
  }>()

  let button: HTMLButtonElement
  let open = false

  $: summary = lockSummary(status, encrypt)
  $: possible = canEncrypt(status)
  $: hasBcc = (status?.recipients ?? []).some((r) => r.bcc)

  $: word =
    summary.kind === 'encrypted'
      ? summary.verified
        ? $t('autoEncrypt.lock.verified')
        : $t('autoEncrypt.lock.encrypted')
      : summary.kind === 'blocked'
        ? $t('autoEncrypt.lock.blocked')
        : summary.kind === 'nobody'
          ? $t('autoEncrypt.lock.label')
          : $t('autoEncrypt.lock.plain')

  $: icon = summary.kind === 'encrypted' ? (summary.verified ? checkedIcon : lockIcon) : summary.kind === 'blocked' ? warnIcon : openIcon

  $: sentence = describe(summary)

  function describe(s: typeof summary): string {
    switch (s.kind) {
      case 'nobody':
        return $t('autoEncrypt.summary.nobody')
      case 'noOwnKey':
        return $t('autoEncrypt.summary.noOwnKey')
      case 'encrypted':
        return s.verified ? $t('autoEncrypt.summary.verified') : $t('autoEncrypt.summary.encrypted')
      case 'blocked':
        return s.missing.length > 0
          ? $t('autoEncrypt.summary.blockedMissing').replace('{emails}', s.missing.join(', '))
          : $t('autoEncrypt.summary.blockedChanged').replace('{emails}', s.changed.join(', '))
      case 'plainMissing': {
        const total = s.withKey + s.missing.length
        const first = $t('autoEncrypt.summary.plainMissing').replace('{emails}', s.missing.join(', '))
        return s.withKey > 0 ? `${first} ${$t('autoEncrypt.summary.plainSome').replace('{n}', String(s.withKey)).replace('{total}', String(total))}` : first
      }
      case 'plainChoice':
        return override === 'off' ? $t('autoEncrypt.summary.plainOff') : $t('autoEncrypt.summary.plainChoice')
    }
  }

  function stateWords(r: RecipientEncryption): string {
    const source = sourceWords(r.source)
    const from = $t(source.key).replace('{server}', source.server)
    switch (r.state) {
      case 'ready':
        return $t('autoEncrypt.recipient.ready').replace('{source}', from)
      case 'verified':
        return $t('autoEncrypt.recipient.verified').replace('{source}', from)
      case 'changed':
        return $t('autoEncrypt.recipient.changed')
      default:
        return r.problem ? $t('autoEncrypt.recipient.missingWhy').replace('{why}', $t(`autoEncrypt.problem.${r.problem}`)) : $t('autoEncrypt.recipient.missing')
    }
  }

  $: prefOptions = [
    { key: '', label: $t('autoEncrypt.pref.default') },
    { key: 'always', label: $t('autoEncrypt.pref.always') },
    { key: 'never', label: $t('autoEncrypt.pref.never') },
  ]

  function toggle(on: boolean): void {
    dispatch('override', on ? 'on' : 'off')
  }
</script>

<button
  type="button"
  class="c2-tool"
  class:on={summary.kind === 'encrypted'}
  class:warn={summary.kind === 'blocked'}
  class:compact
  bind:this={button}
  aria-haspopup="dialog"
  aria-expanded={open}
  aria-label={`${$t('autoEncrypt.lock.label')}: ${word}`}
  use:tip={{ text: compact ? word : '', side: menuAbove ? 'top' : 'bottom' }}
  on:click={() => (open = !open)}
>
  <Ph svg={icon} size={16} />
  {#if !compact}<span>{word}</span>{/if}
</button>

{#if open}
  <AnchoredPopover anchor={button} side={menuAbove ? 'above' : 'below'} align="start" label={$t('autoEncrypt.lock.label')} on:close={() => (open = false)}>
    <div class="menu">
      <p class="sentence">{sentence}</p>
      <div class="row">
        <span class="name">{$t('autoEncrypt.toggle')}</span>
        <ToggleSwitch checked={encrypt} disabled={!encrypt && !possible} label={$t('autoEncrypt.toggle')} on:change={(e) => toggle(e.detail)} />
      </div>
      {#if override !== undefined}
        <button type="button" class="link" on:click={() => dispatch('override', 'auto')}>{$t('autoEncrypt.toggle.auto')}</button>
      {/if}
      {#if status && status.recipients.length > 0}
        <ul class="people">
          {#each status.recipients as r (r.email)}
            <li class="person" class:missing={r.state === 'missing'} class:changed={r.state === 'changed'}>
              <span class="who">
                <span class="email">{r.email}</span>
                {#if r.bcc}<span class="tag">{$t('autoEncrypt.recipient.bcc')}</span>{/if}
              </span>
              <span class="state">{stateWords(r)}</span>
              <Segmented
                value={r.preference}
                options={prefOptions}
                label={$t('autoEncrypt.pref.label').replace('{email}', r.email)}
                on:change={(e) => dispatch('preference', { email: r.email, pref: e.detail as ContactEncryptPref })}
              />
            </li>
          {/each}
        </ul>
      {/if}
      {#if hasBcc && encrypt}
        <p class="note">{$t('autoEncrypt.bccNote')}</p>
      {/if}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .on {
    color: var(--accent);
  }

  .warn {
    color: var(--warning);
  }

  .menu {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: calc(var(--popover-width) + var(--space-6));
    padding: var(--space-3);
  }

  .sentence {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .name {
    flex: 1;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
  }

  .link {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .people {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .person {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .who {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }

  .email {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
  }

  .tag {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .state {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .missing .state,
  .changed .state {
    color: var(--warning);
  }

  .note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
