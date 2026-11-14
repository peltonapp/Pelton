<script lang="ts">
  // the full event editor: title, when, how it repeats, which calendar, where,
  // reminders, who is invited and notes. it edits a draft and hands the
  // finished one back; the screen saves it and asks, for one occurrence of a
  // series, which occurrences the change is for.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import { slide } from 'svelte/transition'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/text-align-left-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import './controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Ph from '../common/Ph.svelte'
  import DateField from './DateField.svelte'
  import { addDays, atMinutes, dayKey, minutesOfDay, parseEventTime, startOfDay, toRFC3339 } from '../../lib/caldate'
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
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
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

  let titleInput: HTMLInputElement
  onMount(async () => {
    await tick()
    titleInput?.focus()
  })

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
          label += `  (${durationLabel(m - from)})`
        }
        return { value: String(m), label }
      })
  }

  function durationLabel(minutes: number): string {
    if (minutes < 60) {
      return $t('calendar.duration.minutes').replace('{n}', String(minutes))
    }
    const hours = minutes / 60
    return $t('calendar.duration.hours').replace('{n}', Number.isInteger(hours) ? String(hours) : hours.toFixed(1))
  }

  $: sameDayEnd = dayKey(startDay) === dayKey(endDay)
  $: startItems = timeItems(startMin, null)
  $: endItems = timeItems(endMin, sameDayEnd ? startMin : null)

  // moving the start keeps the length, as every calendar does.
  function setStartDay(day: Date): void {
    const span = endDay.getTime() - startDay.getTime()
    startDay = startOfDay(day)
    endDay = startOfDay(new Date(startDay.getTime() + span))
  }

  function setStartMin(m: number): void {
    const before = atMinutes(startDay, startMin)
    const length = atMinutes(endDay, endMin).getTime() - before.getTime()
    startMin = m
    const after = new Date(atMinutes(startDay, m).getTime() + length)
    endDay = startOfDay(after)
    endMin = minutesOfDay(after)
  }

  function setAllDay(next: boolean): void {
    allDay = next
    if (!next && endMin <= startMin && dayKey(startDay) === dayKey(endDay)) {
      endMin = Math.min(24 * 60 - 15, startMin + 60)
    }
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

  function reminderItems(current: number): { value: string; label: string }[] {
    const list = reminderChoices.includes(current) ? reminderChoices : [...reminderChoices, current].sort((a, b) => a - b)
    return list.map((m) => ({ value: String(m), label: reminderLabel(m, $t) }))
  }

  function addReminder(): void {
    const used = new Set(reminders.map((r) => r.minutesBefore))
    const next = reminderChoices.find((m) => m >= 15 && !used.has(m)) ?? 15
    reminders = [...reminders, { minutesBefore: next, relatedEnd: false, absolute: '', action: '' }]
  }

  // --- attendees ----------------------------------------------------------------

  let guest = ''

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
    if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault()
      addGuests()
    } else if (event.key === 'Backspace' && guest === '' && attendees.length > 0) {
      attendees = attendees.slice(0, -1)
    }
  }

  $: calendarItems = calendars.map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName') }))
  $: chosen = calendars.find((c) => c.id === calendarId)

  function submit(): void {
    if (guest.trim()) {
      addGuests()
    }
    if (!invalid && !saving) {
      dispatch('save', currentDraft(summary, location, description, calendarId, allDay, start, end, rrule, reminders, attendees))
    }
  }
</script>

<Modal title={draft.id === 0 ? $t('calendar.newEvent') : $t('calendar.editEvent')} size="medium" {dirty} on:close>
  <form id="event-editor" class="editor" style={`--cal:${calendarColor(chosen)}`} on:submit|preventDefault={submit}>
    <input
      class="title"
      bind:this={titleInput}
      bind:value={summary}
      placeholder={$t('calendar.addTitle')}
      aria-label={$t('calendar.title')}
    />

    <div class="line">
      <span class="icon"><Ph svg={clockIcon} size={16} /></span>
      <div class="when">
        <div class="span">
          <DateField value={startDay} {weekStart} label={$t('calendar.startDate')} on:change={(e) => setStartDay(e.detail)} />
          {#if !allDay}
            <Select value={String(startMin)} items={startItems} ariaLabel={$t('calendar.startTime')} on:change={(e) => setStartMin(Number(e.detail))} />
          {/if}
          <span class="to">{$t('calendar.to')}</span>
          <DateField value={endDay} {weekStart} label={$t('calendar.endDate')} on:change={(e) => (endDay = startOfDay(e.detail))} />
          {#if !allDay}
            <Select value={String(endMin)} items={endItems} ariaLabel={$t('calendar.endTime')} on:change={(e) => (endMin = Number(e.detail))} />
          {/if}
        </div>
        <label class="inline">
          <ToggleSwitch checked={allDay} label={$t('calendar.allDay')} on:change={(e) => setAllDay(e.detail)} />
          <span>{$t('calendar.allDay')}</span>
        </label>
        {#if invalid}
          <p class="error"><Ph svg={warningIcon} size={13} />{$t('calendar.endBeforeStart')}</p>
        {/if}
      </div>
    </div>

    <div class="line">
      <span class="icon"><Ph svg={repeatIcon} size={16} /></span>
      <div class="stack">
        <Select value={customOpen ? 'custom' : preset} items={presetItems} ariaLabel={$t('calendar.repeat')} on:change={(e) => pickPreset(e.detail)} />
        {#if occurrence}
          <p class="note">{$t('calendar.repeat.occurrenceNote')}</p>
        {/if}
        {#if customOpen}
          <div class="custom" transition:slide={{ duration: dur(motion.base) }}>
            {#if parseRRule(rrule, start) === null && rrule}
              <p class="note">{$t('calendar.repeat.keptAsIs')}</p>
            {/if}
            <div class="row">
              <span>{$t('calendar.repeat.every')}</span>
              <input
                class="cal-field num"
                type="number"
                min="1"
                max="99"
                value={custom.interval}
                aria-label={$t('calendar.repeat.interval')}
                on:change={(e) => updateCustom({ interval: Math.max(1, Number(e.currentTarget.value) || 1) })}
              />
              <Select value={custom.freq} items={unitItems} ariaLabel={$t('calendar.repeat.unit')} on:change={(e) => updateCustom({ freq: e.detail as Repeat['freq'] })} />
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
              <div class="row">
                <Select
                  value={custom.monthly}
                  items={[
                    { value: 'date', label: $t('calendar.repeat.monthlyOn').replace('{n}', String(start.getDate())) },
                    { value: 'weekday', label: describeRRule(buildRRule({ ...custom, monthly: 'weekday', interval: 1, end: 'never' }, start), start, $t, navigator.language) },
                  ]}
                  ariaLabel={$t('calendar.repeat.monthlyMode')}
                  on:change={(e) => updateCustom({ monthly: e.detail as Repeat['monthly'] })}
                />
              </div>
            {/if}
            <div class="row">
              <span>{$t('calendar.repeat.ends')}</span>
              <Select
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
                  class="cal-field num"
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

    <div class="line">
      <span class="icon"><Ph svg={calendarIcon} size={16} /></span>
      <div class="pick-cal">
        <span class="swatch" aria-hidden="true"></span>
        <Select value={String(calendarId)} items={calendarItems} ariaLabel={$t('calendar.calendar')} on:change={(e) => (calendarId = Number(e.detail))} />
      </div>
    </div>

    <div class="line">
      <span class="icon"><Ph svg={mapPinIcon} size={16} /></span>
      <input class="cal-field grow" bind:value={location} placeholder={$t('calendar.addLocation')} aria-label={$t('calendar.location')} />
    </div>

    <div class="line">
      <span class="icon"><Ph svg={bellIcon} size={16} /></span>
      <div class="stack">
        {#each reminders as r, i (i)}
          <div class="row">
            <Select
              value={String(r.minutesBefore)}
              items={reminderItems(r.minutesBefore)}
              ariaLabel={$t('calendar.reminder')}
              on:change={(e) => (reminders = reminders.map((x, j) => (j === i ? { ...x, minutesBefore: Number(e.detail), absolute: '' } : x)))}
            />
            <button type="button" class="cal-icon" aria-label={$t('calendar.removeReminder')} on:click={() => (reminders = reminders.filter((_, j) => j !== i))}>
              <Ph svg={closeIcon} size={13} />
            </button>
          </div>
        {/each}
        <button type="button" class="cal-item add" on:click={addReminder}>
          <Ph svg={plusIcon} size={13} />{$t('calendar.addReminder')}
        </button>
      </div>
    </div>

    <div class="line">
      <span class="icon"><Ph svg={usersIcon} size={16} /></span>
      <div class="stack">
        <div class="guests cal-field">
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

    <div class="line top">
      <span class="icon"><Ph svg={notesIcon} size={16} /></span>
      <textarea class="cal-field grow" rows="4" bind:value={description} placeholder={$t('calendar.addNotes')} aria-label={$t('calendar.notes')}></textarea>
    </div>
  </form>

  <svelte:fragment slot="footer">
    <div class="foot">
      {#if draft.id !== 0}
        <button type="button" class="cal-btn danger" on:click={() => dispatch('remove')}>{$t('calendar.delete')}</button>
      {/if}
      <span class="spacer"></span>
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('calendar.cancel')}</button>
      <button type="submit" form="event-editor" class="cal-btn primary" disabled={invalid || saving}>{$t('calendar.save')}</button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .title {
    width: 100%;
    padding: var(--space-2) 0 var(--space-3);
    border: none;
    border-bottom: var(--hairline) solid var(--border-default);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-title);
    font-weight: var(--fw-medium);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .title:focus {
    outline: none;
    border-bottom-color: var(--accent);
  }

  .title::placeholder {
    color: var(--text-tertiary);
  }

  .line {
    display: grid;
    grid-template-columns: var(--space-6) minmax(0, 1fr);
    align-items: center;
    gap: var(--space-3);
  }

  .line.top {
    align-items: start;
  }

  .line:has(.stack),
  .line:has(.when) {
    align-items: start;
  }

  .icon {
    display: inline-flex;
    justify-content: center;
    padding-top: var(--space-2);
    color: var(--text-tertiary);
  }

  .when,
  .stack {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-3);
    min-width: 0;
  }

  .span,
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .to {
    padding-inline: var(--space-1);
  }

  .inline {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .error {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fz-label);
    color: var(--danger);
  }

  .note,
  .summary {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .summary {
    color: var(--text-secondary);
  }

  .custom {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    align-self: stretch;
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }

  .num {
    width: calc(var(--control-height) * 2);
    font-variant-numeric: tabular-nums;
  }

  .days {
    display: flex;
    gap: var(--space-2);
  }

  .day {
    width: calc(var(--control-height) - 2px);
    height: calc(var(--control-height) - 2px);
    border: var(--hairline) solid var(--border-default);
    border-radius: 50%;
    background: var(--surface-raised);
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

  .pick-cal {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .swatch {
    width: var(--space-4);
    height: var(--space-4);
    border-radius: calc(var(--radius-control) - 1px);
    background: var(--cal);
  }

  .grow {
    width: 100%;
  }

  .add {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .guests {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    height: auto;
    min-height: var(--control-height);
    padding: var(--space-1) var(--space-2);
    cursor: text;
  }

  .guests:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  .guests input {
    flex: 1;
    min-width: calc(var(--cal-gutter) * 2);
    height: calc(var(--control-height) - 2 * var(--space-1) - 2px);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
  }

  .guests input:focus {
    outline: none;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--chip-height);
    padding-inline: var(--space-3) var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    color: var(--text-primary);
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

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
  }

  .spacer {
    flex: 1;
  }
</style>
