<script lang="ts">
  // the invitation a message carries, above its body in the new design: what
  // and when, who invites, and the answer buttons. accepting or tentatively
  // accepting puts the event in a calendar, declining takes it out, and the
  // reply goes to the organizer through the outbox when asked to. a
  // cancellation or an attendee's reply is applied to the stored copy.
  import { onMount } from 'svelte'
  import { fade } from 'svelte/transition'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import userIcon from '@phosphor-icons/core/assets/duotone/user-circle-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/regular/check.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { parseInvite, answerInvite, applyInvite } from '../../lib/api'
  import { calendars, loadCalendars, calendarFocus } from '../../stores/calendar'
  import { openSpace } from '../../stores/space'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { parseEventTime } from '../../lib/caldate'
  import { formatWhen } from '../../lib/calformat'
  import { describeRRule } from '../../lib/recurrence'
  import { partStatKey } from '../calendar/labels'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Attachment, Invite, InviteResponse } from '../../lib/types'

  /** The message shown. */
  export let messageId: number
  /** Its attachments; the card only looks for an invitation when one is calendar data. */
  export let attachments: Attachment[]

  let invite: Invite | null = null
  let loadedFor = -1
  let busy: InviteResponse | 'apply' | null = null
  let sendReply = true
  let calendarId = 0

  $: carries = attachments.some((a) => {
    const type = a.contentType.toLowerCase()
    return type === 'text/calendar' || type === 'application/ics' || a.filename.toLowerCase().endsWith('.ics')
  })

  $: if (carries && messageId !== loadedFor) {
    loadedFor = messageId
    void load(messageId)
  }
  $: if (!carries) {
    invite = null
    loadedFor = -1
  }

  onMount(() => {
    if ($calendars.length === 0) {
      void loadCalendars()
    }
  })

  async function load(id: number): Promise<void> {
    try {
      const found = await parseInvite(id)
      if (id === messageId) {
        invite = found.found ? found : null
      }
    } catch {
      if (id === messageId) {
        invite = null
      }
    }
  }

  $: writable = $calendars.filter((c) => !c.readOnly)
  $: calendarItems = [{ value: '0', label: $t('invite.defaultCalendar') }, ...writable.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName') }))]
  $: start = invite ? parseEventTime(invite.start, invite.allDay) : null
  $: end = invite ? parseEventTime(invite.end, invite.allDay) : null
  $: when = invite && start && end ? formatWhen(start, end, invite.allDay, $prefs.timeFormat as TimeFormat) : ''
  $: repeat = invite?.rrule && start ? describeRRule(invite.rrule, start, $t, navigator.language) : ''
  $: method = invite?.method ?? ''
  $: answerable = method === 'REQUEST' || method === 'PUBLISH' || method === ''
  $: canReply = method === 'REQUEST' && !!invite?.organizer.email
  $: replier = method === 'REPLY' ? invite?.attendees[0] : undefined
  $: heading = headingFor(method, invite)

  function headingFor(m: string, inv: Invite | null): string {
    switch (m) {
      case 'CANCEL':
        return $t('invite.cancelled')
      case 'REPLY':
        return $t('invite.reply')
      case 'REQUEST':
        return inv && inv.existingEventId ? $t('invite.updated') : $t('invite.invitation')
      default:
        return $t('invite.event')
    }
  }

  async function answer(response: InviteResponse): Promise<void> {
    if (!invite || busy) {
      return
    }
    busy = response
    try {
      const r = await answerInvite({ messageId, calendarId, response, sendReply: canReply && sendReply })
      toastSuccess(r.outboxId ? $t('invite.answeredSent') : $t('invite.answered'))
      await load(messageId)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = null
    }
  }

  async function apply(): Promise<void> {
    if (!invite || busy) {
      return
    }
    busy = 'apply'
    try {
      await applyInvite(messageId)
      toastSuccess($t('invite.applied'))
      await load(messageId)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = null
    }
  }

  function showInCalendar(): void {
    if (start) {
      calendarFocus.set(start)
    }
    openSpace('calendar')
  }

  $: answers = [
    { key: 'accepted' as const, state: 'ACCEPTED', label: method === 'REQUEST' ? $t('invite.accept') : $t('invite.addToCalendar') },
    { key: 'tentative' as const, state: 'TENTATIVE', label: $t('invite.tentative') },
    { key: 'declined' as const, state: 'DECLINED', label: $t('invite.decline') },
  ].filter((a) => method === 'REQUEST' || a.key === 'accepted')
</script>

{#if invite && start}
  <section class="invite" class:cancelled={method === 'CANCEL'} aria-label={heading} in:fade={{ duration: dur(motion.base) }}>
    <div class="tile" aria-hidden="true">
      <span class="month">{start.toLocaleDateString(undefined, { month: 'short' })}</span>
      <span class="day">{start.getDate()}</span>
      <span class="wd">{start.toLocaleDateString(undefined, { weekday: 'short' })}</span>
    </div>
    <div class="body">
      <p class="kind">{heading}</p>
      <h3>{invite.summary || $t('calendar.untitled')}</h3>
      <p class="when">{when}</p>
      {#if repeat}
        <p class="meta"><Ph svg={repeatIcon} size={13} />{repeat}</p>
      {/if}
      {#if invite.location}
        <p class="meta"><Ph svg={mapPinIcon} size={13} />{invite.location}</p>
      {/if}
      {#if invite.organizer.email && method !== 'REPLY'}
        <p class="meta"><Ph svg={userIcon} size={13} />{$t('invite.from').replace('{name}', invite.organizer.name || invite.organizer.email)}</p>
      {/if}
      {#if replier}
        <p class="meta"><Ph svg={userIcon} size={13} />{replier.name || replier.email}: {$t(partStatKey(replier.partStat))}</p>
      {/if}
      {#if invite.outdated}
        <p class="note"><Ph svg={warningIcon} size={13} />{$t('invite.outdated')}</p>
      {/if}

      <div class="actions">
        {#if answerable && !invite.outdated}
          {#each answers as a (a.key)}
            <button
              type="button"
              class="cal-btn"
              class:on={invite.myPartStat === a.state && (method === 'REQUEST' || invite.existingEventId > 0)}
              aria-pressed={invite.myPartStat === a.state}
              disabled={busy !== null}
              on:click={() => answer(a.key)}
            >
              {#if busy === a.key}
                <PieProgress label={a.label} />
              {:else if invite.myPartStat === a.state && (method === 'REQUEST' || invite.existingEventId > 0)}
                <Ph svg={checkIcon} size={12} />
              {/if}
              {a.label}
            </button>
          {/each}
        {:else if (method === 'CANCEL' || method === 'REPLY') && invite.existingEventId > 0}
          <button type="button" class="cal-btn" disabled={busy !== null} on:click={apply}>
            {#if busy === 'apply'}<PieProgress label={$t('invite.apply')} />{/if}
            {method === 'CANCEL' ? $t('invite.applyCancel') : $t('invite.applyReply')}
          </button>
        {/if}
        {#if invite.existingEventId > 0}
          <button type="button" class="cal-btn link" on:click={showInCalendar}>{$t('invite.showInCalendar')}</button>
        {/if}
      </div>

      {#if answerable && !invite.outdated && (canReply || (!invite.existingEventId && writable.length > 1))}
        <div class="options">
          {#if canReply}
            <label class="check">
              <input type="checkbox" bind:checked={sendReply} />
              <span>{$t('invite.sendReply')}</span>
            </label>
          {/if}
          {#if !invite.existingEventId && writable.length > 1}
            <Select value={String(calendarId)} items={calendarItems} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
          {/if}
        </div>
      {/if}
    </div>
  </section>
{/if}

<style>
  .invite {
    display: flex;
    gap: var(--space-4);
    margin: 0 0 var(--space-4);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .tile {
    display: flex;
    flex-direction: column;
    align-items: center;
    flex-shrink: 0;
    width: calc(var(--cal-gutter) + var(--space-2));
    padding: var(--space-2) 0;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    line-height: 1.15;
  }

  .month {
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--accent);
  }

  .day {
    font-size: var(--fz-title);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .wd {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .cancelled .month {
    color: var(--danger);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }

  p {
    margin: 0;
  }

  .kind {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    overflow-wrap: anywhere;
  }

  .cancelled h3 {
    text-decoration: line-through;
  }

  .when {
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .meta,
  .note {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .note {
    color: var(--warning);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }

  .link {
    border-color: transparent;
    background: transparent;
    color: var(--link);
  }

  .options {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-4);
    margin-top: var(--space-2);
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .check input {
    margin: 0;
    accent-color: var(--accent);
  }
</style>
