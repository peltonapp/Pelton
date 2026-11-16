<script lang="ts">
  // the details of one occurrence, hanging off the event that was clicked: its
  // title and time, how it repeats, where, the calendar, who is invited and
  // how they answered, and the notes. editing opens the full editor; deleting
  // a repeating event asks which occurrences go.
  import { createEventDispatcher, onMount } from 'svelte'
  import { fade } from 'svelte/transition'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/text-align-left-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import pencilIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import taskIcon from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import prepareIcon from '@phosphor-icons/core/assets/duotone/clipboard-text-duotone.svg?raw'
  import { mailLinks, openMailLink } from '../../lib/maillink'
  import { openMeetingPrep } from '../../stores/meetingprep'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import { getEvent, projectsOfEvent, setEventProject } from '../../lib/api'
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import ProjectMenu from '../projects/ProjectMenu.svelte'
  import NoteMentions from '../notes/NoteMentions.svelte'
  import { placeOccurrence } from '../../lib/caldate'
  import { formatWhen } from '../../lib/calformat'
  import { describeRRule } from '../../lib/recurrence'
  import { reminderLabel, partStatKey } from './labels'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { CalendarEvent, EventScope, Occurrence } from '../../lib/types'

  /** The occurrence shown. */
  export let occ: Occurrence
  /** The element it hangs off. */
  export let anchor: HTMLElement
  /** Its calendar's name and color. */
  export let calendarName: string = ''
  export let color: string = 'var(--cal-1)'
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{
    close: void
    edit: CalendarEvent
    remove: { occ: Occurrence; scope: EventScope | '' }
    toTask: Occurrence
  }>()

  let event: CalendarEvent | null = null
  let error = ''
  let confirming = false
  let popover: AnchoredPopover

  $: p = placeOccurrence(occ)
  $: when = formatWhen(p.start, p.end, occ.allDay, timeFormat)
  $: repeat = event?.rrule ? describeRRule(event.rrule, p.start, $t, navigator.language) : ''
  $: attendees = event?.attendees ?? []
  $: accepted = attendees.filter((a) => a.partStat === 'ACCEPTED').length

  onMount(async () => {
    try {
      event = await getEvent(occ.eventId, occ.recurrenceId)
      projectId = (await projectsOfEvent(occ.uid))[0] ?? 0
    } catch (err) {
      error = errorMessage(err)
    }
    popover?.reposition()
  })

  // the project the event is filed under, 0 for none. filing is kept on this
  // device and never written into the event.
  let projectId = 0

  async function fileUnder(id: number): Promise<void> {
    const before = projectId
    projectId = id
    try {
      await setEventProject(occ.uid, occ.summary, id)
    } catch (err) {
      projectId = before
      toastError(errorMessage(err))
    }
  }

  function prepare(): void {
    openMeetingPrep(occ.eventId, occ.recurrenceId)
    dispatch('close')
  }

  function askDelete(): void {
    if (occ.recurring) {
      confirming = true
      popover?.reposition()
    } else {
      dispatch('remove', { occ, scope: '' })
    }
  }
</script>

<AnchoredPopover bind:this={popover} {anchor} align="start" label={occ.summary || $t('calendar.untitled')} on:close>
  <div class="pop" style={`--ev:${color}`}>
    <header>
      <span class="swatch" aria-hidden="true"></span>
      <h3 class:cancelled={occ.status === 'CANCELLED'}>{occ.summary || $t('calendar.untitled')}</h3>
      <div class="tools">
        {#if attendees.length > 0 && !occ.task}
          <button type="button" class="cal-icon" aria-label={$t('prep.button')} title={$t('prep.button')} on:click={prepare}>
            <Ph svg={prepareIcon} size={15} />
          </button>
        {/if}
        {#if !occ.readOnly}
          <button type="button" class="cal-icon" aria-label={$t('calendar.edit')} disabled={!event} on:click={() => event && dispatch('edit', event)}>
            <Ph svg={pencilIcon} size={15} />
          </button>
          {#if !occ.recurring}
            <button type="button" class="cal-icon" aria-label={$t('tasks.toTask')} title={$t('tasks.toTask')} on:click={() => dispatch('toTask', occ)}>
              <Ph svg={taskIcon} size={15} />
            </button>
          {/if}
          <button type="button" class="cal-icon" aria-label={$t('calendar.delete')} on:click={askDelete}>
            <Ph svg={trashIcon} size={15} />
          </button>
        {/if}
        <button type="button" class="cal-icon" aria-label={$t('calendar.close')} on:click={() => dispatch('close')}>
          <Ph svg={closeIcon} size={14} />
        </button>
      </div>
    </header>

    {#if confirming}
      <div class="confirm" in:fade={{ duration: dur(motion.fast) }}>
        <p>{$t('calendar.deleteRecurring')}</p>
        <div class="choices">
          <button type="button" class="cal-btn" on:click={() => dispatch('remove', { occ, scope: 'this' })}>{$t('calendar.scope.this')}</button>
          <button type="button" class="cal-btn" on:click={() => dispatch('remove', { occ, scope: 'future' })}>{$t('calendar.scope.future')}</button>
          <button type="button" class="cal-btn danger" on:click={() => dispatch('remove', { occ, scope: 'all' })}>{$t('calendar.scope.all')}</button>
        </div>
      </div>
    {/if}

    <dl>
      <div class="line">
        <dt><Ph svg={clockIcon} size={15} /></dt>
        <dd>{when}</dd>
      </div>
      {#if repeat}
        <div class="line">
          <dt><Ph svg={repeatIcon} size={15} /></dt>
          <dd>{repeat}</dd>
        </div>
      {/if}
      {#if occ.location}
        <div class="line">
          <dt><Ph svg={mapPinIcon} size={15} /></dt>
          <dd class="wrap">{occ.location}</dd>
        </div>
      {/if}
      <div class="line">
        <dt><Ph svg={calendarIcon} size={15} /></dt>
        <dd>
          {calendarName}
          {#if occ.readOnly}<span class="tag">{$t('calendar.readOnly')}</span>{/if}
          {#if occ.pending}<span class="tag">{$t('calendar.pending')}</span>{/if}
        </dd>
      </div>
      {#if event && event.reminders.length > 0}
        <div class="line">
          <dt><Ph svg={bellIcon} size={15} /></dt>
          <dd>{event.reminders.map((r) => reminderLabel(r.minutesBefore, $t)).join(', ')}</dd>
        </div>
      {/if}
      {#if attendees.length > 0}
        <div class="line top">
          <dt><Ph svg={usersIcon} size={15} /></dt>
          <dd>
            <span class="muted">
              {$t('calendar.guests').replace('{n}', String(attendees.length)).replace('{yes}', String(accepted))}
            </span>
            <ul class="people">
              {#if event?.organizer.email}
                <li>
                  <span class="who">{event.organizer.name || event.organizer.email}</span>
                  <span class="state">{$t('calendar.organizer')}</span>
                </li>
              {/if}
              {#each attendees as a (a.email)}
                <li>
                  <span class="mark {a.partStat.toLowerCase()}" aria-hidden="true"></span>
                  <span class="who" title={a.email}>{a.name || a.email}</span>
                  <span class="state">{$t(partStatKey(a.partStat))}</span>
                </li>
              {/each}
            </ul>
          </dd>
        </div>
      {/if}
      {#if event?.description}
        <div class="line top">
          <dt><Ph svg={notesIcon} size={15} /></dt>
          <dd class="notes">{event.description}</dd>
        </div>
        {#each mailLinks(event.description) as link (link)}
          <div class="line">
            <dt><Ph svg={envelopeIcon} size={15} /></dt>
            <dd><button type="button" class="cal-btn" on:click={() => openMailLink(link)}>{$t('calendar.openMail')}</button></dd>
          </div>
        {/each}
      {/if}
      {#if event}
        <div class="line">
          <dt><Ph svg={kanbanIcon} size={15} /></dt>
          <dd><ProjectMenu value={projectId} buttonClass="cal-btn project" on:change={(e) => void fileUnder(e.detail)} /></dd>
        </div>
      {/if}
      {#if occ.uid}
        <div class="line">
          <dt></dt>
          <dd>
            <NoteMentions
              about={{
                kind: 'event',
                id: occ.uid,
                title: occ.summary || $t('calendar.untitled'),
                event: {
                  title: occ.summary || $t('calendar.untitled'),
                  start: when,
                  end: '',
                  location: occ.location,
                  attendees: attendees.map((a) => (a.name ? `${a.name} <${a.email}>` : a.email)),
                },
              }}
            />
          </dd>
        </div>
      {/if}
      {#if occ.conflict || event?.syncError}
        <div class="line warn">
          <dt><Ph svg={warningIcon} size={15} /></dt>
          <dd>{event?.syncError || $t('calendar.conflictShort')}</dd>
        </div>
      {/if}
      {#if error}
        <div class="line warn">
          <dt><Ph svg={warningIcon} size={15} /></dt>
          <dd>{error}</dd>
        </div>
      {/if}
    </dl>
    {#if !event && !error}
      <div class="loading"><PieProgress label={$t('calendar.loading')} /></div>
    {/if}
  </div>
</AnchoredPopover>

<style>
  .pop {
    display: flex;
    flex-direction: column;
    width: var(--cal-popover-width);
    overflow-y: auto;
  }

  header {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding-block: var(--space-4) var(--space-3);
    padding-inline: var(--space-4) var(--space-3);
  }

  .swatch {
    flex-shrink: 0;
    width: var(--space-4);
    height: var(--space-4);
    margin-top: var(--space-1);
    border-radius: calc(var(--radius-control) - 1px);
    background: var(--ev);
  }

  h3 {
    flex: 1;
    min-width: 0;
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    line-height: 1.3;
    overflow-wrap: anywhere;
  }

  h3.cancelled {
    text-decoration: line-through;
  }

  .tools {
    display: flex;
    gap: var(--space-1);
    margin-top: calc(-1 * var(--space-1));
  }

  .confirm {
    margin: 0 var(--space-4) var(--space-3);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .confirm p {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .choices {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  dl {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin: 0;
    padding: 0 var(--space-4) var(--space-4);
  }

  .line {
    display: grid;
    grid-template-columns: var(--space-5) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-3);
  }

  .line.top {
    align-items: start;
  }

  dt {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  dd {
    margin: 0;
    min-width: 0;
    font-size: var(--fz-list);
    color: var(--text-primary);
    line-height: 1.4;
  }

  .wrap {
    overflow-wrap: anywhere;
  }

  .notes {
    max-height: calc(var(--fz-list) * 1.4 * 8);
    overflow-y: auto;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--text-secondary);
    user-select: text;
  }

  .warn dt,
  .warn dd {
    color: var(--warning);
  }

  .tag {
    margin-inline-start: var(--space-2);
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .muted {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .people {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: var(--space-2) 0 0;
    padding: 0;
    list-style: none;
  }

  .people li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
  }

  .who {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .mark {
    flex-shrink: 0;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    border: 1.5px solid var(--text-tertiary);
  }

  .mark.accepted {
    border-color: var(--success);
    background: var(--success);
  }

  .mark.tentative {
    border-color: var(--warning);
  }

  .mark.declined {
    border-color: var(--danger);
    background: var(--danger);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: 0 0 var(--space-4);
    color: var(--text-tertiary);
  }
</style>
