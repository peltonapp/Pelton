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
  import { atMinutes, localeWeekStart } from '../../../lib/caldate'
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
</style>
