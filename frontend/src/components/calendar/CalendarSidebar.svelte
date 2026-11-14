<script lang="ts">
  // the calendar's sidebar: a button for a new event, a small month to jump
  // around with, and the calendars grouped by where they live, each with a
  // colored box that shows or hides its events and a menu for the rest.
  import { createEventDispatcher, tick } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import dotsIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import plugIcon from '@phosphor-icons/core/assets/duotone/plugs-connected-duotone.svg?raw'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import './controls.css'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import MiniMonth from './MiniMonth.svelte'
  import ColorSwatches from './ColorSwatches.svelte'
  import { calendars, calendarAccounts, toggleCalendar, recolorCalendar, loadCalendars, loadOccurrences } from '../../stores/calendar'
  import importIcon from '@phosphor-icons/core/assets/duotone/file-arrow-down-duotone.svg?raw'
  import { createLocalCalendar, renameCalendar, deleteLocalCalendar, exportCalendar } from '../../lib/api'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { calendarColor, paletteHex, paletteSize } from '../../lib/calformat'
  import { t } from '../../lib/i18n'
  import type { Calendar, CalendarAccount } from '../../lib/types'

  /** The day the calendar is on. */
  export let focus: Date
  /** First day of the week. */
  export let weekStart: number
  /** The span the view shows, marked in the small month. */
  export let rangeFrom: Date
  export let rangeTo: Date
  /** Days with events, as yyyy-mm-dd. */
  export let busy: Set<string>

  // import names the calendar to import into, 0 to let the import ask.
  const dispatch = createEventDispatcher<{ pick: Date; create: void; import: number }>()

  type Group = { key: string; label: string; error: string; items: Calendar[] }

  $: groups = groupCalendars($calendars, $calendarAccounts, $t('calendar.onThisDevice'))

  function groupCalendars(list: Calendar[], accounts: CalendarAccount[], localLabel: string): Group[] {
    const out: Group[] = []
    const local = list.filter((c) => c.local)
    if (local.length) {
      out.push({ key: 'local', label: localLabel, error: '', items: local })
    }
    for (const acct of accounts) {
      const items = list.filter((c) => c.accountId === acct.id)
      out.push({ key: `a${acct.id}`, label: acct.name || acct.username, error: acct.lastError, items })
    }
    return out
  }

  function nameOf(cal: Calendar): string {
    return cal.name || $t('calendar.defaultName')
  }

  // the menu of one calendar, and the rename form it can turn into.
  let menuFor: Calendar | null = null
  let menuAnchor: HTMLElement | null = null
  let renaming = ''
  let renameMode = false

  function openMenu(cal: Calendar, event: MouseEvent): void {
    menuFor = cal
    menuAnchor = event.currentTarget as HTMLElement
    renameMode = false
    renaming = nameOf(cal)
  }

  function closeMenu(): void {
    menuFor = null
    menuAnchor = null
  }

  async function recolor(index: number): Promise<void> {
    if (!menuFor) {
      return
    }
    const id = menuFor.id
    const color = paletteHex(index)
    calendars.update((list) => list.map((c) => (c.id === id ? { ...c, color } : c)))
    menuFor = { ...menuFor, color }
    await recolorCalendar(id, color)
  }

  async function rename(): Promise<void> {
    if (!menuFor || !renaming.trim()) {
      return
    }
    try {
      await renameCalendar(menuFor.id, renaming.trim())
      closeMenu()
      await loadCalendars()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function withCalendar(run: (cal: Calendar) => Promise<void>): Promise<void> {
    if (!menuFor) {
      return
    }
    const cal = menuFor
    closeMenu()
    try {
      await run(cal)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  const remove = (): Promise<void> =>
    withCalendar(async (cal) => {
      await deleteLocalCalendar(cal.id)
      await Promise.all([loadCalendars(), loadOccurrences()])
    })

  const runImport = (): Promise<void> =>
    withCalendar(async (cal) => {
      dispatch('import', cal.id)
    })

  const runExport = (format: 'ics' | 'jscal'): Promise<void> =>
    withCalendar(async (cal) => {
      if (await exportCalendar(cal.id, format)) {
        toastSuccess($t('calendar.exported'))
      }
    })

  // adding a local calendar.
  let adding = false
  let addAnchor: HTMLElement
  let newName = ''
  let newColor = 0
  let nameInput: HTMLInputElement

  async function openAdd(): Promise<void> {
    adding = true
    newName = ''
    newColor = $calendars.length % paletteSize
    await tick()
    nameInput?.focus()
  }

  async function add(): Promise<void> {
    if (!newName.trim()) {
      return
    }
    try {
      await createLocalCalendar(newName.trim(), paletteHex(newColor))
      adding = false
      await loadCalendars()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<aside class="side" aria-label={$t('calendar.sidebar')}>
  <button type="button" class="cal-btn primary new" on:click={() => dispatch('create')}>
    <Ph svg={plusIcon} size={14} />
    <span>{$t('calendar.newEvent')}</span>
  </button>

  <MiniMonth value={focus} {weekStart} {rangeFrom} {rangeTo} {busy} on:pick={(e) => dispatch('pick', e.detail)} />

  <div class="lists">
    {#each groups as group (group.key)}
      <section class="group">
        <h3>
          <span class="label">{group.label}</span>
          {#if group.error}
            <span class="warn" title={group.error}><Ph svg={warningIcon} size={13} /></span>
          {/if}
        </h3>
        <ul>
          {#each group.items as cal (cal.id)}
            <li class="row" style={`--cal:${calendarColor(cal)}`}>
              <label class="toggle">
                <input type="checkbox" checked={cal.visible} on:change={(e) => toggleCalendar(cal.id, e.currentTarget.checked)} />
                <span class="box" aria-hidden="true">
                  <svg viewBox="0 0 12 12"><path d="M2.5 6.4 5 8.8 9.6 3.4" /></svg>
                </span>
                <span class="name">{nameOf(cal)}</span>
                {#if cal.readOnly}
                  <span class="ro" title={$t('calendar.readOnly')}><Ph svg={lockIcon} size={12} /></span>
                {/if}
              </label>
              <button
                type="button"
                class="cal-icon more"
                class:open={menuFor?.id === cal.id}
                aria-label={$t('calendar.calendarMenu').replace('{name}', nameOf(cal))}
                aria-haspopup="dialog"
                on:click={(e) => openMenu(cal, e)}
              >
                <Ph svg={dotsIcon} size={14} />
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/each}
  </div>

  <div class="foot">
    <button type="button" class="cal-item" bind:this={addAnchor} on:click={openAdd}>
      <Ph svg={plusIcon} size={13} />
      <span>{$t('calendar.addCalendar')}</span>
    </button>
    <button type="button" class="cal-item" on:click={() => dispatch('import', 0)}>
      <Ph svg={importIcon} size={13} />
      <span>{$t('calendar.importFile')}</span>
    </button>
    <button type="button" class="cal-item" on:click={() => openSettingsAt('calendar')}>
      <Ph svg={plugIcon} size={13} />
      <span>{$t('calendar.connectAccount')}</span>
    </button>
  </div>
</aside>

{#if menuFor && menuAnchor}
  <AnchoredPopover anchor={menuAnchor} align="start" label={nameOf(menuFor)} on:close={closeMenu}>
    <div class="cal-menu">
      {#if renameMode}
        <form class="rename" on:submit|preventDefault={rename}>
          <!-- svelte-ignore a11y-autofocus -->
          <input class="cal-field" bind:value={renaming} aria-label={$t('calendar.calendarName')} autofocus />
          <button type="submit" class="cal-btn primary">{$t('calendar.save')}</button>
        </form>
      {:else}
        <div class="swatches">
          <ColorSwatches value={menuFor.color} on:pick={(e) => recolor(e.detail)} />
        </div>
        <div class="cal-sep"></div>
        {#if menuFor.local}
          <button type="button" class="cal-item" on:click={() => (renameMode = true)}>{$t('calendar.rename')}</button>
        {/if}
        {#if !menuFor.readOnly}
          <button type="button" class="cal-item" on:click={runImport}>{$t('calendar.importFile')}</button>
        {/if}
        <button type="button" class="cal-item" on:click={() => runExport('ics')}>{$t('calendar.exportIcs')}</button>
        <button type="button" class="cal-item" on:click={() => runExport('jscal')}>{$t('calendar.exportJscal')}</button>
        {#if menuFor.local}
          <button type="button" class="cal-item danger" on:click={remove}>{$t('calendar.deleteCalendar')}</button>
        {/if}
      {/if}
    </div>
  </AnchoredPopover>
{/if}

{#if adding && addAnchor}
  <AnchoredPopover anchor={addAnchor} side="above" align="start" label={$t('calendar.addCalendar')} on:close={() => (adding = false)}>
    <form class="cal-menu add" on:submit|preventDefault={add}>
      <input
        class="cal-field"
        bind:this={nameInput}
        bind:value={newName}
        placeholder={$t('calendar.calendarName')}
        aria-label={$t('calendar.calendarName')}
      />
      <ColorSwatches index={newColor} on:pick={(e) => (newColor = e.detail)} />
      <div class="actions">
        <button type="button" class="cal-btn" on:click={() => (adding = false)}>{$t('calendar.cancel')}</button>
        <button type="submit" class="cal-btn primary" disabled={!newName.trim()}>{$t('calendar.add')}</button>
      </div>
    </form>
  </AnchoredPopover>
{/if}

<style>
  .side {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    width: var(--cal-sidebar-width);
    height: 100%;
    padding: var(--space-4) var(--space-4) var(--space-3);
    overflow-y: auto;
  }

  .new {
    height: var(--control-height);
    font-size: var(--fz-list);
  }

  .lists {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    flex: 1;
  }

  h3 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-2);
    padding-inline-start: var(--space-2);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .warn {
    display: inline-flex;
    color: var(--warning);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-control);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .toggle {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    height: calc(var(--control-height) - 2px);
    padding-inline: var(--space-2);
    cursor: var(--cursor-action);
    font-size: var(--fz-list);
    color: var(--text-primary);
  }

  .toggle input {
    position: absolute;
    opacity: 0;
    pointer-events: none;
  }

  .box {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--space-5);
    height: var(--space-5);
    border: 1.5px solid var(--cal);
    border-radius: calc(var(--radius-control) - 1px);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .box svg {
    width: var(--space-4);
    height: var(--space-4);
  }

  .box path {
    fill: none;
    stroke: var(--text-inverse);
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 12;
    stroke-dashoffset: 12;
    transition: stroke-dashoffset var(--motion-base) var(--ease-out);
  }

  .toggle input:checked + .box {
    background: var(--cal);
  }

  .toggle input:checked + .box path {
    stroke-dashoffset: 0;
  }

  .toggle input:focus-visible + .box {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ro {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .more {
    margin-inline-end: var(--space-1);
    opacity: 0;
  }

  .row:hover .more,
  .more:focus-visible,
  .more.open {
    opacity: 1;
  }

  .foot {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .foot .cal-item {
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .foot .cal-item:hover {
    color: var(--text-primary);
  }

  .swatches {
    padding: var(--space-2);
  }

  .add {
    gap: var(--space-3);
    padding: var(--space-3);
  }

  .rename {
    display: flex;
    gap: var(--space-2);
    padding: var(--space-1);
  }

  .rename .cal-field {
    flex: 1;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
