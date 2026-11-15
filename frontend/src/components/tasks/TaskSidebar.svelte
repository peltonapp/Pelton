<script lang="ts">
  // the task sidebar: the smart lists every install has, with how many tasks
  // each holds, then the task lists grouped by where they live, each with its
  // color and a menu for the rest. a task dragged onto an entry moves there:
  // to another list, to Today, out of its date, or into Completed.
  import { createEventDispatcher, tick } from 'svelte'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import dotsIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import plugIcon from '@phosphor-icons/core/assets/duotone/plugs-connected-duotone.svg?raw'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import sunIcon from '@phosphor-icons/core/assets/duotone/sun-duotone.svg?raw'
  import upcomingIcon from '@phosphor-icons/core/assets/duotone/calendar-dots-duotone.svg?raw'
  import overdueIcon from '@phosphor-icons/core/assets/duotone/hourglass-duotone.svg?raw'
  import nodateIcon from '@phosphor-icons/core/assets/duotone/circle-dashed-duotone.svg?raw'
  import doneIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import ColorSwatches from '../calendar/ColorSwatches.svelte'
  import { taskLists, tasks, loadTasks, taskClock } from '../../stores/tasks'
  import { recolorCalendar } from '../../stores/calendar'
  import { createTaskList, deleteTaskList, exportCalendar, renameCalendar } from '../../lib/api'
  import { askConfirm } from '../../stores/confirm'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { calendarColor, paletteHex, paletteSize } from '../../lib/calformat'
  import { inSmartList, smartLists, type SmartList, type TaskScope } from '../../lib/tasklogic'
  import { t } from '../../lib/i18n'
  import type { TaskList } from '../../lib/types'

  /** What the list shows. */
  export let scope: TaskScope

  const dispatch = createEventDispatcher<{ scope: TaskScope }>()

  const smartIcons: Record<SmartList, string> = {
    today: sunIcon,
    upcoming: upcomingIcon,
    overdue: overdueIcon,
    nodate: nodateIcon,
    completed: doneIcon,
  }

  $: counts = Object.fromEntries(smartLists.map((k) => [k, $tasks.filter((x) => inSmartList(x, k, $taskClock)).length])) as Record<SmartList, number>
  $: openIn = (id: number): number => $tasks.filter((x) => x.listId === id && !x.done).length

  type Group = { key: string; label: string; error: string; items: TaskList[] }

  $: groups = groupLists($taskLists, $t('calendar.onThisDevice'))

  function groupLists(list: TaskList[], localLabel: string): Group[] {
    const out: Group[] = []
    const local = list.filter((l) => l.local)
    if (local.length) {
      out.push({ key: 'local', label: localLabel, error: '', items: local })
    }
    const accounts = [...new Set(list.filter((l) => !l.local).map((l) => l.accountId))]
    for (const id of accounts) {
      const items = list.filter((l) => l.accountId === id)
      out.push({ key: `a${id}`, label: items[0].accountName, error: items.find((l) => l.lastError)?.lastError ?? '', items })
    }
    return out
  }

  function nameOf(list: TaskList): string {
    return list.name || $t('tasks.defaultList')
  }

  function isOn(next: TaskScope): boolean {
    return next.kind === scope.kind && (next.kind === 'smart' ? scope.kind === 'smart' && scope.key === next.key : scope.kind === 'list' && scope.id === next.id)
  }

  // the menu of one list, and the rename form it can turn into.
  let menuFor: TaskList | null = null
  let menuAnchor: HTMLElement | null = null
  let renaming = ''
  let renameMode = false

  function openMenu(list: TaskList, event: MouseEvent): void {
    menuFor = list
    menuAnchor = event.currentTarget as HTMLElement
    renameMode = false
    renaming = nameOf(list)
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
    taskLists.update((list) => list.map((l) => (l.id === id ? { ...l, color } : l)))
    menuFor = { ...menuFor, color }
    await recolorCalendar(id, color)
  }

  async function withList(run: (list: TaskList) => Promise<void>): Promise<void> {
    if (!menuFor) {
      return
    }
    const list = menuFor
    closeMenu()
    try {
      await run(list)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function rename(): Promise<void> {
    const name = renaming.trim()
    await withList(async (list) => {
      if (name) {
        await renameCalendar(list.id, name)
        await loadTasks()
      }
    })
  }

  const remove = (): Promise<void> =>
    withList(async (list) => {
      const ok = await askConfirm({
        title: $t('tasks.deleteList.title').replace('{name}', nameOf(list)),
        body: $t('tasks.deleteList.message'),
        confirmLabel: $t('tasks.deleteList.confirm'),
        danger: true,
      })
      if (!ok) {
        return
      }
      await deleteTaskList(list.id)
      if (scope.kind === 'list' && scope.id === list.id) {
        dispatch('scope', { kind: 'smart', key: 'today' })
      }
      await loadTasks()
    })

  const runExport = (format: 'ics' | 'jscal'): Promise<void> =>
    withList(async (list) => {
      if (await exportCalendar(list.id, format)) {
        toastSuccess($t('calendar.exported'))
      }
    })

  // adding a local list.
  let adding = false
  let addAnchor: HTMLElement
  let newName = ''
  let newColor = 0
  let nameInput: HTMLInputElement

  async function openAdd(): Promise<void> {
    adding = true
    newName = ''
    newColor = $taskLists.length % paletteSize
    await tick()
    nameInput?.focus()
  }

  async function add(): Promise<void> {
    if (!newName.trim()) {
      return
    }
    try {
      const made = await createTaskList(newName.trim(), paletteHex(newColor))
      adding = false
      await loadTasks()
      dispatch('scope', { kind: 'list', id: made.id })
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<aside class="side" aria-label={$t('tasks.sidebar')}>
  <ul class="smart" role="list">
    {#each smartLists as key (key)}
      <li>
        <button
          type="button"
          class="entry"
          class:on={isOn({ kind: 'smart', key })}
          class:alert={key === 'overdue' && counts.overdue > 0}
          aria-current={isOn({ kind: 'smart', key }) ? 'page' : undefined}
          data-task-drop={key === 'upcoming' || key === 'overdue' ? undefined : `smart:${key}`}
          on:click={() => dispatch('scope', { kind: 'smart', key })}
        >
          <Ph svg={smartIcons[key]} size={16} />
          <span class="name">{$t(`tasks.smart.${key}`)}</span>
          {#if counts[key] > 0 && key !== 'completed'}
            <span class="count">{counts[key]}</span>
          {/if}
        </button>
      </li>
    {/each}
  </ul>

  <div class="lists">
    {#each groups as group (group.key)}
      <section class="group">
        <h3>
          <span class="label">{group.label}</span>
          {#if group.error}
            <span class="warn" title={group.error}><Ph svg={warningIcon} size={13} /></span>
          {/if}
        </h3>
        <ul role="list">
          {#each group.items as list (list.id)}
            <li class="row" class:on={isOn({ kind: 'list', id: list.id })} style={`--cal:${calendarColor(list)}`}>
              <button
                type="button"
                class="entry"
                aria-current={isOn({ kind: 'list', id: list.id }) ? 'page' : undefined}
                data-task-drop={list.readOnly ? undefined : `list:${list.id}`}
                on:click={() => dispatch('scope', { kind: 'list', id: list.id })}
              >
                <span class="swatch" aria-hidden="true"></span>
                <span class="name">{nameOf(list)}</span>
                {#if list.readOnly}
                  <span class="ro" title={$t('calendar.readOnly')}><Ph svg={lockIcon} size={12} /></span>
                {/if}
                {#if openIn(list.id) > 0}
                  <span class="count">{openIn(list.id)}</span>
                {/if}
              </button>
              <button
                type="button"
                class="cal-icon more"
                class:open={menuFor?.id === list.id}
                aria-label={$t('tasks.listMenu').replace('{name}', nameOf(list))}
                aria-haspopup="dialog"
                on:click={(e) => openMenu(list, e)}
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
      <span>{$t('tasks.addList')}</span>
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
          <input class="cal-field" bind:value={renaming} aria-label={$t('tasks.listName')} autofocus />
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
        <button type="button" class="cal-item" on:click={() => runExport('ics')}>{$t('calendar.exportIcs')}</button>
        <button type="button" class="cal-item" on:click={() => runExport('jscal')}>{$t('calendar.exportJscal')}</button>
        {#if menuFor.local}
          <button type="button" class="cal-item danger" on:click={remove}>{$t('tasks.deleteList')}</button>
        {/if}
      {/if}
    </div>
  </AnchoredPopover>
{/if}

{#if adding && addAnchor}
  <AnchoredPopover anchor={addAnchor} side="above" align="start" label={$t('tasks.addList')} on:close={() => (adding = false)}>
    <form class="cal-menu add" on:submit|preventDefault={add}>
      <input class="cal-field" bind:this={nameInput} bind:value={newName} placeholder={$t('tasks.listName')} aria-label={$t('tasks.listName')} />
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

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .smart {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .entry {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    width: 100%;
    height: calc(var(--control-height) - 2px);
    padding-inline: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .smart .entry {
    color: var(--text-secondary);
  }

  .entry:hover,
  .row:hover {
    background: var(--surface-hover);
  }

  .row:hover .entry {
    background: transparent;
  }

  .entry.on,
  .row.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .entry:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .entry:global(.task-drop-over) {
    background: var(--selection-bg-strong);
  }

  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .entry.alert .count {
    color: var(--danger);
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

  .row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-control);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .swatch {
    flex-shrink: 0;
    width: var(--space-3);
    height: var(--space-3);
    border-radius: 50%;
    background: var(--cal);
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
