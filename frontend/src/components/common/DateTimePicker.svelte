<script lang="ts">
  // a friendlier drop-in replacement for native `<input type="date">` /
  // `<input type="datetime-local">`: a trigger button showing the current
  // value that opens a month-grid calendar (plus an hour/minute control in
  // datetime mode). all date math here is local wall-clock time, matching
  // what the native inputs produced, so callers keep their existing
  // parsing/formatting untouched.
  import { formatDateTimeMedium, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { tick, createEventDispatcher, onDestroy } from 'svelte'
  import { popPopup, pushPopup } from '../../lib/popups'
  import { IconCalendar, IconChevronLeft, IconChevronRight } from '@tabler/icons-svelte'
  import { currentUIScale } from '../../theme/theme'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ confirm: void }>()

  // the bound value: 'YYYY-MM-DD' in date mode, 'YYYY-MM-DDTHH:mm' in
  // datetime mode (empty string means unset), same shape the native inputs
  // used so existing parsing at call sites keeps working unchanged.
  export let value = ''
  // 'date' shows just the calendar; 'datetime' adds the hour/minute control.
  export let mode: 'date' | 'datetime' = 'date'
  // forwarded to the trigger button so an external <label for=...> still works.
  export let id: string | undefined = undefined
  // when set, a primary confirm button (e.g. "Schedule") is shown inside the
  // panel; clicking it commits the value, closes the panel and dispatches
  // `confirm`. only the compose send-later menu passes this, so the plain
  // date/settings pickers keep their simple footer.
  export let confirmLabel: string | undefined = undefined

  let open = false
  let triggerEl: HTMLButtonElement
  let panelEl: HTMLDivElement
  let panelLeft = 0
  let panelTop = 0
  // 'days' is the normal month grid; 'years' is a scrollable year list opened
  // by clicking the month/year label, for jumping far without repeated
  // prev/next clicks (e.g. birthdays, old date ranges).
  let view: 'days' | 'years' = 'days'
  let yearListEl: HTMLDivElement

  const today = new Date()
  // bounds for the scrollable year list: a century back, a couple decades
  // forward. generous enough for any real date without scrolling forever.
  const YEAR_MIN = today.getFullYear() - 100
  const YEAR_MAX = today.getFullYear() + 20

  type YMD = { y: number; m: number; d: number }

  let selected: YMD | null = null
  let hour = today.getHours()
  let minute = today.getMinutes()
  let viewYear = today.getFullYear()
  let viewMonth = today.getMonth()
  let focused: YMD = { y: viewYear, m: viewMonth, d: today.getDate() }
  let dayButtons: Record<string, HTMLButtonElement> = {}

  // guards against our own commit() round-tripping back through the
  // `$: syncFromValue(value)` reactive block and clobbering in-progress edits.
  let lastEmitted: string | null = null

  $: syncFromValue(value)
  function syncFromValue(v: string): void {
    if (v === lastEmitted) {
      return
    }
    if (!v) {
      selected = null
      return
    }
    if (mode === 'datetime') {
      const parsed = parseDateTime(v)
      if (!parsed) {
        return
      }
      selected = { y: parsed.y, m: parsed.m, d: parsed.d }
      hour = parsed.h
      minute = parsed.min
    } else {
      const parsed = parseDate(v)
      if (!parsed) {
        return
      }
      selected = parsed
    }
    viewYear = selected.y
    viewMonth = selected.m
    focused = { ...selected }
  }

  function pad(n: number): string {
    return String(n).padStart(2, '0')
  }

  function parseDate(s: string): YMD | null {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(s)
    return m ? { y: Number(m[1]), m: Number(m[2]) - 1, d: Number(m[3]) } : null
  }

  function parseDateTime(s: string): (YMD & { h: number; min: number }) | null {
    const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(s)
    return m
      ? { y: Number(m[1]), m: Number(m[2]) - 1, d: Number(m[3]), h: Number(m[4]), min: Number(m[5]) }
      : null
  }

  function fmtDate(ymd: YMD): string {
    return `${ymd.y}-${pad(ymd.m + 1)}-${pad(ymd.d)}`
  }

  function addDays(ymd: YMD, delta: number): YMD {
    const d = new Date(ymd.y, ymd.m, ymd.d + delta)
    return { y: d.getFullYear(), m: d.getMonth(), d: d.getDate() }
  }

  function sameDay(a: YMD, b: YMD): boolean {
    return a.y === b.y && a.m === b.m && a.d === b.d
  }

  function isToday(ymd: YMD): boolean {
    return sameDay(ymd, { y: today.getFullYear(), m: today.getMonth(), d: today.getDate() })
  }

  function cellKey(ymd: YMD): string {
    return `${ymd.y}-${ymd.m}-${ymd.d}`
  }

  // 6x7 grid: the weeks that cover the visible month, monday-first. cells
  // outside the visible month are shown dimmed and not interactive; keyboard
  // navigation still crosses into them via plain date arithmetic.
  $: cells = buildCells(viewYear, viewMonth)
  function buildCells(y: number, m: number): (YMD & { inMonth: boolean })[] {
    const firstWeekday = (new Date(y, m, 1).getDay() + 6) % 7 // 0 = monday
    const start: YMD = addDays({ y, m, d: 1 }, -firstWeekday)
    const out: (YMD & { inMonth: boolean })[] = []
    for (let i = 0; i < 42; i++) {
      const c = addDays(start, i)
      out.push({ ...c, inMonth: c.m === m && c.y === y })
    }
    return out
  }

  $: monthLabel = new Intl.DateTimeFormat(undefined, { month: 'long', year: 'numeric' }).format(
    new Date(viewYear, viewMonth, 1),
  )

  const years = Array.from({ length: YEAR_MAX - YEAR_MIN + 1 }, (_, i) => YEAR_MIN + i)

  async function openYearView(): Promise<void> {
    view = 'years'
    await positionPanel()
    yearListEl?.querySelector('.year.current')?.scrollIntoView({ block: 'center' })
  }

  function selectYear(y: number): void {
    viewYear = y
    view = 'days'
    void positionPanel()
  }

  // positionPanel computes the panel's on-screen position from the trigger's
  // and panel's own (already-rendered) rects, clamped to the viewport. the
  // panel is `position: fixed` and placed by explicit coordinates rather than
  // css anchoring so an ancestor with `overflow: hidden`/`auto` (settings
  // pages, dropdown menus) never clips it - the same fix already used for the
  // compose window's send-later menu.
  //
  // the app applies an interface zoom via css `zoom` on <html> (see
  // ContextMenu.svelte). under zoom, getBoundingClientRect position (like
  // pointer clientX/Y) stays in unscaled screen pixels while a `position:
  // fixed` element is placed in the zoomed layout space, so raw rect values
  // land the panel in the wrong place - dividing by the scale converts them
  // (a no-op at 100%). offsetWidth/Height are already layout-space and must
  // not be divided.
  async function positionPanel(): Promise<void> {
    await tick()
    if (!triggerEl || !panelEl) {
      return
    }
    const scale = currentUIScale()
    const margin = 8
    const triggerRect = triggerEl.getBoundingClientRect()
    const triggerLeft = triggerRect.left / scale
    const triggerBottom = triggerRect.bottom / scale
    const panelW = panelEl.offsetWidth
    const panelH = panelEl.offsetHeight
    const vw = window.innerWidth / scale
    const vh = window.innerHeight / scale
    const maxLeft = vw - panelW - margin
    const maxTop = vh - panelH - margin
    const wantLeft = Math.min(Math.max(triggerLeft, margin), Math.max(margin, maxLeft))
    const wantTop = Math.min(Math.max(triggerBottom + 4, margin), Math.max(margin, maxTop))
    panelLeft = wantLeft
    panelTop = wantTop

    // `position: fixed` is only relative to the viewport while no ancestor has
    // a transform. Modal and the snooze dialog both centre with
    // translate(-50%, -50%), which makes the dialog the containing block, so
    // the coordinates above get re-anchored to its top-left corner and the
    // panel lands half a dialog away - off screen entirely in the snooze
    // dialog (#170 follow-up). Rather than forbid transforms on every dialog
    // that might ever hold a picker, measure where the panel actually ended up
    // and shift it by the difference. The correction is zero when there is no
    // transformed ancestor, so the common case is unchanged.
    await tick()
    const placed = panelEl.getBoundingClientRect()
    const offLeft = placed.left / scale - wantLeft
    const offTop = placed.top / scale - wantTop
    if (Math.abs(offLeft) > 0.5 || Math.abs(offTop) > 0.5) {
      panelLeft = wantLeft - offLeft
      panelTop = wantTop - offTop
    }
  }

  // weekday header letters, monday-first; the reference week (2023-01-02 was
  // a monday) is arbitrary, only its weekdays matter.
  const weekdayLabels = (() => {
    const fmt = new Intl.DateTimeFormat(undefined, { weekday: 'narrow' })
    return Array.from({ length: 7 }, (_, i) => fmt.format(new Date(2023, 0, 2 + i)))
  })()

  $: displayText = formatDisplay(selected, hour, minute)
  function formatDisplay(sel: YMD | null, h: number, min: number): string {
    if (!sel) {
      return ''
    }
    const d = new Date(sel.y, sel.m, sel.d, h, min)
    return formatDateTimeMedium(d, mode === 'datetime', $prefs.timeFormat as TimeFormat)
  }

  function commit(): void {
    if (!selected) {
      return
    }
    const next = mode === 'datetime' ? `${fmtDate(selected)}T${pad(hour)}:${pad(minute)}` : fmtDate(selected)
    lastEmitted = next
    value = next
  }

  function selectDay(ymd: YMD): void {
    selected = { ...ymd }
    focused = { ...ymd }
    viewYear = ymd.y
    viewMonth = ymd.m
    view = 'days'
    commit()
    if (mode === 'date') {
      open = false
    }
  }

  function pickToday(): void {
    selectDay({ y: today.getFullYear(), m: today.getMonth(), d: today.getDate() })
  }

  function clear(): void {
    selected = null
    lastEmitted = ''
    value = ''
    open = false
  }

  function onHourChange(): void {
    hour = Math.min(23, Math.max(0, hour || 0))
    if (!selected) {
      selected = { ...focused }
    }
    commit()
  }

  function onMinuteChange(): void {
    minute = Math.min(59, Math.max(0, minute || 0))
    if (!selected) {
      selected = { ...focused }
    }
    commit()
  }

  async function moveFocus(deltaDays: number): Promise<void> {
    focused = addDays(focused, deltaDays)
    viewYear = focused.y
    viewMonth = focused.m
    await tick()
    dayButtons[cellKey(focused)]?.focus()
  }

  function onGridKeydown(event: KeyboardEvent): void {
    switch (event.key) {
      case 'ArrowLeft':
        event.preventDefault()
        void moveFocus(-1)
        break
      case 'ArrowRight':
        event.preventDefault()
        void moveFocus(1)
        break
      case 'ArrowUp':
        event.preventDefault()
        void moveFocus(-7)
        break
      case 'ArrowDown':
        event.preventDefault()
        void moveFocus(7)
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        selectDay(focused)
        break
      case 'Escape':
        // the panel's own handler takes it
        break
    }
  }

  function prevMonth(): void {
    const d = new Date(viewYear, viewMonth - 1, 1)
    viewYear = d.getFullYear()
    viewMonth = d.getMonth()
  }

  function nextMonth(): void {
    const d = new Date(viewYear, viewMonth + 1, 1)
    viewYear = d.getFullYear()
    viewMonth = d.getMonth()
  }

  function toggle(): void {
    if (open) {
      close()
    } else {
      open = true
      view = 'days'
      void positionPanel()
    }
  }

  // the open panel registers as a popup, so the dialog around it leaves
  // Escape to it instead of closing itself and throwing the edits away.
  let registered = false
  $: syncPopup(open)
  function syncPopup(isOpen: boolean): void {
    if (isOpen && !registered) {
      pushPopup()
      registered = true
    } else if (!isOpen && registered) {
      popPopup()
      registered = false
    }
  }
  onDestroy(() => syncPopup(false))

  // Escape anywhere in the panel closes only the panel, wherever the focus
  // sits: on the month arrows and the time fields as much as in the day grid.
  function onPanelKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      close()
    }
  }

  function close(): void {
    open = false
    triggerEl?.focus()
  }

  function confirm(): void {
    commit()
    open = false
    dispatch('confirm')
  }
</script>

<div class="picker">
  <button
    type="button"
    class="trigger"
    {id}
    bind:this={triggerEl}
    aria-haspopup="true"
    aria-expanded={open}
    on:click={toggle}
  >
    <span class="value" class:placeholder={!displayText}>
      {displayText || $t('common.datePicker.selectDate')}
    </span>
    <IconCalendar size={15} stroke={1.7} />
  </button>

  {#if open}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" on:click={close}></div>
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div class="panel" bind:this={panelEl} style={`left:${panelLeft}px; top:${panelTop}px`} on:keydown={onPanelKeydown}>
      <div class="nav">
        <button
          type="button"
          class="nav-btn"
          aria-label={$t('common.datePicker.prevMonth')}
          disabled={view === 'years'}
          on:click={prevMonth}
        >
          <IconChevronLeft size={15} stroke={1.8} />
        </button>
        <button
          type="button"
          class="month-label"
          aria-label={$t('common.datePicker.selectYear')}
          on:click={openYearView}
        >
          {monthLabel}
        </button>
        <button
          type="button"
          class="nav-btn"
          aria-label={$t('common.datePicker.nextMonth')}
          disabled={view === 'years'}
          on:click={nextMonth}
        >
          <IconChevronRight size={15} stroke={1.8} />
        </button>
      </div>

      {#if view === 'years'}
        <div class="year-list" bind:this={yearListEl}>
          {#each years as y (y)}
            <button
              type="button"
              class="year"
              class:current={y === viewYear}
              class:today={y === today.getFullYear()}
              on:click={() => selectYear(y)}
            >
              {y}
            </button>
          {/each}
        </div>
      {:else}
        <div class="weekdays">
          {#each weekdayLabels as w, i (i)}
            <span>{w}</span>
          {/each}
        </div>

        <!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
        <div class="grid" role="group" tabindex="-1" on:keydown={onGridKeydown}>
          {#each cells as cell (cellKey(cell))}
            {#if cell.inMonth}
              <button
                type="button"
                class="day"
                class:selected={selected && sameDay(selected, cell)}
                class:today={isToday(cell)}
                tabindex={sameDay(focused, cell) ? 0 : -1}
                bind:this={dayButtons[cellKey(cell)]}
                on:click={() => selectDay(cell)}
                on:focus={() => (focused = cell)}
              >
                {cell.d}
              </button>
            {:else}
              <span class="day pad">{cell.d}</span>
            {/if}
          {/each}
        </div>
      {/if}

      {#if mode === 'datetime'}
        <div class="time">
          <input
            type="number"
            class="time-field"
            min="0"
            max="23"
            aria-label={$t('common.datePicker.hour')}
            bind:value={hour}
            on:change={onHourChange}
          />
          <span class="sep">:</span>
          <input
            type="number"
            class="time-field"
            min="0"
            max="59"
            aria-label={$t('common.datePicker.minute')}
            bind:value={minute}
            on:change={onMinuteChange}
          />
        </div>
      {/if}

      <div class="footer">
        <button type="button" class="link-btn" on:click={clear}>{$t('common.datePicker.clear')}</button>
        <button type="button" class="link-btn" on:click={pickToday}>{$t('common.datePicker.today')}</button>
      </div>

      {#if confirmLabel}
        <button type="button" class="confirm-btn" disabled={!selected} on:click={confirm}>
          {confirmLabel}
        </button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .picker {
    position: relative;
    display: inline-block;
  }

  .trigger {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    font-size: var(--fz-list);
    width: 100%;
  }

  .trigger:hover {
    background: var(--surface-hover);
  }

  .trigger .value {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .trigger .value.placeholder {
    color: var(--text-tertiary);
  }

  .trigger :global(svg) {
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .scrim {
    position: fixed;
    inset: 0;
    z-index: 300;
  }

  .panel {
    position: fixed;
    z-index: 301;
    width: 240px;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .nav {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .nav-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  .nav-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .nav-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .month-label {
    border: none;
    background: transparent;
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
  }

  .month-label:hover {
    background: var(--surface-hover);
  }

  .year-list {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: var(--space-1);
    max-height: 200px;
    overflow-y: auto;
  }

  .year {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-2) 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  .year:hover {
    background: var(--surface-hover);
  }

  .year.today {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .year.current {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .weekdays,
  .grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
  }

  .weekdays span {
    text-align: center;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .day {
    display: flex;
    align-items: center;
    justify-content: center;
    aspect-ratio: 1;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  .day.pad {
    color: var(--text-tertiary);
    opacity: 0.4;
    cursor: default;
  }

  .day:not(.pad):hover {
    background: var(--surface-hover);
  }

  .day.today {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }

  .day.selected {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .day:focus-visible {
    outline: 1px solid var(--accent);
    outline-offset: 1px;
  }

  .time {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-1);
  }

  .time-field {
    width: 40px;
    height: var(--control-height);
    text-align: center;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-list);
  }

  .time-field:focus {
    border-color: var(--accent);
    outline: none;
  }

  .sep {
    color: var(--text-tertiary);
  }

  .footer {
    display: flex;
    justify-content: space-between;
    padding-top: var(--space-1);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .link-btn {
    border: none;
    background: transparent;
    color: var(--link);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
  }

  .link-btn:hover {
    background: var(--surface-hover);
  }

  .confirm-btn {
    width: 100%;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }

  .confirm-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
