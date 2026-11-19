<script lang="ts">
  // the sender passport and the privacy shield under the sender in the new
  // design: one line saying who sent the mail as far as the user's own server
  // and local mail can tell, a chip when something tried to track the reader,
  // and the panel with the evidence behind both, loaded on first open.
  import shieldCheckIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
  import shieldWarningIcon from '@phosphor-icons/core/assets/duotone/shield-warning-duotone.svg?raw'
  import shieldIcon from '@phosphor-icons/core/assets/duotone/shield-duotone.svg?raw'
  import eyeSlashIcon from '@phosphor-icons/core/assets/duotone/eye-slash-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { t } from '../../lib/i18n'
  import { fill, passportLine, passportTone, shieldLabel } from '../../lib/passport'
  import { linkCheck, loadTrust, passports, replyCheck, shields } from '../../stores/trust'
  import type { MessageDetail } from '../../lib/types'

  /** The open message. */
  export let detail: MessageDetail

  let loadedFor = -1
  $: if (detail.id !== loadedFor) {
    loadedFor = detail.id
    open = null
    void loadTrust(detail.id)
  }

  $: passport = $passports.get(detail.id) ?? null
  $: shield = $shields.get(detail.id) ?? null
  $: tone = passport ? passportTone(passport) : 'neutral'
  $: line = passport ? passportLine(passport, $t) : ''
  $: chip = shield ? shieldLabel(shield, $t) : ''
  $: still = shield?.unsub?.stillSending ? shield.unsub : null

  // which part of the panel is open, null while it is closed.
  let open: 'passport' | 'shield' | null = null
  // what the panel last did, read out by the live region below.
  let announce = ''
  const panelId = `trust-panel-${Math.random().toString(36).slice(2)}`

  function toggle(part: 'passport' | 'shield'): void {
    open = open === part ? null : part
  }

  $: checking = ($replyCheck !== null && $replyCheck.detail.id === detail.id) || ($linkCheck !== null && $linkCheck.messageId === detail.id)
</script>

{#if passport || chip}
  <div class="trust">
    {#if passport}
      <button
        type="button"
        class="verdict {tone}"
        aria-expanded={open === 'passport'}
        aria-controls={panelId}
        title={$t('passport.panel')}
        on:click={() => toggle('passport')}
      >
        <Ph svg={tone === 'good' ? shieldCheckIcon : tone === 'neutral' ? shieldIcon : shieldWarningIcon} size={14} />
        <span class="text">{line}</span>
      </button>
    {/if}
    {#if chip}
      <button
        type="button"
        class="chip"
        aria-expanded={open === 'shield'}
        aria-controls={panelId}
        title={$t('shield.title')}
        on:click={() => toggle('shield')}
      >
        <Ph svg={eyeSlashIcon} size={13} />
        {chip}
      </button>
    {/if}
  </div>
  {#if still && open === null}
    <p class="still">
      {fill($t('shield.unsub.still'), { name: detail.fromName || still.sender, date: new Date(still.at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) })}
      <button type="button" class="link" on:click={() => (open = 'shield')}>{$t('shield.unsub.choices')}</button>
    </p>
  {/if}
{/if}

<div id={panelId}>
  {#if open && passport}
    {#await import('./TrustPanel.svelte') then m}
      <svelte:component
        this={m.default}
        {detail}
        {passport}
        {shield}
        focus={open}
        on:close={() => (open = null)}
        on:announce={(e) => (announce = e.detail)}
      />
    {/await}
  {/if}
</div>
<span class="sr-only" role="status" aria-live="polite">{announce}</span>

{#if checking}
  {#await import('./TrustChecks.svelte') then m}
    <svelte:component this={m.default} />
  {/await}
{/if}

<style>
  .trust {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-2);
    min-width: 0;
  }

  .verdict,
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-width: 0;
    max-width: 100%;
    padding: 1px var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-meta);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .verdict:hover,
  .chip:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .verdict:focus-visible,
  .chip:focus-visible,
  .link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .verdict .text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* a verified sender keeps the neutral color: the check is of the sending
     domain, and a green mark would read as a verdict on the content. */
  .verdict.caution :global(.ph) {
    color: var(--warning);
  }

  .verdict.bad {
    border-color: var(--danger);
    color: var(--danger);
  }

  .still {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-meta);
    color: var(--warning);
  }

  .link {
    margin-inline-start: var(--space-2);
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    text-decoration: underline;
    cursor: var(--cursor-action);
  }
</style>
