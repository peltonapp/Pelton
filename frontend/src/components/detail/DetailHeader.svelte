<script lang="ts">
  // the header block of the reading pane: subject, sender with avatar, recipients,
  // full date, the technical-info badges the list rows show, and the
  // unsubscribe button when the message advertises a mechanism (or, failing
  // that, contains an unsubscribe link in its body).
  import { IconMailOff, IconCheck, IconStar, IconStarFilled } from '@tabler/icons-svelte'
  import Avatar from '../common/Avatar.svelte'
  import TechBadges from '../common/TechBadges.svelte'
  import { prefs } from '../../stores/prefs'
  import { formatFullDate, displayName, type TimeFormat } from '../../lib/format'
  import { unsubscribeMessage, checkSMIMERevocation } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import { vipSenders, bareAddress, addVIP, removeVIP } from '../../stores/vip'
  import type { MessageDetail, UnsubscribeInfo, SMIMERevocation } from '../../lib/types'

  export let detail: MessageDetail

  // what the signing certificate's authority says about it now. The check runs
  // when a signed message is opened rather than at sync: a certificate valid
  // when the mail arrived can be withdrawn afterwards, which is the case worth
  // catching. The backend answers with an empty status when the setting is off,
  // so asking unconditionally costs nothing.
  let revocation: SMIMERevocation | undefined
  let checkedId = 0

  $: if (detail.smime.status !== '' && detail.id !== checkedId) {
    checkedId = detail.id
    revocation = undefined
    void loadRevocation(detail.id)
  }

  // a failed check is not a verdict, so a rejection leaves the badge as the
  // signature verdict alone rather than raising anything at the reader.
  async function loadRevocation(id: number): Promise<void> {
    try {
      const result = await checkSMIMERevocation(id)
      if (id === checkedId) {
        revocation = result
      }
    } catch {
      // offline, or the store could not be read. Either way, say nothing.
    }
  }

  // the vip store (loaded at startup, updated on every toggle) is the single
  // source of truth, so un-starring reflects immediately. The backend
  // senderVip flag only seeds the store, it must not be OR'd in here or a
  // removed sender would stay lit.
  $: isVip = $vipSenders.has(bareAddress(detail.fromAddress))

  // toggleVip stars or unstars the sender; failures surface as a toast and the
  // store reverts via the reload path.
  async function toggleVip(): Promise<void> {
    try {
      if (isVip) {
        await removeVIP(detail.fromAddress)
        toastSuccess($t('vip.removed').replace('{who}', displayName(detail.fromName, detail.fromAddress)))
      } else {
        await addVIP(detail.fromAddress)
        toastSuccess($t('vip.added').replace('{who}', displayName(detail.fromName, detail.fromAddress)))
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // unsub resolves the mechanism: the stored List-Unsubscribe headers first,
  // otherwise an unsubscribe-looking link scraped from the (already
  // sanitized) body as a browser-link fallback.
  $: unsub = detail.unsubscribe ?? bodyUnsubscribeLink(detail.bodyHtmlSafe)
  $: done = detail.unsubscribe?.done ?? false

  // a mis-click must not silently POST anywhere: the first click arms the
  // button, the second within the window carries it out.
  let confirming = false
  let confirmTimer = 0
  let working = false

  // reset the confirm state when another message opens.
  $: if (detail.id) {
    confirming = false
    working = false
  }

  const unsubWords = /unsubscrib|abmelden|abbestellen|d[eé]sabonn|desuscrib|afmeld/i

  function bodyUnsubscribeLink(html: string): UnsubscribeInfo | null {
    if (!html) {
      return null
    }
    const doc = new DOMParser().parseFromString(html, 'text/html')
    for (const a of Array.from(doc.querySelectorAll('a[href]'))) {
      const href = a.getAttribute('href') ?? ''
      if (!/^https?:/i.test(href)) {
        continue
      }
      if (unsubWords.test(href) || unsubWords.test(a.textContent ?? '')) {
        return { kind: 'link', target: href, done: false }
      }
    }
    return null
  }

  async function onUnsubscribe(): Promise<void> {
    if (!unsub || working || done) {
      return
    }
    if (!confirming) {
      confirming = true
      clearTimeout(confirmTimer)
      confirmTimer = window.setTimeout(() => (confirming = false), 5000)
      return
    }
    clearTimeout(confirmTimer)
    confirming = false
    if (unsub.kind === 'link') {
      openURL(unsub.target)
      return
    }
    working = true
    try {
      await unsubscribeMessage(detail.id)
      done = true
      toastSuccess($t('detail.unsubscribe.done'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      working = false
    }
  }
</script>

<header class="head">
  <!-- the subject is the sender's words, so it reads in its own direction
       whatever the interface is set to (#356). -->
  <h1 dir="auto" class="subject">{detail.subject || $t('detail.noSubject')}</h1>

  <div class="from-row">
    <Avatar name={detail.fromName} email={detail.fromAddress} size={36} />
    <div class="from-info">
      <div class="from-line">
        <button
          type="button"
          class="vip-toggle"
          class:active={isVip}
          on:click={toggleVip}
          title={isVip ? $t('vip.unmark') : $t('vip.mark')}
          aria-pressed={isVip}
        >
          {#if isVip}
            <IconStarFilled size={15} />
          {:else}
            <IconStar size={15} stroke={1.6} />
          {/if}
        </button>
        <span dir="auto" class="from-name">{displayName(detail.fromName, detail.fromAddress)}</span>
        {#if detail.fromName}
          <span class="from-addr">&lt;{detail.fromAddress}&gt;</span>
        {/if}
      </div>
      <div class="recipients">
        {#if detail.toAddresses}<span>{$t('detail.header.to')} {detail.toAddresses}</span>{/if}
        {#if detail.ccAddresses}<span class="cc">{$t('detail.header.cc')} {detail.ccAddresses}</span>{/if}
      </div>
    </div>
    {#if $prefs.showDateTime}
      <time class="date">{formatFullDate(detail.date, $prefs.timeFormat as TimeFormat)}</time>
    {/if}
  </div>

  <div class="badges-row">
    <TechBadges
      accountEmail={detail.accountEmail}
      folderName={detail.folderName}
      pgp={detail.pgp}
      auth={detail.auth}
      smime={detail.smime}
      revocation={revocation}
      charsetGuess={detail.charsetGuess}
    />
    {#if unsub}
      <button type="button" class="unsub" class:confirming disabled={done || working} on:click={onUnsubscribe}>
        {#if done}
          <IconCheck size={13} stroke={2} />
          {$t('detail.unsubscribe.doneLabel')}
        {:else if confirming}
          {unsub.kind === 'link' ? $t('detail.unsubscribe.confirmOpen') : $t('detail.unsubscribe.confirm')}
        {:else}
          <IconMailOff size={13} stroke={1.8} />
          {$t('detail.unsubscribe.button')}
        {/if}
      </button>
    {/if}
  </div>
</header>

<style>
  .head {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding-bottom: var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .subject {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    line-height: 1.3;
    user-select: text;
  }

  .from-row {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }

  .from-info {
    flex: 1;
    min-width: 0;
  }

  .from-line {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    flex-wrap: wrap;
  }

  .vip-toggle {
    align-self: center;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 2px;
    border: none;
    background: none;
    border-radius: var(--radius-sm);
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .vip-toggle:hover {
    background: var(--surface-hover);
    color: var(--text-secondary);
  }

  .vip-toggle.active {
    color: var(--warning);
  }

  .from-name {
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .from-addr {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .recipients {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .recipients span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 48ch;
  }

  .badges-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .unsub {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: 2px var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
  }

  .unsub:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .unsub.confirming {
    border-color: var(--warning);
    color: var(--warning);
  }

  .unsub:disabled {
    color: var(--text-tertiary);
    cursor: default;
  }

  .date {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    flex-shrink: 0;
    white-space: nowrap;
  }
</style>
