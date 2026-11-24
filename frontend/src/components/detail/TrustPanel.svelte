<script lang="ts">
  // the panel under the sender line: the evidence behind the passport in plain
  // words, with the technical values folded away, and what the shield found
  // in the mail with what each would have revealed. Loaded on first open.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import { t } from '../../lib/i18n'
  import { fill, monthYear, passportLine } from '../../lib/passport'
  import {
    blockUnsubscribedSender,
    dismissStillSending,
    markSpam,
    noteUnsubscribeLink,
    setSenderMark,
    unsubscribeMessage,
  } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { errorMessage, toastError } from '../../stores/toast'
  import { openMailScreen } from '../../stores/mailscreen'
  import { imageRequest, loadedRemote, loadTrust, reportFilter } from '../../stores/trust'
  import type { MessageDetail, Passport, Shield, ShieldItem } from '../../lib/types'

  /** The open message. */
  export let detail: MessageDetail
  /** Its passport. */
  export let passport: Passport
  /** Its shield, null when it could not be read. */
  export let shield: Shield | null = null
  /** The part the user opened, scrolled to and focused. */
  export let focus: 'passport' | 'shield' = 'passport'

  const dispatch = createEventDispatcher<{ close: void; announce: string }>()

  let busy = false
  let passportHeading: HTMLElement
  let shieldHeading: HTMLElement

  onMount(async () => {
    await tick()
    ;(focus === 'shield' && shieldHeading ? shieldHeading : passportHeading)?.focus()
  })

  $: p = passport
  $: domain = p.domain || p.address
  $: since = monthYear(p.since)
  $: ev = p.evidence

  async function mark(value: '' | 'known' | 'impostor'): Promise<void> {
    busy = true
    try {
      await setSenderMark(p.address, value)
      await loadTrust(detail.id, true)
      dispatch('announce', fill($t('passport.mark.saved'), { address: p.address }))
      // the button pressed is gone now; focus stays in the panel.
      await tick()
      passportHeading?.focus()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  // the shield's items, with the ordinary remote images folded into one line.
  $: items = shield?.items ?? []
  $: images = items.filter((i) => i.kind === 'image')
  $: listed = items.filter((i) => i.kind !== 'image')
  $: loaded = $loadedRemote.get(detail.id)

  function status(item: ShieldItem): string {
    if (loaded === 'all' && (item.kind === 'pixel' || item.kind === 'image' || item.kind === 'background' || item.kind === 'media')) {
      return 'loaded'
    }
    if (loaded === 'images' && (item.kind === 'image' || item.kind === 'background' || item.kind === 'media')) {
      return 'loaded'
    }
    return item.status
  }

  function reveals(item: ShieldItem): string {
    const key = `shield.reveals.${item.kind}`
    const text = $t(key)
    return text === key ? fill($t('shield.reveals.other'), { host: item.host }) : text
  }

  // why an item was counted: the pixel reasons the remote-content bar
  // already explains, and the link ones of the shield.
  function reason(r: string): string {
    for (const key of [`detail.mailBody.pixelReason.${r}`, `shield.reason.${r}`]) {
      const text = $t(key)
      if (text !== key) {
        return text
      }
    }
    return r
  }

  $: canLoadKeep = !!shield && !shield.remoteAllowed && !loaded && images.length > 0 && items.some((i) => i.kind === 'pixel')

  function loadKeep(): void {
    imageRequest.set(detail.id)
  }

  $: unsub = shield?.unsub ?? null

  async function act(run: () => Promise<unknown>): Promise<void> {
    busy = true
    try {
      await run()
      await loadTrust(detail.id, true)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function retry(): Promise<void> {
    const u = detail.unsubscribe
    if (!u) {
      return
    }
    if (u.kind === 'link') {
      openURL(u.target)
      await noteUnsubscribeLink(detail.id)
      return
    }
    await unsubscribeMessage(detail.id)
  }

  function openReport(): void {
    reportFilter.set(p.address)
    openMailScreen('privacy')
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      dispatch('close')
    }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section class="panel" aria-label={$t('passport.panel')} on:keydown={onKeydown}>
  <h3 tabindex="-1" bind:this={passportHeading}>{$t('passport.panel')}</h3>
  <p class="verdict">{passportLine(p, $t)}</p>

  {#if p.state === 'verified'}
    <p>{fill($t('passport.what.verified'), { domain })}</p>
    <p>{fill($t('passport.what.limits'), { domain })}</p>
  {:else if p.state === 'failed'}
    <p>{fill($t('passport.what.failed'), { domain })}</p>
    {#if ev.dmarcPolicy === 'reject' || ev.dmarcPolicy === 'quarantine'}
      <p>{fill($t(`passport.what.policy.${ev.dmarcPolicy}`), { domain })}</p>
    {/if}
  {:else if p.state === 'lookalike' && p.lookalike}
    <p>
      {p.lookalike.kind === 'name'
        ? fill($t('passport.what.lookalikeName'), { name: p.lookalike.of, address: p.lookalike.address ?? '' })
        : fill($t('passport.what.lookalikeDomain'), { known: p.lookalike.of })}
    </p>
  {:else if p.state !== 'impostor'}
    <p>{$t('passport.what.unchecked')}</p>
  {/if}

  <ul class="facts">
    {#if since}
      <li>
        {p.sent > 0
          ? fill($t('passport.history.both'), { received: p.received, sent: p.sent, date: since })
          : fill($t('passport.history.received'), { received: p.received, date: since })}
      </li>
    {/if}
    {#if p.contact}<li>{$t('passport.history.contact')}</li>{/if}
    {#if ev.mixedScript}<li class="change">{$t('passport.mixedScript')}</li>{/if}
    {#if p.sameDomain}<li>{$t('passport.history.sameDomain')}</li>{/if}
    {#each p.changes as change (change.kind)}
      <li class="change">{fill($t(`passport.change.${change.kind}`), { detail: change.detail, before: change.before ?? '' })}</li>
    {/each}
    {#if p.replyElsewhere}
      <li class="change">{fill($t('passport.replyTo'), { address: p.replyGoesTo })}</li>
    {/if}
  </ul>

  <div class="actions">
    {#if p.mark === ''}
      <button type="button" class="cal-btn" disabled={busy} on:click={() => mark('known')}>{$t('passport.mark.known')}</button>
      <button type="button" class="cal-btn danger" disabled={busy} on:click={() => mark('impostor')}>{$t('passport.mark.impostor')}</button>
    {:else}
      <button type="button" class="cal-btn" disabled={busy} on:click={() => mark('')}>{$t('passport.mark.undo')}</button>
    {/if}
  </div>

  <details class="tech">
    <summary>{$t('passport.tech')}</summary>
    <dl>
      <dt>{$t('passport.tech.checkedBy')}</dt>
      <dd>
        {#if ev.authState === 'trusted'}
          {ev.servId || '-'}
        {:else if ev.authState === 'untrusted'}
          {fill($t('passport.tech.ignored'), { id: ev.servId || '-' })}
        {:else}
          -
        {/if}
      </dd>
      <dt>SPF</dt>
      <dd>{ev.spf || '-'} {ev.spfDomain}{#if ev.alignedSpf}, {$t('passport.tech.aligned')}{/if}</dd>
      <dt>DKIM</dt>
      <dd>{ev.dkim || '-'} {ev.dkimDomains.join(', ')}{#if ev.alignedDkim}, {$t('passport.tech.aligned')}{/if}</dd>
      <dt>DMARC</dt>
      <dd>{ev.dmarc || '-'} {ev.dmarcPolicy ? `p=${ev.dmarcPolicy}` : ''} {ev.headerFrom}</dd>
      {#if ev.returnPath}<dt>Return-Path</dt><dd>{ev.returnPath}</dd>{/if}
      {#if ev.sender}<dt>Sender</dt><dd>{ev.sender}</dd>{/if}
      {#if ev.signers.length > 0}<dt>{$t('passport.tech.signers')}</dt><dd>{ev.signers.join(', ')}</dd>{/if}
      {#if ev.receivedSpf}<dt>Received-SPF</dt><dd>{ev.receivedSpf}</dd>{/if}
      {#if ev.arc}<dt>ARC</dt><dd>{ev.arc}</dd>{/if}
    </dl>
  </details>

  {#if shield}
    <h3 tabindex="-1" bind:this={shieldHeading}>{$t('shield.title')}</h3>
    {#if listed.length === 0 && images.length === 0}
      <p>{$t('shield.none')}</p>
    {:else}
      <ul class="items">
        {#if images.length > 0}
          <li>
            <span class="kind">{$t('shield.kind.image')} ({images.length})</span>
            <span class="status">{$t(`shield.status.${status(images[0])}`)}</span>
            <span class="why">{$t('shield.reveals.image')}</span>
          </li>
        {/if}
        {#each listed as item, i (item.kind + item.url + item.host + i)}
          <li>
            <span class="kind">{$t(`shield.kind.${item.kind}`)}</span>
            <span class="status">{$t(`shield.status.${status(item)}`)}</span>
            <span class="host" dir="ltr">{item.company ? `${item.company} (${item.host})` : item.host}</span>
            {#if item.destination}
              <span class="dest" dir="ltr">{fill($t('shield.goesTo'), { destination: item.destination })}</span>
            {/if}
            {#if item.reasons.length > 0}
              <span class="why">{item.reasons.map(reason).join(', ')}</span>
            {/if}
            <span class="why">{reveals(item)}</span>
          </li>
        {/each}
      </ul>
    {/if}
    <div class="actions">
      {#if canLoadKeep}
        <button type="button" class="cal-btn" on:click={loadKeep}>{$t('shield.loadKeep')}</button>
      {/if}
      <button type="button" class="cal-btn" on:click={openReport}>{$t('privacy.report.title')}</button>
    </div>

    {#if unsub}
      <p class="unsub">{fill($t('shield.unsub.done'), { date: new Date(unsub.at).toLocaleDateString() })}</p>
      {#if unsub.stillSending}
        <p class="still">
          {fill($t('shield.unsub.still'), { name: detail.fromName || unsub.sender, date: new Date(unsub.at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' }) })}
        </p>
        <div class="actions">
          {#if detail.unsubscribe}
            <button type="button" class="cal-btn" disabled={busy} on:click={() => act(retry)}>{$t('shield.unsub.retry')}</button>
          {/if}
          {#if unsub.blocked === ''}
            <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => blockUnsubscribedSender(unsub?.sender ?? '', 'junk'))}>{$t('shield.unsub.junk')}</button>
            <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => blockUnsubscribedSender(unsub?.sender ?? '', 'trash'))}>{$t('shield.unsub.trash')}</button>
          {/if}
          <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => markSpam(detail.id))}>{$t('shield.unsub.spam')}</button>
          <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => dismissStillSending(unsub?.sender ?? ''))}>{$t('shield.unsub.dismiss')}</button>
        </div>
      {/if}
      {#if unsub.blocked !== ''}
        <p class="unsub">
          {$t(`shield.unsub.blocked.${unsub.blocked}`)}
          <button type="button" class="cal-btn" disabled={busy} on:click={() => act(() => blockUnsubscribedSender(unsub?.sender ?? '', ''))}>{$t('shield.unsub.unblock')}</button>
        </p>
      {/if}
    {/if}
  {/if}

  <div class="actions end">
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('action.close')}</button>
  </div>
</section>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    color: var(--text-primary);
    user-select: text;
  }

  h3 {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  h3:first-child {
    margin-top: 0;
  }

  h3:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  p {
    margin: 0;
    color: var(--text-secondary);
  }

  .verdict {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .facts,
  .items {
    margin: 0;
    padding-inline-start: var(--space-4);
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .change,
  .still {
    color: var(--warning);
  }

  .items li {
    display: flex;
    flex-wrap: wrap;
    gap: 0 var(--space-2);
  }

  .kind {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .status {
    color: var(--text-tertiary);
  }

  .why {
    flex-basis: 100%;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .host,
  .dest {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .actions.end {
    justify-content: flex-end;
  }

  .unsub {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }

  .tech summary {
    cursor: var(--cursor-action);
    color: var(--text-secondary);
  }

  .tech dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-1) var(--space-3);
    margin: var(--space-2) 0 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    overflow-wrap: anywhere;
  }

  .tech dt {
    color: var(--text-tertiary);
  }

  .tech dd {
    margin: 0;
    color: var(--text-secondary);
  }
</style>
