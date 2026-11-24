// palette-calendar.ts puts the calendar into the command palette: making an
// event, the quick add, moving around the calendar, and showing or hiding
// single calendars. the entries work from any space; one that needs the
// calendar screen opens it first.

import { get } from 'svelte/store'
import {
  IconCalendar,
  IconCalendarPlus,
  IconCalendarEvent,
  IconCalendarWeek,
  IconCalendarMonth,
  IconCalendarStats,
  IconListDetails,
  IconBolt,
  IconRefresh,
  IconFileImport,
  IconEye,
  IconSettings,
  IconCalendarCheck,
  IconCalendarTime,
  IconMailForward,
} from '@tabler/icons-svelte'
import { registerPaletteSource } from './palettesources'
import type { CommandStep, PaletteCommand } from './commands'
import { blockMailRequest, calendars, calendarFocus, calendarView, findTimeFor, planMyDay, refreshCalendars, toggleCalendar } from '../stores/calendar'
import { tasks } from '../stores/tasks'
import { openSpace, runInSpace } from '../stores/space'
import { openSettingsAt } from '../stores/settingsnav'
import { menuHint } from '../stores/shortcuts'
import type { CalendarView } from './types'

const views: { key: CalendarView; labelKey: string; icon: PaletteCommand['icon'] }[] = [
  { key: 'day', labelKey: 'calendar.view.day', icon: IconCalendarEvent },
  { key: 'week', labelKey: 'calendar.view.week', icon: IconCalendarWeek },
  { key: 'month', labelKey: 'calendar.view.month', icon: IconCalendarMonth },
  { key: 'agenda', labelKey: 'calendar.view.agenda', icon: IconListDetails },
  { key: 'year', labelKey: 'calendar.view.year', icon: IconCalendarStats },
]

// a step listing every calendar; picking one shows or hides it.
function calendarsStep(t: (key: string) => string): CommandStep {
  return {
    label: t('palette.calendar.toggle'),
    placeholder: t('palette.calendar.togglePlaceholder'),
    items: get(calendars).map((cal) => ({
      id: `calendar:toggle:${cal.id}`,
      group: 'calendar',
      label: cal.name || t('calendar.defaultName'),
      hint: cal.visible ? t('palette.calendar.shown') : t('palette.calendar.hidden'),
      icon: IconCalendar,
      swatch: cal.color || undefined,
      run: () => void toggleCalendar(cal.id, !cal.visible),
    })),
  }
}

// a step listing the open tasks; picking one finds time for it.
function findTimeStep(t: (key: string) => string): CommandStep {
  return {
    label: t('timeblock.palette.findTime'),
    placeholder: t('timeblock.palette.pickTask'),
    items: get(tasks)
      .filter((x) => !x.done)
      .map((x) => ({
        id: `calendar:find-time:${x.id}`,
        group: 'calendar' as const,
        label: x.title || t('tasks.untitled'),
        icon: IconCalendarTime,
        swatch: x.listColor || undefined,
        run: () => findTimeFor(x.id),
      })),
  }
}

registerPaletteSource('calendar', (t) => {
  const out: PaletteCommand[] = [
    {
      id: 'calendar:plan-day',
      group: 'calendar',
      label: t('timeblock.palette.planDay'),
      keywords: t('timeblock.palette.planKeywords'),
      hint: menuHint('plan-day'),
      icon: IconCalendarCheck,
      run: () => planMyDay(),
    },
    {
      id: 'calendar:block-mail',
      group: 'calendar',
      label: t('timeblock.palette.blockMail'),
      keywords: t('timeblock.palette.blockKeywords'),
      hint: menuHint('block-time'),
      icon: IconMailForward,
      run: () => blockMailRequest.set(true),
    },
    {
      id: 'calendar:open',
      group: 'calendar',
      label: t('palette.calendar.open'),
      icon: IconCalendar,
      run: () => openSpace('calendar'),
    },
    {
      id: 'calendar:new-event',
      group: 'calendar',
      label: t('calendar.newEvent'),
      keywords: t('palette.calendar.newKeywords'),
      icon: IconCalendarPlus,
      run: () => runInSpace('calendar', 'compose'),
    },
    {
      id: 'calendar:quick-add',
      group: 'calendar',
      label: t('palette.calendar.quickAdd'),
      keywords: t('palette.calendar.quickAddKeywords'),
      hint: menuHint('calendar-quick-add'),
      icon: IconBolt,
      run: () => runInSpace('calendar', 'calendar-quick-add'),
    },
    {
      id: 'calendar:today',
      group: 'calendar',
      label: t('palette.calendar.today'),
      icon: IconCalendarEvent,
      run: () => {
        calendarFocus.set(new Date())
        openSpace('calendar')
      },
    },
  ]
  for (const view of views) {
    out.push({
      id: `calendar:view:${view.key}`,
      group: 'calendar',
      label: t('palette.calendar.view').replace('{view}', t(view.labelKey)),
      icon: view.icon,
      run: () => {
        calendarView.set(view.key)
        openSpace('calendar')
      },
    })
  }
  out.push(
    {
      id: 'calendar:sync',
      group: 'calendar',
      label: t('calendar.sync'),
      icon: IconRefresh,
      run: () => void refreshCalendars(),
    },
    {
      id: 'calendar:import',
      group: 'calendar',
      label: t('calendar.importFile'),
      keywords: 'ics ical jscalendar',
      icon: IconFileImport,
      run: () => runInSpace('calendar', 'calendar-import'),
    },
    {
      id: 'calendar:settings',
      group: 'calendar',
      label: t('palette.calendar.settings'),
      icon: IconSettings,
      run: () => openSettingsAt('calendar'),
    },
  )
  if (get(tasks).some((x) => !x.done)) {
    out.push({
      id: 'calendar:find-time',
      group: 'calendar',
      label: t('timeblock.palette.findTime'),
      keywords: t('timeblock.palette.blockKeywords'),
      icon: IconCalendarTime,
      run: () => findTimeStep(t),
    })
  }
  if (get(calendars).length > 0) {
    out.push({
      id: 'calendar:toggle',
      group: 'calendar',
      label: t('palette.calendar.toggle'),
      icon: IconEye,
      run: () => calendarsStep(t),
    })
  }
  return out
})
