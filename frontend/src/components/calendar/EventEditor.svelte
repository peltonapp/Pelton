<script lang="ts">
  // the full event editor: title, when, how it repeats, which calendar, where,
  // reminders, who is invited and notes, as one column of hairline rows led by
  // an icon. it edits a draft and hands the finished one back; the screen
  // saves it and asks, for one occurrence of a series, which occurrences the
  // change is for. Cmd or Ctrl+Enter saves; escape asks before it throws edits
  // away.
  import { createEventDispatcher, tick } from 'svelte'
  import { slide } from 'svelte/transition'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/text-align-left-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import './controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import Ph from '../common/Ph.svelte'
  import DateField from './DateField.svelte'
  import { addDays, atMinutes, dayKey, daysBetween, minutesOfDay, parseEventTime, startOfDay, toRFC3339 } from '../../lib/caldate'
  import { calendarColor, formatTime } from '../../lib/calformat'
  import {
    buildRRule,
    defaultRepeat,
    defaultUntil,
    describeRRule,
    parseRRule,
    presetOf,
    presetRule,
    weekdayName,
    type Repeat,
    type RepeatPreset,
  } from '../../lib/recurrence'
  import { reminderChoices, reminderLabel, partStatKey } from './labels'
  import { localZone } from './occurrence'
  import { mailLinks, openMailLink } from '../../lib/maillink'
  import { dur, motion } from '../../lib/motion'
  import { isMac, t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { FoundDate } from '../../lib/eventphrase'
  import type { Attendee, Calendar, EventDraft, Reminder } from '../../lib/types'

  /** The event being edited; id 0 for a new one. */
  export let draft: EventDraft
  /** Calendars the event can go in. */
  export let calendars: Calendar[]
  export let weekStart: number = 1
  export let timeFormat: TimeFormat = 'auto'
  /** Who invites, when attendees are added to an event that names no organizer. */
  export let identity: { name: string; email: string } | null = null
  /** True while the save is running. */
  export let saving: boolean = false
  /** Dates found in a mail the event comes from, offered as chips to switch to. */
  export let found: FoundDate[] = []
  /** A quiet line above the form, such as why the time is a guess. */
  export let note: string = ''

  const dispatch = createEventDispatcher<{ close: void; save: EventDraft; remove: void }>()

  const initial = JSON.stringify(draft)
  const occurrence = draft.recurrenceId !== ''

  let summary = draft.summary
  let location = draft.location
  let description = draft.description
  let calendarId = draft.calendarId
  let allDay = draft.allDay
  let reminders: Reminder[] = draft.reminders.map((r) => ({ ...r }))
  let attendees: Attendee[] = draft.attendees.map((a) => ({ ...a }))
  let rrule = draft.rrule

  const firstStart = parseEventTime(draft.start, draft.allDay)
  const firstEnd = parseEventTime(draft.end, draft.allDay)
  let startDay = startOfDay(firstStart)
  let startMin = draft.allDay ? 9 * 60 : minutesOfDay(firstStart)
  let endDay = draft.allDay ? addDays(startOfDay(firstEnd), -1) : startOfDay(firstEnd)
  let endMin = draft.allDay ? 10 * 60 : minutesOfDay(firstEnd)
  if (endDay < startDay) {
    endDay = startDay
  }

  $: start = allDay ? startDay : atMinutes(startDay, startMin)
  $: end = allDay ? addDays(endDay, 1) : atMinutes(endDay, endMin)
  $: invalid = end <= start
  $: preset = presetOf(rrule, start)
  let custom: Repeat = parseRRule(draft.rrule, firstStart) ?? defaultRepeat(firstStart)
  let customOpen = presetOf(draft.rrule, firstStart) === 'custom'

  $: built = currentDraft(summary, location, description, calendarId, allDay, start, end, rrule, reminders, attendees)
  $: dirty = JSON.stringify(built) !== initial
  $: links = mailLinks(description)

  function currentDraft(
    s: string,
    loc: string,
    desc: string,
    cal: number,
    whole: boolean,
    from: Date,
    to: Date,
    rule: string,
    rems: Reminder[],
    people: Attendee[],
  ): EventDraft {
    const organizer = draft.organizer.email || people.length === 0 || !identity ? draft.organizer : { email: identity.email, name: identity.name, role: 'CHAIR', partStat: 'ACCEPTED', rsvp: false }
    return {
      ...draft,
      summary: s,
      location: loc,
      description: desc,
      calendarId: cal,
      allDay: whole,
      start: whole ? dayKey(from) : toRFC3339(from),
      end: whole ? dayKey(to) : toRFC3339(to),
      timeZone: whole ? '' : draft.allDay ? localZone() : draft.timeZone,
      rrule: rule,
      reminders: rems,
      attendees: people,
      organizer,
    }
  }

  // --- times --------------------------------------------------------------------

  const slots = Array.from({ length: 96 }, (_, i) => i * 15)

  function timeItems(selected: number, from: number | null): { value: string; label: string }[] {
    const list = slots.includes(selected) ? slots : [...slots, selected].sort((a, b) => a - b)
    return list
      .filter((m) => from === null || m > from)
      .map((m) => {
        let label = formatTime(atMinutes(startDay, m), timeFormat)
        if (from !== null) {
          label += `  ${durationLabel(m - from)}`
        }
        return { value: String(m), label }
      })
  }

  function durationLabel(minutes: number): string {
    if (minutes < 60) {
      return $t('calendar.duration.minutes').replace('{n}', String(minutes))
    }
    if (minutes % 1440 === 0) {
      const days = minutes / 1440
      return $t(days === 1 ? 'calendar.duration.day' : 'calendar.duration.days').replace('{n}', String(days))
    }
    const hours = minutes / 60
    return $t('calendar.duration.hours').replace('{n}', Number.isInteger(hours) ? String(hours) : hours.toFixed(1))
  }

  $: sameDayEnd = dayKey(startDay) === dayKey(endDay)
  $: startItems = timeItems(startMin, null)
  $: endItems = timeItems(endMin, sameDayEnd ? startMin : null)
  $: length = allDay ? (daysBetween(startDay, endDay) + 1) * 1440 : Math.round((end.getTime() - start.getTime()) / 60000)

  // moving the start keeps the length, as every calendar does.
  function setStartDay(day: Date): void {
    const span = daysBetween(startDay, endDay)
    startDay = startOfDay(day)
    endDay = addDays(startDay, span)
  }

  function setStartMin(m: number): void {
    const before = atMinutes(startDay, startMin)
    const span = atMinutes(endDay, endMin).getTime() - before.getTime()
    startMin = m
    const after = new Date(atMinutes(startDay, m).getTime() + span)
    endDay = startOfDay(after)
    endMin = minutesOfDay(after)
  }

  function setEndDay(day: Date): void {
    endDay = startOfDay(day) < startDay ? startDay : startOfDay(day)
  }

  function setAllDay(next: boolean): void {
    allDay = next
    if (!next && endMin <= startMin && sameDayEnd) {
      endMin = Math.min(24 * 60 - 15, startMin + 60)
    }
  }

  function useFound(f: FoundDate): void {
    allDay = f.allDay
    startDay = startOfDay(f.start)
    endDay = f.allDay ? addDays(startOfDay(f.end), -1) : startOfDay(f.end)
    if (!f.allDay) {
      startMin = minutesOfDay(f.start)
      endMin = minutesOfDay(f.end)
    }
    if (f.location && !location) {
      location = f.location
    }
  }

  function isFound(f: FoundDate, from: Date, to: Date, whole: boolean): boolean {
    return whole === f.allDay && from.getTime() === (f.allDay ? startOfDay(f.start).getTime() : f.start.getTime()) && (whole || to.getTime() === f.end.getTime())
  }

  function foundLabel(f: FoundDate): string {
    const day = f.start.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
    return f.allDay ? day : `${day}, ${formatTime(f.start, timeFormat)}`
  }

  // --- repeat -------------------------------------------------------------------

  $: presetItems = [
    { value: 'none', label: $t('calendar.repeat.none') },
    { value: 'daily', label: $t('calendar.repeat.daily') },
    { value: 'weekdays', label: $t('calendar.repeat.weekdays') },
    { value: 'weekly', label: $t('calendar.repeat.weeklyOn').replace('{day}', weekdayName(start.getDay(), navigator.language, 'long')) },
    { value: 'monthly', label: $t('calendar.repeat.monthlyOn').replace('{n}', String(start.getDate())) },
    { value: 'yearly', label: $t('calendar.repeat.yearlyOn').replace('{date}', start.toLocaleDateString(undefined, { day: 'numeric', month: 'long' })) },
    { value: 'custom', label: $t('calendar.repeat.customEllipsis') },
  ]

  function pickPreset(value: string): void {
    if (value === 'custom') {
      customOpen = true
      rrule = buildRRule(custom, start)
      return
    }
    customOpen = false
    rrule = presetRule(value as Exclude<RepeatPreset, 'custom'>, start)
    custom = parseRRule(rrule, start) ?? defaultRepeat(start)
  }

  function updateCustom(patch: Partial<Repeat>): void {
    custom = { ...custom, ...patch }
    rrule = buildRRule(custom, start)
  }

  function toggleDay(day: number): void {
    const has = custom.byDay.includes(day)
    const next = has ? custom.byDay.filter((d) => d !== day) : [...custom.byDay, day]
    updateCustom({ byDay: next.length ? next : [start.getDay()] })
  }

  $: unitItems = [
    { value: 'DAILY', label: $t('calendar.unit.days') },
    { value: 'WEEKLY', label: $t('calendar.unit.weeks') },
    { value: 'MONTHLY', label: $t('calendar.unit.months') },
    { value: 'YEARLY', label: $t('calendar.unit.years') },
  ]

  $: weekOrder = Array.from({ length: 7 }, (_, i) => (weekStart + i) % 7)

  // --- reminders ----------------------------------------------------------------

  let addingReminder = false
  let reminderButton: HTMLButtonElement

  $: unusedReminders = reminderChoices.filter((m) => !reminders.some((r) => r.minutesBefore === m && !r.relatedEnd && !r.absolute))

  function addReminder(minutes: number): void {
    reminders = [...reminders, { minutesBefore: minutes, relatedEnd: false, absolute: '', action: '' }].sort((a, b) => a.minutesBefore - b.minutesBefore)
    addingReminder = false
    reminderButton?.focus()
  }

  function reminderText(r: Reminder): string {
    if (r.absolute) {
      return new Date(r.absolute).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
    }
    return reminderLabel(r.minutesBefore, $t)
  }

  // --- attendees ----------------------------------------------------------------

  let guest = ''
  let guestInput: HTMLInputElement

  function addGuests(): void {
    const parts = guest.split(/[,;\n]/).map((s) => s.trim()).filter(Boolean)
    const known = new Set(attendees.map((a) => a.email.toLowerCase()))
    const added: Attendee[] = []
    for (const part of parts) {
      const match = part.match(/^(.*?)\s*<([^>]+)>$/)
      const email = (match ? match[2] : part).trim()
      const name = match ? match[1].replace(/^"|"$/g, '').trim() : ''
      if (!email.includes('@') || known.has(email.toLowerCase())) {
        continue
      }
      known.add(email.toLowerCase())
      added.push({ email, name, role: 'REQ-PARTICIPANT', partStat: 'NEEDS-ACTION', rsvp: true })
    }
    attendees = [...attendees, ...added]
    guest = ''
  }

  function onGuestKey(event: KeyboardEvent): void {
    if (event.key === 'Enter' || event.key === ',' || event.key === ';') {
      if (guest.trim()) {
        event.preventDefault()
        addGuests()
      }
    } else if (event.key === 'Backspace' && guest === '' && attendees.length > 0) {
      attendees = attendees.slice(0, -1)
    }
  }

  // --- notes --------------------------------------------------------------------

  // the notes grow with what is in them rather than scrolling inside a box;
  // the text is the parameter so every change refits.
  function grow(el: HTMLTextAreaElement, _text: string): { update(): void } {
    const fit = (): void => {
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
    }
    void tick().then(fit)
    return { update: fit }
  }

  // --- calendar and saving --------------------------------------------------------

  $: calendarItems = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName'), color: calendarColor(c) }))
  $: chosen = calendars.find((c) => c.id === calendarId)

  function submit(): void {
    if (guest.trim()) {
      addGuests()
    }
    if (!invalid && !saving) {
      dispatch('save', currentDraft(summary, location, description, calendarId, allDay, start, end, rrule, reminders, attendees))
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Enter' && (isMac ? event.metaKey : event.ctrlKey)) {
      event.preventDefault()
      submit()
    }
  }

  $: saveHint = `${isMac ? '⌘' : 'Ctrl+'}Enter`
</script>

<Modal title={draft.id === 0 ? $t('calendar.newEvent') : $t('calendar.editEvent')} size="medium" {dirty} on:close>
  <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
  <form id="event-editor" class="editor" style={`--cal:${calendarColor(chosen)}`} on:submit|preventDefault={submit} on:keydown={onKeydown}>
    <!-- svelte-ignore a11y-autofocus -->
    <input class="title" bind:value={summary} placeholder={$t('calendar.addTitle')} aria-label={$t('calendar.title')} autofocus />

    {#if note}
      <p class="note lead">{note}</p>
    {/if}
    {#if found.length > 0}
      <div class="found" role="group" aria-label={$t('calendar.foundDates')}>
        <span class="found-label">{$t('calendar.foundDates')}</span>
        {#each found as f, i (i)}
          <button type="button" class="pill" class:on={isFound(f, start, end, allDay)} aria-pressed={isFound(f, start, end, allDay)} on:click={() => useFound(f)}>
            {foundLabel(f)}
          </button>
        {/each}
      </div>
    {/if}

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={clockIcon} size={16} /></span>
      <div class="cell">
        <div class="when">
          <DateField class="ctl" value={startDay} {weekStart} label={$t('calendar.startDate')} on:change={(e) => setStartDay(e.detail)} />
          {#if !allDay}
            <Select class="ctl time" value={String(startMin)} items={startItems} ariaLabel={$t('calendar.startTime')} on:change={(e) => setStartMin(Number(e.detail))} />
          {/if}
          <span class="dash" aria-hidden="true">-</span>
          {#if allDay || !sameDayEnd}
            <DateField class="ctl" value={endDay} {weekStart} label={$t('calendar.endDate')} on:change={(e) => setEndDay(e.detail)} />
          {/if}
          {#if !allDay}
            <Select class="ctl time" value={String(endMin)} items={endItems} ariaLabel={$t('calendar.endTime')} on:change={(e) => (endMin = Number(e.detail))} />
          {/if}
          {#if !invalid}
            <span class="length">{durationLabel(length)}</span>
          {/if}
          <label class="allday">
            <ToggleSwitch checked={allDay} label={$t('calendar.allDay')} on:change={(e) => setAllDay(e.detail)} />
            <span>{$t('calendar.allDay')}</span>
          </label>
        </div>
        {#if !allDay && sameDayEnd}
          <button type="button" class="link" on:click={() => setEndDay(addDays(startDay, 1))}>{$t('calendar.endsLater')}</button>
        {/if}
        {#if invalid}
          <p class="error"><Ph svg={warningIcon} size={13} />{$t('calendar.endBeforeStart')}</p>
        {/if}
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={repeatIcon} size={16} /></span>
      <div class="cell">
        <Select class="ctl" value={customOpen ? 'custom' : preset} items={presetItems} ariaLabel={$t('calendar.repeat')} on:change={(e) => pickPreset(e.detail)} />
        {#if occurrence}
          <p class="note">{$t('calendar.repeat.occurrenceNote')}</p>
        {/if}
        {#if customOpen}
          <div class="custom" transition:slide={{ duration: dur(motion.base) }}>
            {#if parseRRule(rrule, start) === null && rrule}
              <p class="note">{$t('calendar.repeat.keptAsIs')}</p>
            {/if}
            <div class="line">
              <span>{$t('calendar.repeat.every')}</span>
              <input
                class="num"
                type="number"
                min="1"
                max="99"
                value={custom.interval}
                aria-label={$t('calendar.repeat.interval')}
                on:change={(e) => updateCustom({ interval: Math.max(1, Number(e.currentTarget.value) || 1) })}
              />
              <Select class="ctl" value={custom.freq} items={unitItems} ariaLabel={$t('calendar.repeat.unit')} on:change={(e) => updateCustom({ freq: e.detail as Repeat['freq'] })} />
            </div>
            {#if custom.freq === 'WEEKLY'}
              <div class="days" role="group" aria-label={$t('calendar.repeat.onDays')}>
                {#each weekOrder as d (d)}
                  <button
                    type="button"
                    class="day"
                    class:on={custom.byDay.includes(d)}
                    aria-pressed={custom.byDay.includes(d)}
                    aria-label={weekdayName(d, navigator.language, 'long')}
                    on:click={() => toggleDay(d)}
                  >
                    {weekdayName(d, navigator.language, 'narrow')}
                  </button>
                {/each}
              </div>
            {/if}
            {#if custom.freq === 'MONTHLY'}
              <Select
                class="ctl"
                value={custom.monthly}
                items={[
                  { value: 'date', label: $t('calendar.repeat.monthlyOn').replace('{n}', String(start.getDate())) },
                  { value: 'weekday', label: describeRRule(buildRRule({ ...custom, monthly: 'weekday', interval: 1, end: 'never' }, start), start, $t, navigator.language) },
                ]}
                ariaLabel={$t('calendar.repeat.monthlyMode')}
                on:change={(e) => updateCustom({ monthly: e.detail as Repeat['monthly'] })}
              />
            {/if}
            <div class="line">
              <span>{$t('calendar.repeat.ends')}</span>
              <Select
                class="ctl"
                value={custom.end}
                items={[
                  { value: 'never', label: $t('calendar.repeat.never') },
                  { value: 'count', label: $t('calendar.repeat.afterCount') },
                  { value: 'until', label: $t('calendar.repeat.onDate') },
                ]}
                ariaLabel={$t('calendar.repeat.ends')}
                on:change={(e) => updateCustom({ end: e.detail as Repeat['end'], until: custom.until || defaultUntil(start) })}
              />
              {#if custom.end === 'count'}
                <input
                  class="num"
                  type="number"
                  min="1"
                  max="999"
                  value={custom.count}
                  aria-label={$t('calendar.repeat.count')}
                  on:change={(e) => updateCustom({ count: Math.max(1, Number(e.currentTarget.value) || 1) })}
                />
                <span>{$t('calendar.repeat.occurrences')}</span>
              {:else if custom.end === 'until'}
                <DateField
                  class="ctl"
                  value={new Date(`${custom.until || defaultUntil(start)}T00:00:00`)}
                  {weekStart}
                  label={$t('calendar.repeat.untilDate')}
                  on:change={(e) => updateCustom({ until: dayKey(e.detail) })}
                />
              {/if}
            </div>
            {#if rrule}
              <p class="summary">{describeRRule(rrule, start, $t, navigator.language)}</p>
            {/if}
          </div>
        {/if}
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={calendarIcon} size={16} /></span>
      <div class="cell">
        <Select class="ctl" value={String(calendarId)} items={calendarItems} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={mapPinIcon} size={16} /></span>
      <input class="field" bind:value={location} placeholder={$t('calendar.addLocation')} aria-label={$t('calendar.location')} />
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={bellIcon} size={16} /></span>
      <div class="chips" role="group" aria-label={$t('calendar.reminder')}>
        {#each reminders as r, i (i)}
          <span class="chip">
            {reminderText(r)}
            <button type="button" aria-label={`${$t('calendar.removeReminder')}: ${reminderText(r)}`} on:click={() => (reminders = reminders.filter((_, j) => j !== i))}>
              <Ph svg={closeIcon} size={10} />
            </button>
          </span>
        {/each}
        <button
          type="button"
          class="add"
          bind:this={reminderButton}
          aria-haspopup="dialog"
          aria-expanded={addingReminder}
          on:click={() => (addingReminder = !addingReminder)}
        >
          <Ph svg={plusIcon} size={12} />{reminders.length ? $t('calendar.add') : $t('calendar.addReminder')}
        </button>
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={usersIcon} size={16} /></span>
      <div class="cell">
        <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
        <div class="chips" on:click={() => guestInput?.focus()}>
          {#each attendees as a, i (a.email)}
            <span class="chip" title={`${a.email}, ${$t(partStatKey(a.partStat))}`}>
              <span class="mark {a.partStat.toLowerCase()}" aria-hidden="true"></span>
              {a.name || a.email}
              <button type="button" aria-label={$t('calendar.removeGuest').replace('{name}', a.name || a.email)} on:click={() => (attendees = attendees.filter((_, j) => j !== i))}>
                <Ph svg={closeIcon} size={10} />
              </button>
            </span>
          {/each}
          <input
            class="field inline"
            bind:this={guestInput}
            bind:value={guest}
            placeholder={attendees.length ? '' : $t('calendar.addGuests')}
            aria-label={$t('calendar.guestsLabel')}
            on:keydown={onGuestKey}
            on:blur={() => guest.trim() && addGuests()}
          />
        </div>
        {#if attendees.length > 0}
          <p class="note">{$t('calendar.guestsNote')}</p>
        {/if}
      </div>
    </div>

    <div class="row top">
      <span class="icon" aria-hidden="true"><Ph svg={notesIcon} size={16} /></span>
      <div class="cell">
        <textarea class="field notes" rows="2" bind:value={description} use:grow={description} placeholder={$t('calendar.addNotes')} aria-label={$t('calendar.notes')}></textarea>
        {#if links.length > 0}
          <div class="mails">
            {#each links as link (link)}
              <button type="button" class="pill" on:click={() => openMailLink(link)}>
                <Ph svg={envelopeIcon} size={13} />{$t('calendar.openMail')}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </form>

  <svelte:fragment slot="footer">
    <div class="foot">
      {#if draft.id !== 0}
        <button type="button" class="cal-btn danger" on:click={() => dispatch('remove')}>{$t('calendar.delete')}</button>
      {/if}
      <span class="spacer"></span>
      <kbd class="hint" aria-hidden="true">{saveHint}</kbd>
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('calendar.cancel')}</button>
      <button type="submit" form="event-editor" class="cal-btn primary" disabled={invalid || saving}>{$t('calendar.save')}</button>
    </div>
  </svelte:fragment>
</Modal>

{#if addingReminder && reminderButton}
  <AnchoredPopover anchor={reminderButton} align="start" label={$t('calendar.addReminder')} on:close={() => (addingReminder = false)}>
    <div class="cal-menu">
      {#each unusedReminders as m (m)}
        <button type="button" class="cal-item" on:click={() => addReminder(m)}>{reminderLabel(m, $t)}</button>
      {/each}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .editor {
    display: flex;
    flex-direction: column;
  }

  .title {
    width: 100%;
    padding: var(--space-2) 0 var(--space-3);
    border: none;
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-title);
    font-weight: var(--fw-medium);
  }

  .title:focus {
    outline: none;
  }

  .title::placeholder,
  .field::placeholder {
    color: var(--text-tertiary);
    opacity: 1;
  }

  .found {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .found-label {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  /* one row per thing: the icon column, then the value. rows are divided by
     hairlines instead of each control carrying its own box. */
  .row {
    display: grid;
    grid-template-columns: var(--space-6) minmax(0, 1fr);
    align-items: center;
    column-gap: var(--space-3);
    min-height: calc(var(--control-height) + var(--space-3));
    padding: var(--space-2) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .row:last-child {
    border-bottom: none;
  }

  .row.top,
  .row:has(.custom),
  .row:has(.note),
  .row:has(.error),
  .row:has(.link) {
    align-items: start;
  }

  .icon {
    display: inline-flex;
    justify-content: center;
    align-items: center;
    height: var(--control-height);
    color: var(--text-tertiary);
  }

  .cell {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    min-width: 0;
  }

  .when {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
    width: 100%;
  }

  /* every dropdown in the form: flat until pointed at, the same height and
     the same chevron whether it picks a day, a time or a calendar. */
  .editor :global(.ctl) {
    height: var(--control-height);
    padding: 0 var(--space-2);
    border-color: transparent;
    background: transparent;
    font-size: var(--fz-list);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .editor :global(.ctl:not(:disabled):hover) {
    border-color: var(--border-default);
    background: var(--surface-raised);
  }

  .editor :global(.ctl.open) {
    border-color: var(--accent);
  }

  .editor :global(.ctl .chevron) {
    color: var(--text-tertiary);
  }

  .editor :global(.ctl.time) {
    font-variant-numeric: tabular-nums;
  }

  .dash {
    color: var(--text-tertiary);
  }

  .length {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .allday {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-inline-start: auto;
    font-size: var(--fz-label);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .link {
    padding: 0 var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .link:hover {
    color: var(--text-primary);
  }

  .field {
    width: 100%;
    min-width: 0;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
  }

  .field:focus {
    outline: none;
  }

  .row:focus-within > .field,
  .chips:focus-within {
    box-shadow: inset 0 calc(-1 * var(--hairline)) 0 var(--accent);
  }

  .notes {
    height: auto;
    min-height: calc(var(--control-height) * 2);
    padding: var(--space-2);
    line-height: 1.5;
    resize: none;
    overflow: hidden;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .row:focus-within .notes {
    box-shadow: inset 0 calc(-1 * var(--hairline)) 0 var(--accent);
  }

  .error {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    padding-inline: var(--space-2);
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .note,
  .summary {
    margin: 0;
    padding-inline: var(--space-2);
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }

  .lead {
    padding: var(--space-3) 0 0;
  }

  .summary {
    color: var(--text-secondary);
  }

  .custom {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-1) 0 var(--space-1) var(--space-2);
    border-inline-start: var(--hairline) solid var(--border-default);
    margin-inline-start: var(--space-2);
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .num {
    width: calc(var(--control-height) * 1.6);
    height: calc(var(--control-height) - 4px);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-variant-numeric: tabular-nums;
  }

  .num:focus {
    outline: none;
    border-color: var(--accent);
  }

  .days {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .day {
    width: calc(var(--control-height) - 4px);
    height: calc(var(--control-height) - 4px);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .day.on {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
    width: 100%;
    min-height: var(--control-height);
    padding-inline: var(--space-2);
    cursor: text;
  }

  .chips .field.inline {
    flex: 1;
    width: auto;
    min-width: calc(var(--cal-gutter) * 2);
    height: var(--chip-height);
    padding: 0;
  }

  .chip,
  .pill {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--chip-height);
    padding-inline: var(--space-3) var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    color: var(--text-primary);
    white-space: nowrap;
  }

  .pill {
    padding-inline: var(--space-3);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .pill:hover {
    background: var(--surface-hover);
  }

  .pill.on {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .chip button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--chip-height) - var(--space-2));
    height: calc(var(--chip-height) - var(--space-2));
    border: none;
    border-radius: calc(var(--radius-control) - 1px);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .chip button:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .add {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--chip-height);
    padding-inline: var(--space-2);
    border: var(--hairline) dashed var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .add:hover {
    color: var(--text-primary);
    border-color: var(--border-strong);
  }

  .mark {
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

  .mails {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding-inline: var(--space-2);
  }

  .day:focus-visible,
  .chip button:focus-visible,
  .pill:focus-visible,
  .add:focus-visible,
  .link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }

  .spacer {
    flex: 1;
  }

  .hint {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  @media (max-width: 520px) {
    .allday {
      margin-inline-start: 0;
    }

    .hint {
      display: none;
    }
  }
</style>
