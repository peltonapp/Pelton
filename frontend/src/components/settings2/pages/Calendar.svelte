<script lang="ts">
  // the Calendar page: the view the calendar opens in, the week's first day,
  // working hours, how time zones show, what a new event starts with, how
  // often calendars sync, and the CalDAV accounts they sync through.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import CalendarSkeleton from '../skeletons/CalendarSkeleton.svelte'
  import ZoneSkeleton from '../skeletons/ZoneSkeleton.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import CalDavAccounts from '../../calendar/CalDavAccounts.svelte'
  import { calendarSettings, calendars, loadCalendars, loadCalendarSettings, saveCalendarSettings } from '../../../stores/calendar'
  import { loadTaskSettings, saveTaskSettings, taskSettings } from '../../../stores/tasks'
  import { atMinutes, localeWeekStart, resolveWeekStart } from '../../../lib/caldate'
  import { blockSettings, formatMinutes, loadBlockSettings, saveBlockSettings } from '../../calendar/timeblocks'
  import { formatTime } from '../../../lib/calformat'
  import { weekdayName } from '../../../lib/recurrence'
  import { reminderChoices, reminderLabel } from '../../calendar/labels'
  import { prefs } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import '../../../theme/controls.css'
  import { defaultCalendarFileStatus, setDefaultCalendarFileHandler } from '../../../lib/api'
  import { errorMessage, toastError } from '../../../stores/toast'
  import type { TimeFormat } from '../../../lib/format'
  import type { CalendarView, TimeZoneDisplay } from '../../../lib/types'

  let icsHandler = { known: false, isDefault: false }

  onMount(() => {
    void loadBlockSettings()
    void loadTaskSettings()
    void loadCalendarSettings()
    void loadCalendars()
    void readICSHandler()
  })

  async function readICSHandler(): Promise<void> {
    try {
      icsHandler = await defaultCalendarFileStatus()
    } catch {
      icsHandler = { known: false, isDefault: false }
    }
  }

  // the system asks the user to confirm where it can, so the status is read
  // again afterwards rather than assumed.
  async function useForICS(): Promise<void> {
    try {
      await setDefaultCalendarFileHandler()
    } catch (err) {
      toastError(errorMessage(err))
    }
    await readICSHandler()
  }

  $: s = $calendarSettings

  $: viewOptions = [
    { key: 'day', label: $t('calendar.view.day') },
    { key: 'week', label: $t('calendar.view.week') },
    { key: 'month', label: $t('calendar.view.month') },
    { key: 'agenda', label: $t('calendar.view.agenda') },
    { key: 'year', label: $t('calendar.view.year') },
  ]

  $: weekStartOptions = [
    { key: '-1', label: $t('calendarSettings.weekStart.auto').replace('{day}', weekdayName(localeWeekStart(navigator.language), navigator.language, 'short')) },
    { key: '0', label: weekdayName(0, navigator.language, 'long') },
    { key: '1', label: weekdayName(1, navigator.language, 'long') },
    { key: '6', label: weekdayName(6, navigator.language, 'long') },
  ]

  $: zoneOptions = [
    { key: 'off', label: $t('calendarSettings.zone.off'), hint: $t('calendarSettings.zone.off.hint') },
    { key: 'label', label: $t('calendarSettings.zone.label'), hint: $t('calendarSettings.zone.label.hint') },
    { key: 'dual', label: $t('calendarSettings.zone.dual'), hint: $t('calendarSettings.zone.dual.hint') },
  ]

  const halfHours = Array.from({ length: 49 }, (_, i) => i * 30)
  $: hourItems = (from: number, to: number) =>
    halfHours
      .filter((m) => m >= from && m <= to)
      .map((m) => ({ value: String(m), label: m === 24 * 60 ? $t('calendarSettings.midnight') : formatTime(atMinutes(new Date(2026, 0, 5), m), $prefs.timeFormat as TimeFormat) }))

  $: zones = listZones()

  function listZones(): { value: string; label: string }[] {
    const intl = Intl as unknown as { supportedValuesOf?: (key: string) => string[] }
    const list = intl.supportedValuesOf?.('timeZone') ?? ['UTC', 'Europe/London', 'Europe/Berlin', 'America/New_York', 'America/Los_Angeles', 'Asia/Tokyo']
    return [{ value: '', label: $t('calendarSettings.zone.pick') }, ...list.map((z) => ({ value: z, label: z.replaceAll('_', ' ') }))]
  }

  $: reminderItems = [
    { value: '-1', label: $t('calendarSettings.reminder.none') },
    ...reminderChoices.map((m) => ({ value: String(m), label: reminderLabel(m, $t) })),
  ]

  $: calendarItems = [
    { value: '0', label: $t('calendarSettings.defaultCalendar.first') },
    ...$calendars.filter((c) => !c.readOnly).map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName') })),
  ]

  $: b = $blockSettings
  $: weekOrder = Array.from({ length: 7 }, (_, i) => (resolveWeekStart(s.weekStart, navigator.language) + i) % 7)

  function minuteItems(choices: number[], current: number): { value: string; label: string }[] {
    return [...new Set([...choices, current])].sort((x, y) => x - y).map((m) => ({ value: String(m), label: formatMinutes(m, $t) }))
  }

  function toggleWorkDay(day: number): void {
    const days = b.workDays.includes(day) ? b.workDays.filter((d) => d !== day) : [...b.workDays, day]
    void saveBlockSettings({ workDays: days.sort((x, y) => x - y) })
  }

  $: syncItems = [
    { value: '0', label: $t('calendarSettings.sync.manual') },
    ...[5, 15, 30, 60].map((m) => ({ value: String(m), label: $t('calendarSettings.sync.every').replace('{n}', String(m)) })),
  ]
</script>

<Card title={$t('calendarSettings.view')}>
  <Row id="calendar-view" label={$t('calendarSettings.defaultView')} hint={$t('calendarSettings.defaultView.hint')} stacked>
    <ChoiceCards
      label={$t('calendarSettings.defaultView')}
      value={s.defaultView}
      options={viewOptions}
      minWidth={130}
      on:change={(e) => saveCalendarSettings({ defaultView: e.detail as CalendarView })}
      let:option
    >
      <CalendarSkeleton view={option.key as CalendarView} />
    </ChoiceCards>
  </Row>
  <Row id="calendar-week-start" label={$t('calendarSettings.weekStart')} hint={$t('calendarSettings.weekStart.hint')}>
    <Segmented
      value={String(s.weekStart)}
      options={weekStartOptions}
      label={$t('calendarSettings.weekStart')}
      on:change={(e) => saveCalendarSettings({ weekStart: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-working-hours" label={$t('calendarSettings.workingHours')} hint={$t('calendarSettings.workingHours.hint')}>
    <Select
      value={String(s.workDayStart)}
      items={hourItems(0, s.workDayEnd - 30)}
      ariaLabel={$t('calendarSettings.workStart')}
      on:change={(e) => saveCalendarSettings({ workDayStart: Number(e.detail) })}
    />
    <span class="to">{$t('calendar.to')}</span>
    <Select
      value={String(s.workDayEnd)}
      items={hourItems(s.workDayStart + 30, 24 * 60)}
      ariaLabel={$t('calendarSettings.workEnd')}
      on:change={(e) => saveCalendarSettings({ workDayEnd: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-time-zone" label={$t('calendarSettings.zone')} hint={$t('calendarSettings.zone.hint')} stacked>
    <ChoiceCards
      label={$t('calendarSettings.zone')}
      value={s.timeZoneDisplay}
      options={zoneOptions}
      minWidth={150}
      on:change={(e) => saveCalendarSettings({ timeZoneDisplay: e.detail as TimeZoneDisplay })}
      let:option
    >
      <ZoneSkeleton mode={option.key as TimeZoneDisplay} />
    </ChoiceCards>
  </Row>
  {#if s.timeZoneDisplay === 'dual'}
    <Row id="calendar-second-zone" label={$t('calendarSettings.secondZone')} hint={$t('calendarSettings.secondZone.hint')}>
      <Select
        value={s.secondaryTimeZone}
        items={zones}
        ariaLabel={$t('calendarSettings.secondZone')}
        on:change={(e) => saveCalendarSettings({ secondaryTimeZone: e.detail })}
      />
    </Row>
  {/if}
</Card>

<Card title={$t('calendarSettings.events')}>
  <Row id="calendar-default-calendar" label={$t('calendarSettings.defaultCalendar')} hint={$t('calendarSettings.defaultCalendar.hint')}>
    <Select
      value={String(s.defaultCalendarId)}
      items={calendarItems}
      ariaLabel={$t('calendarSettings.defaultCalendar')}
      on:change={(e) => saveCalendarSettings({ defaultCalendarId: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-default-reminder" label={$t('calendarSettings.defaultReminder')} hint={$t('calendarSettings.defaultReminder.hint')}>
    <Select
      value={String(s.defaultReminderMinutes)}
      items={reminderItems}
      ariaLabel={$t('calendarSettings.defaultReminder')}
      on:change={(e) => saveCalendarSettings({ defaultReminderMinutes: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-reminders" label={$t('calendarSettings.reminders')} hint={$t('calendarSettings.reminders.hint')}>
    <ToggleSwitch
      checked={s.remindersEnabled}
      label={$t('calendarSettings.reminders')}
      on:change={(e) => saveCalendarSettings({ remindersEnabled: e.detail })}
    />
  </Row>
  <Row id="calendar-show-tasks" label={$t('taskSettings.inCalendar')} hint={$t('taskSettings.inCalendar.hint')}>
    <ToggleSwitch checked={$taskSettings.showInCalendar} label={$t('taskSettings.inCalendar')} on:change={(e) => saveTaskSettings({ showInCalendar: e.detail })} />
  </Row>
  <Row id="calendar-ics-handler" label={$t('calendarSettings.icsHandler')} hint={$t('calendarSettings.icsHandler.hint')}>
    {#if icsHandler.known && icsHandler.isDefault}
      <span class="to">{$t('calendarSettings.icsHandler.isDefault')}</span>
    {:else}
      <button type="button" class="cal-btn" on:click={useForICS}>{$t('calendarSettings.icsHandler.set')}</button>
    {/if}
  </Row>
</Card>

<Card title={$t('calendarSettings.timeblock')} description={$t('calendarSettings.timeblock.hint')}>
  <Row id="calendar-block-length" label={$t('calendarSettings.timeblock.length')} hint={$t('calendarSettings.timeblock.length.hint')}>
    <Select
      value={String(b.defaultMinutes)}
      items={minuteItems([15, 30, 45, 60, 90, 120], b.defaultMinutes)}
      ariaLabel={$t('calendarSettings.timeblock.length')}
      on:change={(e) => saveBlockSettings({ defaultMinutes: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-block-work-days" label={$t('calendarSettings.timeblock.workDays')} hint={$t('calendarSettings.timeblock.workDays.hint')}>
    <div class="days" role="group" aria-label={$t('calendarSettings.timeblock.workDays')}>
      {#each weekOrder as day (day)}
        <button type="button" class="cal-btn day" aria-pressed={b.workDays.includes(day)} on:click={() => toggleWorkDay(day)}>
          {weekdayName(day, navigator.language, 'short')}
        </button>
      {/each}
    </div>
  </Row>
  <Row id="calendar-block-min-slot" label={$t('calendarSettings.timeblock.minSlot')} hint={$t('calendarSettings.timeblock.minSlot.hint')}>
    <Select
      value={String(b.minSlotMinutes)}
      items={minuteItems([5, 10, 15, 30, 45, 60], b.minSlotMinutes)}
      ariaLabel={$t('calendarSettings.timeblock.minSlot')}
      on:change={(e) => saveBlockSettings({ minSlotMinutes: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-block-buffer" label={$t('calendarSettings.timeblock.buffer')} hint={$t('calendarSettings.timeblock.buffer.hint')}>
    <Select
      value={String(b.bufferMinutes)}
      items={[{ value: '0', label: $t('calendarSettings.timeblock.buffer.none') }, ...minuteItems([5, 10, 15, 30], b.bufferMinutes).filter((i) => i.value !== '0')]}
      ariaLabel={$t('calendarSettings.timeblock.buffer')}
      on:change={(e) => saveBlockSettings({ bufferMinutes: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-block-search" label={$t('calendarSettings.timeblock.searchDays')} hint={$t('calendarSettings.timeblock.searchDays.hint')}>
    <Select
      value={String(b.searchDays)}
      items={[3, 7, 14, 31].map((n) => ({ value: String(n), label: $t('calendarSettings.timeblock.days').replace('{n}', String(n)) }))}
      ariaLabel={$t('calendarSettings.timeblock.searchDays')}
      on:change={(e) => saveBlockSettings({ searchDays: Number(e.detail) })}
    />
  </Row>
  <Row id="calendar-block-write" label={$t('calendarSettings.timeblock.write')} hint={$t('calendarSettings.timeblock.write.hint')}>
    <ToggleSwitch checked={b.writeToCalendar} label={$t('calendarSettings.timeblock.write')} on:change={(e) => saveBlockSettings({ writeToCalendar: e.detail })} />
  </Row>
  {#if b.writeToCalendar}
    <Row id="calendar-block-calendar" label={$t('calendarSettings.timeblock.calendar')} hint={$t('calendarSettings.timeblock.calendar.hint')}>
      <Select
        value={String(b.calendarId)}
        items={calendarItems}
        ariaLabel={$t('calendarSettings.timeblock.calendar')}
        on:change={(e) => saveBlockSettings({ calendarId: Number(e.detail) })}
      />
    </Row>
  {/if}
  <Row id="calendar-block-title" label={$t('calendarSettings.timeblock.eventTitle')} hint={$t('calendarSettings.timeblock.eventTitle.hint')}>
    <input
      class="cal-field"
      value={b.eventTitle}
      aria-label={$t('calendarSettings.timeblock.eventTitle')}
      on:change={(e) => e.currentTarget.value.trim() && saveBlockSettings({ eventTitle: e.currentTarget.value.trim() })}
    />
  </Row>
  <Row id="calendar-block-include-title" label={$t('calendarSettings.timeblock.includeTitle')} hint={$t('calendarSettings.timeblock.includeTitle.hint')}>
    <ToggleSwitch checked={b.includeTitle} label={$t('calendarSettings.timeblock.includeTitle')} on:change={(e) => saveBlockSettings({ includeTitle: e.detail })} />
  </Row>
</Card>

<Card title={$t('calendarSettings.accounts')} description={$t('calendarSettings.accounts.hint')}>
  <Row id="calendar-sync" label={$t('calendarSettings.sync')} hint={$t('calendarSettings.sync.hint')}>
    <Select
      value={String(s.syncIntervalMinutes)}
      items={syncItems}
      ariaLabel={$t('calendarSettings.sync')}
      on:change={(e) => saveCalendarSettings({ syncIntervalMinutes: Number(e.detail) })}
    />
  </Row>
  <div data-setting="calendar-accounts">
    <CalDavAccounts />
  </div>
</Card>

<style>
  .to {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .days {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .day[aria-pressed='true'] {
    border-color: var(--accent);
    background: var(--selection-bg);
  }
</style>
