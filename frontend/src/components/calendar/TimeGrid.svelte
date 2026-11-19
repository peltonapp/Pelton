<script lang="ts">
  // the week and day views: a column per day under a row of dates and an
  // all-day lane, with the hours down the side. events sit in their columns
  // side by side where they overlap. dragging over empty time draws a new
  // event, dragging an event moves it, and dragging its lower edge changes
  // when it ends; in the all-day lane the same works across days. mail dropped
  // on a slot starts an event there.
  //
  // every position is a share of the day column's height, so the interface
  // scale and the density's hour height never enter the math: a pointer's
  // place is read as its offset into the column over the column's height.
  import { createEventDispatcher, onDestroy, onMount, tick } from 'svelte'
  import { openScriptMenu } from '../../lib/scriptmenu'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import TaskTick from '../tasks/TaskTick.svelte'
  import { maildrop, mailDrag, type MailDropOptions } from '../../stores/maildrag'
  import { mailDropHint } from './maildrop'
  import {
    addDays,
    atMinutes,
    dayKey,
    dayMinutes,
    isLaneEvent,
    layoutDay,
    layoutRow,
    minutesOfDay,
    sameDay,
    startOfDay,
    type Placed,
  } from '../../lib/caldate'
  import { formatHour, formatLongDay, formatTime, formatTimeRange } from '../../lib/calformat'
  import { occurrenceKey } from './occurrence'
  import { eventLabel } from './labels'
  import { calendars, type CellMenuAt, type MailDropAt } from '../../stores/calendar'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence, TimeZoneDisplay } from '../../lib/types'

  /** The days shown, one column each. */
  export let days: Date[]
  /** The occurrences of the range, times read. */
  export let placed: Placed<Occurrence>[]
  /** Colors by calendar id. */
  export let colors: Map<number, string>
  /** The current time, for the now line. */
  export let now: Date
  /** Working hours, minutes after midnight. */
  export let workStart: number = 9 * 60
  export let workEnd: number = 17 * 60
  export let timeFormat: TimeFormat = 'auto'
  export let zoneDisplay: TimeZoneDisplay = 'off'
  export let secondaryZone: string = ''
  /** The event a quick create is open for, drawn as a ghost. */
  export let pending: { start: Date; end: Date; allDay: boolean } | null = null
  /** The occurrence whose details are open. */
  export let selectedKey: string = ''
  /** A popover is open: the next press only closes it. */
  export let busy: boolean = false

  const dispatch = createEventDispatcher<{
    create: { start: Date; end: Date; allDay: boolean }
    open: { occ: Occurrence; anchor: HTMLElement }
    change: { occ: Occurrence; start: Date; end: Date }
    day: Date
    mailDrop: MailDropAt
    menu: CellMenuAt
    toggleTask: Occurrence
  }>()

  const step = 15
  const clickMinutes = 60
  const dragThreshold = 4
  const hours = Array.from({ length: 24 }, (_, h) => h)

  let scroller: HTMLDivElement
  let colEls: HTMLDivElement[] = []
  let laneCells: HTMLDivElement[] = []

  $: today = startOfDay(now)
  $: lanePlaced = placed.filter(isLaneEvent)
  $: timedPlaced = placed.filter((p) => !isLaneEvent(p))
  $: laneSpans = layoutRow(lanePlaced, days)
  $: laneCount = Math.max(1, ...laneSpans.map((s) => s.lane + 1))
  $: boxes = days.map((day) => layoutDay(timedPlaced, day))
  $: nowMinutes = minutesOfDay(now)
  $: dual = zoneDisplay === 'dual' && secondaryZone !== ''
  $: localZone = zoneName(undefined, now)
  $: altZone = dual ? zoneName(secondaryZone, now) : ''

  function zoneName(timeZone: string | undefined, at: Date): string {
    try {
      const parts = new Intl.DateTimeFormat(undefined, { timeZone, timeZoneName: 'short' }).formatToParts(at)
      return parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
    } catch {
      return ''
    }
  }

  function hourLabel(h: number, zone?: string): string {
    return formatHour(atMinutes(days[0] ?? now, h * 60), timeFormat, undefined, zone)
  }

  function pct(minutes: number): string {
    return `${(minutes / dayMinutes) * 100}%`
  }

  function colorOf(occ: Occurrence): string {
    return colors.get(occ.calendarId) ?? occ.calendarColor ?? 'var(--cal-1)'
  }

  // the calendar an event is in, said in words where the grid only shows its color.
  function calendarNameOf(id: number): string {
    const cal = $calendars.find((c) => c.id === id)
    return cal ? cal.name || $t('calendar.defaultName') : ''
  }

  // --- pointer geometry ----------------------------------------------------

  function columnAt(clientX: number, list: HTMLElement[]): number {
    let best = 0
    let bestDistance = Infinity
    list.forEach((el, i) => {
      const r = el?.getBoundingClientRect()
      if (!r) {
        return
      }
      if (clientX >= r.left && clientX < r.right) {
        best = i
        bestDistance = -1
      } else if (bestDistance >= 0) {
        const d = Math.min(Math.abs(clientX - r.left), Math.abs(clientX - r.right))
        if (d < bestDistance) {
          best = i
          bestDistance = d
        }
      }
    })
    return best
  }

  function minutesAtY(clientY: number, round: (n: number) => number = Math.round): number {
    const r = colEls[0]?.getBoundingClientRect()
    if (!r || r.height === 0) {
      return 0
    }
    const ratio = Math.min(1, Math.max(0, (clientY - r.top) / r.height))
    return Math.min(dayMinutes, round((ratio * dayMinutes) / step) * step)
  }

  function timeAt(col: number, minutes: number): Date {
    return atMinutes(days[col], minutes)
  }

  // --- drags -----------------------------------------------------------------

  type Drag =
    | { kind: 'create'; col: number; anchor: number; current: number }
    | { kind: 'move'; p: Placed<Occurrence>; el: HTMLElement; grab: number; start: Date }
    | { kind: 'resize'; p: Placed<Occurrence>; el: HTMLElement; end: Date }
    | { kind: 'laneCreate'; anchor: number; current: number }
    | { kind: 'laneMove'; p: Placed<Occurrence>; el: HTMLElement; from: number; delta: number }

  let drag: Drag | null = null
  let moved = false
  let downX = 0
  let downY = 0

  function begin(event: PointerEvent, next: Drag): void {
    drag = next
    moved = false
    downX = event.clientX
    downY = event.clientY
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('keydown', onKey, true)
  }

  function end(): void {
    drag = null
    window.removeEventListener('pointermove', onMove)
    window.removeEventListener('pointerup', onUp)
    window.removeEventListener('keydown', onKey, true)
  }

  // a press on empty time while a popover is open only closes the popover; a
  // press on an event opens that one instead.
  function usable(event: PointerEvent, creates: boolean): boolean {
    return event.button === 0 && event.pointerType !== 'touch' && !(creates && busy)
  }

  function onColumnDown(event: PointerEvent, col: number): void {
    if (!usable(event, true) || (event.target as HTMLElement).closest('.ev')) {
      return
    }
    event.preventDefault()
    const at = minutesAtY(event.clientY, Math.floor)
    begin(event, { kind: 'create', col, anchor: Math.min(at, dayMinutes - step), current: at })
  }

  function onEventDown(event: PointerEvent, p: Placed<Occurrence>): void {
    if (!usable(event, false)) {
      return
    }
    event.stopPropagation()
    const el = event.currentTarget as HTMLElement
    const col = columnAt(event.clientX, colEls)
    const pointer = timeAt(col, minutesAtY(event.clientY))
    begin(event, { kind: 'move', p, el, grab: pointer.getTime() - p.start.getTime(), start: p.start })
  }

  function onResizeDown(event: PointerEvent, p: Placed<Occurrence>): void {
    if (!usable(event, false) || p.occ.readOnly) {
      return
    }
    event.stopPropagation()
    event.preventDefault()
    const el = (event.currentTarget as HTMLElement).closest('.ev') as HTMLElement
    begin(event, { kind: 'resize', p, el, end: p.end })
  }

  function onLaneDown(event: PointerEvent): void {
    if (!usable(event, true) || (event.target as HTMLElement).closest('.bar')) {
      return
    }
    event.preventDefault()
    const col = columnAt(event.clientX, laneCells)
    begin(event, { kind: 'laneCreate', anchor: col, current: col })
  }

  function onBarDown(event: PointerEvent, p: Placed<Occurrence>): void {
    if (!usable(event, false)) {
      return
    }
    event.stopPropagation()
    begin(event, { kind: 'laneMove', p, el: event.currentTarget as HTMLElement, from: columnAt(event.clientX, laneCells), delta: 0 })
  }

  function onMove(event: PointerEvent): void {
    if (!drag) {
      return
    }
    if (!moved && Math.hypot(event.clientX - downX, event.clientY - downY) < dragThreshold) {
      return
    }
    moved = true
    switch (drag.kind) {
      case 'create':
        drag = { ...drag, current: minutesAtY(event.clientY) }
        break
      case 'move': {
        if (drag.p.occ.readOnly) {
          return
        }
        const col = columnAt(event.clientX, colEls)
        const pointer = timeAt(col, minutesAtY(event.clientY))
        const start = new Date(pointer.getTime() - drag.grab)
        start.setMinutes(Math.round(start.getMinutes() / step) * step, 0, 0)
        drag = { ...drag, start }
        break
      }
      case 'resize': {
        const col = columnAt(event.clientX, colEls)
        let endAt = timeAt(col, minutesAtY(event.clientY))
        const min = new Date(drag.p.start.getTime() + step * 60000)
        if (endAt < min) {
          endAt = min
        }
        drag = { ...drag, end: endAt }
        break
      }
      case 'laneCreate':
        drag = { ...drag, current: columnAt(event.clientX, laneCells) }
        break
      case 'laneMove':
        if (!drag.p.occ.readOnly) {
          drag = { ...drag, delta: columnAt(event.clientX, laneCells) - drag.from }
        }
        break
    }
  }

  function onUp(): void {
    const d = drag
    const wasMoved = moved
    end()
    if (!d) {
      return
    }
    switch (d.kind) {
      case 'create': {
        const range = createRange(d, wasMoved)
        dispatch('create', { start: timeAt(d.col, range.from), end: timeAt(d.col, range.to), allDay: false })
        break
      }
      case 'move':
        if (!wasMoved) {
          dispatch('open', { occ: d.p.occ, anchor: d.el })
        } else if (d.start.getTime() !== d.p.start.getTime()) {
          dispatch('change', { occ: d.p.occ, start: d.start, end: new Date(d.start.getTime() + (d.p.end.getTime() - d.p.start.getTime())) })
        }
        break
      case 'resize':
        if (wasMoved && d.end.getTime() !== d.p.end.getTime()) {
          dispatch('change', { occ: d.p.occ, start: d.p.start, end: d.end })
        }
        break
      case 'laneCreate': {
        const from = Math.min(d.anchor, d.current)
        const to = Math.max(d.anchor, d.current)
        dispatch('create', { start: startOfDay(days[from]), end: addDays(startOfDay(days[to]), 1), allDay: true })
        break
      }
      case 'laneMove':
        if (!wasMoved || d.delta === 0) {
          if (!wasMoved) {
            dispatch('open', { occ: d.p.occ, anchor: d.el })
          }
        } else {
          dispatch('change', { occ: d.p.occ, start: addDays(d.p.start, d.delta), end: addDays(d.p.end, d.delta) })
        }
        break
    }
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key === 'Escape' && drag) {
      event.preventDefault()
      event.stopPropagation()
      end()
    }
  }

  function createRange(d: { anchor: number; current: number }, wasMoved: boolean): { from: number; to: number } {
    if (!wasMoved) {
      const from = Math.min(d.anchor, dayMinutes - clickMinutes)
      return { from, to: from + clickMinutes }
    }
    if (d.current >= d.anchor) {
      return { from: d.anchor, to: Math.max(d.anchor + step, d.current) }
    }
    return { from: d.current, to: d.anchor + step }
  }

  // the ghost a drag or an open quick create draws: which column, and where.
  type Ghost = { col: number; top: number; bottom: number; label: string; kind: 'new' | 'move' }
  $: ghosts = ghostsFor(drag, moved, pending, days)

  function clipToDays(list: Date[], start: Date, endAt: Date, kind: Ghost['kind']): Ghost[] {
    const out: Ghost[] = []
    list.forEach((day, col) => {
      const from = startOfDay(day)
      const to = addDays(from, 1)
      if (start < to && endAt > from) {
        const top = Math.max(0, (start.getTime() - from.getTime()) / 60000)
        const bottom = Math.min(dayMinutes, (endAt.getTime() - from.getTime()) / 60000)
        out.push({ col, top, bottom, label: formatTimeRange(start, endAt, timeFormat), kind })
      }
    })
    return out
  }

  function ghostsFor(d: Drag | null, isMoved: boolean, open: typeof pending, list: Date[]): Ghost[] {
    if (d?.kind === 'create' && isMoved) {
      const r = createRange(d, true)
      return clipToDays(list, timeAt(d.col, r.from), timeAt(d.col, r.to), 'new')
    }
    if (d?.kind === 'move' && isMoved && !d.p.occ.readOnly) {
      return clipToDays(list, d.start, new Date(d.start.getTime() + (d.p.end.getTime() - d.p.start.getTime())), 'move')
    }
    if (d?.kind === 'resize' && isMoved) {
      return clipToDays(list, d.p.start, d.end, 'move')
    }
    if (open && !open.allDay) {
      return clipToDays(list, open.start, open.end, 'new')
    }
    return []
  }

  $: draggingKey = drag && moved && (drag.kind === 'move' || drag.kind === 'resize' || drag.kind === 'laneMove') ? occurrenceKey(drag.p.occ) : ''

  // the lane's ghost: a new all-day span, or a bar being moved.
  $: laneGhost = laneGhostFor(drag, moved, pending, days)

  function laneGhostFor(d: Drag | null, isMoved: boolean, open: typeof pending, list: Date[]): { col: number; span: number } | null {
    if (d?.kind === 'laneCreate' && isMoved) {
      return { col: Math.min(d.anchor, d.current), span: Math.abs(d.anchor - d.current) + 1 }
    }
    if (d?.kind === 'laneMove' && isMoved && d.delta !== 0) {
      const span = laneSpans.find((s) => occurrenceKey(s.placed.occ) === occurrenceKey(d.p.occ))
      if (span) {
        const col = Math.max(0, span.col + d.delta)
        return { col, span: Math.max(1, Math.min(list.length, span.col + d.delta + span.span) - col) }
      }
    }
    if (open?.allDay && list.length) {
      const first = startOfDay(list[0])
      const from = Math.max(0, Math.round((startOfDay(open.start).getTime() - first.getTime()) / 86400000))
      const to = Math.min(list.length, Math.round((startOfDay(open.end).getTime() - first.getTime()) / 86400000))
      if (to > from) {
        return { col: from, span: to - from }
      }
    }
    return null
  }

  // --- mail dropped on a slot --------------------------------------------------

  let lastMail = { x: 0, y: 0 }
  const offMail = mailDrag.subscribe((d) => {
    if (d) {
      lastMail = { x: d.x, y: d.y }
    }
  })

  function inside(el: HTMLElement | undefined, x: number, y: number): boolean {
    const r = el?.getBoundingClientRect()
    return !!r && x >= r.left && x < r.right && y >= r.top && y < r.bottom
  }

  $: dropHint = $mailDrag && scroller ? hintAt($mailDrag.x, $mailDrag.y) : null

  function hintAt(x: number, y: number): { col: number; minutes: number } | null {
    const col = colEls.findIndex((el) => inside(el, x, y))
    if (col === -1) {
      return null
    }
    return { col, minutes: Math.min(dayMinutes - clickMinutes, minutesAtY(y, Math.floor)) }
  }

  function slotDrop(col: number): MailDropOptions {
    return {
      onDrop: (items, info) => {
        const hit = hintAt(lastMail.x, lastMail.y)
        if (items.length > 0) {
          dispatch('mailDrop', { messageIds: items.map((m) => m.id), at: timeAt(hit?.col ?? col, hit?.minutes ?? workStart), mode: 'time', shift: info.shift })
        }
      },
      hint: (items, shift) => mailDropHint(items.length, shift, true),
    }
  }

  function laneDrop(col: number): MailDropOptions {
    return {
      onDrop: (items, info) => {
        if (items.length > 0) {
          dispatch('mailDrop', { messageIds: items.map((m) => m.id), at: startOfDay(days[col]), mode: 'allDay', shift: info.shift })
        }
      },
      hint: (items, shift) => mailDropHint(items.length, shift, true),
    }
  }

  // a right click on empty time or the all-day lane asks for the cell menu.
  function onColumnMenu(event: MouseEvent, col: number): void {
    if ((event.target as HTMLElement).closest('.ev')) {
      return
    }
    event.preventDefault()
    dispatch('menu', { at: timeAt(col, Math.min(dayMinutes - step, minutesAtY(event.clientY, Math.floor))), mode: 'time', x: event.clientX, y: event.clientY })
  }

  function onLaneMenu(event: MouseEvent): void {
    if ((event.target as HTMLElement).closest('.bar')) {
      return
    }
    event.preventDefault()
    dispatch('menu', { at: startOfDay(days[columnAt(event.clientX, laneCells)]), mode: 'allDay', x: event.clientX, y: event.clientY })
  }

  // --- scrolling ---------------------------------------------------------------

  /** Scrolls the hours so the given minute sits a little below the top. */
  function scrollToMinutes(minutes: number): void {
    const body = scroller?.querySelector<HTMLElement>('.body')
    if (body) {
      scroller.scrollTop = (Math.max(0, minutes - 30) / dayMinutes) * body.offsetHeight
    }
  }

  onMount(async () => {
    await tick()
    const showsToday = days.some((d) => sameDay(d, now))
    scrollToMinutes(showsToday ? Math.max(workStart, nowMinutes - 60) : workStart)
  })

  onDestroy(() => {
    end()
    offMail()
  })
</script>

<div class="tg" class:dual class:single={days.length === 1} style={`--days:${days.length}`}>
  <div class="scroller" bind:this={scroller}>
    <div class="top">
      <div class="row head">
        <div class="corner zones">
          {#if zoneDisplay !== 'off'}
            {#if dual}<span>{altZone}</span>{/if}
            <span>{localZone}</span>
          {/if}
        </div>
        {#each days as day, i (dayKey(day))}
          <button
            type="button"
            class="dh"
            class:today={sameDay(day, today)}
            class:weekend={day.getDay() === 0 || day.getDay() === 6}
            aria-current={sameDay(day, today) ? 'date' : undefined}
            aria-label={`${day.toLocaleDateString(undefined, { weekday: 'short' })} ${day.getDate()}, ${formatLongDay(day)}`}
            style={`grid-column:${i + 2}`}
            on:click={() => dispatch('day', day)}
          >
            <span class="wd">{day.toLocaleDateString(undefined, { weekday: 'short' })}</span>
            <span class="num">{day.getDate()}</span>
          </button>
        {/each}
      </div>
      <div class="row lane">
        <div class="corner lane-label">{$t('calendar.allDay')}</div>
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <div class="lane-grid" style={`--lanes:${laneCount}`} on:pointerdown={onLaneDown} on:contextmenu={onLaneMenu}>
          {#each days as day, i (dayKey(day))}
            <div class="lane-cell" style={`grid-column:${i + 1}`} bind:this={laneCells[i]} use:maildrop={laneDrop(i)}></div>
          {/each}
          {#each laneSpans as s (occurrenceKey(s.placed.occ))}
            {@const occ = s.placed.occ}
            <button
              type="button"
              class="bar"
              class:before={s.before}
              class:after={s.after}
              class:selected={selectedKey === occurrenceKey(occ)}
              class:dragging={draggingKey === occurrenceKey(occ)}
              class:cancelled={occ.status === 'CANCELLED'}
              class:task={occ.task}
              class:done={occ.task?.done}
              style={`grid-column:${s.col + 1} / span ${s.span};grid-row:${s.lane + 1};--ev:${colorOf(occ)}`}
              on:pointerdown={(e) => onBarDown(e, s.placed)}
              on:click={(e) => e.detail === 0 && dispatch('open', { occ, anchor: e.currentTarget })}
              on:contextmenu={(e) => !occ.task && openScriptMenu(e, 'calendar.event', { id: String(occ.eventId) })}
            >
              {#if occ.task}
                <TaskTick
                  checked={occ.task.done}
                  label={$t(occ.task.done ? 'tasks.inCalendarOpen' : 'tasks.inCalendarDone').replace('{title}', occ.summary || $t('tasks.untitled'))}
                  disabled={occ.task.readOnly}
                  on:toggle={() => dispatch('toggleTask', occ)}
                />
              {/if}
              <span class="title">{occ.summary || $t(occ.task ? 'tasks.untitled' : 'calendar.untitled')}</span>
            </button>
          {/each}
          {#if laneGhost}
            <div class="bar ghost" style={`grid-column:${laneGhost.col + 1} / span ${laneGhost.span};grid-row:1`} data-pending=""></div>
          {/if}
        </div>
      </div>
    </div>

    <div class="row body" style={`height:calc(24 * var(--cal-hour-height))`}>
      <div class="gutter" aria-hidden="true">
        {#if dual}
          <div class="alt">
            {#each hours.slice(1) as h (h)}
              <span class="hour" style={`top:${pct(h * 60)}`}>{hourLabel(h, secondaryZone)}</span>
            {/each}
          </div>
        {/if}
        <div class="local">
          {#each hours.slice(1) as h (h)}
            <span class="hour" style={`top:${pct(h * 60)}`}>{hourLabel(h)}</span>
          {/each}
        </div>
      </div>
      <div class="cols">
        <div class="lines" aria-hidden="true">
          {#each hours as h (h)}
            <i class="line" style={`top:${pct(h * 60)}`}></i>
            <i class="line half" style={`top:${pct(h * 60 + 30)}`}></i>
          {/each}
        </div>
        {#each days as day, i (dayKey(day))}
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div
            class="col"
            class:today={sameDay(day, today)}
            bind:this={colEls[i]}
            use:maildrop={slotDrop(i)}
            on:pointerdown={(e) => onColumnDown(e, i)}
            on:contextmenu={(e) => onColumnMenu(e, i)}
          >
            {#if workStart > 0}
              <div class="off" style={`top:0;height:${pct(workStart)}`}></div>
            {/if}
            {#if workEnd < dayMinutes}
              <div class="off" style={`top:${pct(workEnd)};bottom:0`}></div>
            {/if}
            {#if day.getDay() === 0 || day.getDay() === 6}
              <div class="off weekend" style="top:0;bottom:0"></div>
            {/if}

            {#each boxes[i] ?? [] as b (occurrenceKey(b.placed.occ))}
              {@const occ = b.placed.occ}
              {@const short = b.bottom - b.top < 45}
              <button
                type="button"
                class="ev"
                class:short
                class:selected={selectedKey === occurrenceKey(occ)}
                class:dragging={draggingKey === occurrenceKey(occ)}
                class:tentative={occ.status === 'TENTATIVE'}
                class:cancelled={occ.status === 'CANCELLED'}
                class:free={occ.transparency === 'TRANSPARENT'}
                class:readonly={occ.readOnly}
                class:task={occ.task}
                class:done={occ.task?.done}
                style={`top:${pct(b.top)};height:${pct(b.bottom - b.top)};--col:${b.col};--cols:${b.cols};--ev:${colorOf(occ)}`}
                aria-label={occ.task ? undefined : eventLabel({
                  title: occ.summary || $t('calendar.untitled'),
                  when: `${formatTimeRange(b.placed.start, b.placed.end, timeFormat)}, ${occ.location && !short ? `${occ.location}, ` : ''}${formatLongDay(b.placed.start)}`,
                  calendar: calendarNameOf(occ.calendarId),
                  status: occ.status,
                  free: occ.transparency === 'TRANSPARENT',
                  conflict: occ.conflict,
                  recurring: occ.recurring,
                }, $t)}
                on:pointerdown={(e) => onEventDown(e, b.placed)}
                on:click={(e) => e.detail === 0 && dispatch('open', { occ, anchor: e.currentTarget })}
                on:contextmenu={(e) => !occ.task && openScriptMenu(e, 'calendar.event', { id: String(occ.eventId) })}
              >
                <span class="title">
                  {#if occ.task}
                    <TaskTick
                      checked={occ.task.done}
                      label={$t(occ.task.done ? 'tasks.inCalendarOpen' : 'tasks.inCalendarDone').replace('{title}', occ.summary || $t('tasks.untitled'))}
                      disabled={occ.task.readOnly}
                      on:toggle={() => dispatch('toggleTask', occ)}
                    />
                  {/if}
                  {#if occ.conflict}<Ph svg={warningIcon} size={11} />{/if}
                  {occ.summary || $t('calendar.untitled')}
                </span>
                {#if short}
                  <span class="time inline">{formatTime(b.placed.start, timeFormat)}</span>
                {:else}
                  <span class="time">
                    {formatTimeRange(b.placed.start, b.placed.end, timeFormat)}
                    {#if occ.recurring}<Ph svg={repeatIcon} size={10} />{/if}
                  </span>
                  {#if occ.location}
                    <span class="where">{occ.location}</span>
                  {/if}
                {/if}
                {#if !occ.readOnly && !occ.task}
                  <!-- svelte-ignore a11y-no-static-element-interactions -->
                  <span class="grip" on:pointerdown={(e) => onResizeDown(e, b.placed)}></span>
                {/if}
              </button>
            {/each}

            {#each ghosts.filter((g) => g.col === i) as g, gi (gi)}
              <div class="ghost {g.kind}" style={`top:${pct(g.top)};height:${pct(Math.max(step, g.bottom - g.top))}`} data-pending={g.kind === 'new' ? '' : undefined}>
                <span>{g.label}</span>
              </div>
            {/each}

            {#if dropHint && dropHint.col === i}
              <div class="ghost drop" style={`top:${pct(dropHint.minutes)};height:${pct(clickMinutes)}`}>
                <span>{formatTime(timeAt(i, dropHint.minutes), timeFormat)}</span>
              </div>
            {/if}

            {#if sameDay(day, today)}
              <div class="now" style={`top:${pct(nowMinutes)}`} aria-hidden="true"></div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
</div>

<style>
  .tg {
    --gutter: var(--cal-gutter);
    --gap-end: var(--space-3);
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    user-select: none;
  }

  .tg.dual {
    --gutter: calc(2 * var(--cal-gutter));
  }

  .scroller {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
  }

  .row {
    display: grid;
    grid-template-columns: var(--gutter) minmax(0, 1fr);
  }

  .head {
    grid-template-columns: var(--gutter) repeat(var(--days), minmax(0, 1fr));
  }

  .top {
    position: sticky;
    top: 0;
    z-index: 3;
    background: var(--surface-raised);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .corner {
    display: flex;
    align-items: flex-end;
    justify-content: flex-end;
    gap: var(--space-2);
    padding-block-end: var(--space-2);
    padding-inline-end: var(--space-3);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .zones span {
    flex: 1;
    text-align: end;
  }

  .dh {
    display: flex;
    align-items: baseline;
    justify-content: center;
    gap: var(--space-2);
    min-width: 0;
    padding: var(--space-3) var(--space-2);
    border: none;
    border-inline-start: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    cursor: var(--cursor-action);
    overflow: hidden;
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .single .dh {
    justify-content: flex-start;
    padding-inline: var(--space-4);
  }

  .dh:hover {
    background: var(--surface-hover);
  }

  .wd {
    font-size: var(--fz-label);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    white-space: nowrap;
  }

  .num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: calc(var(--fz-heading) * 1.7);
    height: calc(var(--fz-heading) * 1.7);
    border-radius: var(--radius-control);
    font-size: var(--fz-heading);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    font-variant-numeric: tabular-nums;
  }

  .dh.today .wd {
    color: var(--accent);
  }

  .dh.today .num {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .lane {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .lane-label {
    align-items: center;
    padding-block: var(--space-2);
  }

  .lane-grid {
    position: relative;
    display: grid;
    grid-template-columns: repeat(var(--days), minmax(0, 1fr));
    grid-template-rows: repeat(var(--lanes), var(--cal-lane-height));
    row-gap: var(--space-1);
    padding-block: var(--space-2);
    max-height: calc(4 * (var(--cal-lane-height) + var(--space-1)) + 2 * var(--space-2));
    overflow-y: auto;
  }

  .lane-cell {
    grid-row: 1 / -1;
    margin-block: calc(-1 * var(--space-2));
    border-inline-start: var(--hairline) solid var(--border-subtle);
  }

  .bar {
    position: relative;
    z-index: 1;
    display: flex;
    align-items: center;
    min-width: 0;
    margin-inline: var(--space-1);
    padding: 0 var(--space-3);
    border: none;
    border-inline-start: var(--cal-edge) solid var(--ev);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--ev) 22%, var(--surface-raised));
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .bar:hover,
  .bar.selected {
    background: color-mix(in srgb, var(--ev) 34%, var(--surface-raised));
  }

  .bar.before {
    border-start-start-radius: 0;
    border-end-start-radius: 0;
    margin-inline-start: 0;
  }

  .bar.after {
    border-start-end-radius: 0;
    border-end-end-radius: 0;
    margin-inline-end: 0;
  }

  .bar.ghost {
    border: var(--hairline) dashed var(--accent);
    background: var(--selection-bg);
    pointer-events: none;
  }

  .title {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: var(--fw-medium);
  }

  .body {
    position: relative;
  }

  .gutter {
    display: flex;
    position: relative;
  }

  .gutter > div {
    position: relative;
    flex: 1;
  }

  .alt {
    color: var(--text-tertiary);
  }

  .hour {
    position: absolute;
    inset-inline-end: var(--space-3);
    transform: translateY(-50%);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .cols {
    position: relative;
    display: grid;
    grid-template-columns: repeat(var(--days), minmax(0, 1fr));
  }

  .lines {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }

  .line {
    position: absolute;
    inset-inline: 0;
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .line.half {
    border-top-style: dotted;
    opacity: 0.6;
  }

  .col {
    position: relative;
    border-inline-start: var(--hairline) solid var(--border-subtle);
    cursor: var(--cursor-action);
  }

  .off {
    position: absolute;
    inset-inline: 0;
    background: var(--surface-sunken);
    opacity: 0.55;
    pointer-events: none;
  }

  .off.weekend {
    opacity: 0.3;
  }

  .ev {
    position: absolute;
    z-index: 1;
    inset-inline-start: calc(var(--col) * (100% - var(--gap-end)) / var(--cols));
    width: calc((100% - var(--gap-end)) / var(--cols) - var(--space-1));
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-1);
    min-height: 0;
    padding: var(--space-1) var(--space-3);
    border: none;
    border-inline-start: var(--cal-edge) solid var(--ev);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--ev) 18%, var(--surface-raised));
    box-shadow: 0 0 0 var(--hairline) var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    line-height: 1.3;
    text-align: start;
    overflow: hidden;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out);
  }

  .ev.short {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
    padding-block: 0;
  }

  .ev:hover {
    background: color-mix(in srgb, var(--ev) 28%, var(--surface-raised));
  }

  .ev.selected {
    background: color-mix(in srgb, var(--ev) 32%, var(--surface-raised));
    box-shadow: 0 0 0 1px var(--ev);
    z-index: 2;
  }

  .ev.dragging,
  .bar.dragging {
    opacity: 0.35;
  }

  .ev.tentative {
    border-inline-start-style: dashed;
  }

  .ev.free {
    background: var(--surface-raised);
    box-shadow: inset 0 0 0 var(--hairline) color-mix(in srgb, var(--ev) 60%, transparent);
  }

  /* a task: an outline instead of a fill and its checkbox in front, quieter
     than the events around it. */
  .bar.task,
  .ev.task {
    gap: var(--space-2);
    border-inline-start: none;
    background: var(--surface-raised);
    box-shadow: inset 0 0 0 var(--hairline) var(--border-default);
    color: var(--text-secondary);
  }

  .bar.task:hover,
  .ev.task:hover {
    background: var(--surface-hover);
  }

  .bar.task .title,
  .ev.task .title {
    gap: var(--space-2);
    font-weight: var(--fw-regular);
  }

  .bar.task.done .title,
  .ev.task.done .title {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .ev.cancelled .title,
  .bar.cancelled .title {
    text-decoration: line-through;
    opacity: 0.7;
  }

  .ev:focus-visible,
  .bar:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .time,
  .where {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .time.inline {
    flex-shrink: 0;
  }

  .grip {
    position: absolute;
    inset-inline: 0;
    bottom: 0;
    height: var(--space-3);
    cursor: ns-resize;
  }

  .ghost {
    position: absolute;
    z-index: 2;
    inset-inline: 0 var(--gap-end);
    display: flex;
    align-items: flex-start;
    padding: var(--space-1) var(--space-3);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    pointer-events: none;
    animation: ghost-in var(--motion-fast) var(--ease-out);
  }

  .ghost.new {
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: var(--shadow-overlay);
  }

  .ghost.move {
    border: 1px solid var(--accent);
    background: var(--selection-bg-strong);
    color: var(--text-primary);
  }

  .ghost.drop {
    border: 1px dashed var(--accent);
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  @keyframes ghost-in {
    from {
      opacity: 0;
    }
  }

  .now {
    position: absolute;
    z-index: 2;
    inset-inline: 0;
    height: 0;
    border-top: 1.5px solid var(--accent);
    pointer-events: none;
  }

  .now::before {
    content: '';
    position: absolute;
    top: calc(-1 * var(--space-2) + 0.25px);
    inset-inline-start: calc(-1 * var(--space-2));
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--accent);
  }
</style>
