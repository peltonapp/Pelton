<script lang="ts">
  // the detail pane of one task: its title and checkbox, then hairline rows
  // led by an icon, as in the event editor: when it is due, when it starts,
  // how it repeats, reminders, priority and progress, the list it is in, its
  // subtasks, the people it is for, the mail and events it links to, a web
  // link, tags and notes. every change is saved as it is made; text saves when
  // the field is left or Enter is pressed. the "extra" slot adds rows under
  // the notes, which the Projects board uses for what a card holds.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import startIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import repeatIcon from '@phosphor-icons/core/assets/duotone/repeat-duotone.svg?raw'
  import bellIcon from '@phosphor-icons/core/assets/duotone/bell-duotone.svg?raw'
  import flagIcon from '@phosphor-icons/core/assets/duotone/flag-duotone.svg?raw'
  import progressIcon from '@phosphor-icons/core/assets/duotone/chart-pie-duotone.svg?raw'
  import listIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import subIcon from '@phosphor-icons/core/assets/duotone/list-checks-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/link-duotone.svg?raw'
  import tagIcon from '@phosphor-icons/core/assets/duotone/tag-duotone.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/text-align-left-duotone.svg?raw'
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import eventIcon from '@phosphor-icons/core/assets/duotone/calendar-check-duotone.svg?raw'
  import warningIcon from '@phosphor-icons/core/assets/duotone/warning-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import DateField from '../calendar/DateField.svelte'
  import TaskCheck from './TaskCheck.svelte'
  import { atMinutes, dayKey, minutesOfDay, parseDayKey } from '../../lib/caldate'
  import { calendarColor, formatTime } from '../../lib/calformat'
  import { presetOf, presetRule, describeRRule, type RepeatPreset } from '../../lib/recurrence'
  import { reminderChoices, reminderLabel } from '../calendar/labels'
  import { dueDate, dueHasTime } from '../../lib/tasklogic'
  import { openMailLink } from '../../lib/maillink'
  import { searchAddresses } from '../../lib/api'
  import { draftOf, localZone, saveTask, toggleDone } from '../../stores/tasks'
  import NoteMentions from '../notes/NoteMentions.svelte'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { AddressBookEntry, Reminder, Task, TaskContact, TaskDraft, TaskList } from '../../lib/types'

  /** The task shown. */
  export let task: Task
  /** Lists the task can move to. */
  export let lists: TaskList[]
  /** Its subtasks, in order. */
  export let subtasks: Task[] = []
  export let weekStart: number = 1
  export let timeFormat: TimeFormat = 'auto'
  /** The time of day a due date gets when a time is first added. */
  export let defaultDueMinutes: number = -1

  const dispatch = createEventDispatcher<{
    close: void
    remove: Task
    open: number
    addSubtask: string
    toEvent: Task
    openEvent: string
  }>()

  // the fields being typed into, kept apart from the task so a reload while
  // typing does not throw the text away.
  let title = task.title
  let notes = task.notes
  let url = task.url
  let tags = task.categories.join(', ')
  let shownId = task.id
  $: if (task.id !== shownId) {
    shownId = task.id
    title = task.title
    notes = task.notes
    url = task.url
    tags = task.categories.join(', ')
  }

  $: list = lists.find((l) => l.id === task.listId)
  $: color = list ? calendarColor(list) : ''
  $: due = dueDate(task)
  $: timed = dueHasTime(task.due)
  $: start = task.start ? (dueHasTime(task.start) ? new Date(task.start) : parseDayKey(task.start)) : null
  $: anchorDay = due ?? new Date()
  $: preset = presetOf(task.rrule, anchorDay)
  $: readOnly = task.readOnly

  async function commit(patch: Partial<TaskDraft>): Promise<void> {
    if (readOnly) {
      return
    }
    await saveTask({ ...draftOf(task), ...patch })
  }

  function commitText(): void {
    const categories = tags
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
    const changed = title !== task.title || notes !== task.notes || url !== task.url || categories.join(',') !== task.categories.join(',')
    if (changed) {
      void commit({ title, notes, url: url.trim(), categories })
    }
  }

  function onFieldKey(event: KeyboardEvent): void {
    if (event.key === 'Enter' && !(event.currentTarget instanceof HTMLTextAreaElement)) {
      event.preventDefault()
      ;(event.currentTarget as HTMLElement).blur()
    }
  }

  onDestroy(commitText)

  // --- due and start -------------------------------------------------------------

  const slots = Array.from({ length: 96 }, (_, i) => i * 15)
  $: dueMinutes = due && timed ? minutesOfDay(due) : 9 * 60
  $: timeItems = (slots.includes(dueMinutes) ? slots : [...slots, dueMinutes].sort((a, b) => a - b)).map((m) => ({
    value: String(m),
    label: formatTime(atMinutes(anchorDay, m), timeFormat),
  }))

  function setDueDay(day: Date): void {
    void commit({ due: timed ? atMinutes(day, dueMinutes).toISOString() : dayKey(day), timeZone: localZone() })
  }

  function setDueTime(minutes: number): void {
    void commit({ due: atMinutes(anchorDay, minutes).toISOString(), timeZone: localZone() })
  }

  function addTime(): void {
    setDueTime(defaultDueMinutes >= 0 ? defaultDueMinutes : 9 * 60)
  }

  function addDue(): void {
    void commit({ due: dayKey(new Date()) })
  }

  function clearDue(): void {
    void commit({ due: '', rrule: '', reminders: task.reminders.filter((r) => r.absolute) })
  }

  function setStart(day: Date | null): void {
    void commit({ start: day ? dayKey(day) : '' })
  }

  // --- repeat ----------------------------------------------------------------------

  $: repeatItems = [
    { value: 'none', label: $t('calendar.repeat.none') },
    { value: 'daily', label: $t('calendar.repeat.daily') },
    { value: 'weekdays', label: $t('calendar.repeat.weekdays') },
    { value: 'weekly', label: $t('calendar.repeat.weeklyOn').replace('{day}', anchorDay.toLocaleDateString(undefined, { weekday: 'long' })) },
    { value: 'monthly', label: $t('calendar.repeat.monthlyOn').replace('{n}', String(anchorDay.getDate())) },
    { value: 'yearly', label: $t('calendar.repeat.yearlyOn').replace('{date}', anchorDay.toLocaleDateString(undefined, { day: 'numeric', month: 'long' })) },
    ...(preset === 'custom' ? [{ value: 'custom', label: describeRRule(task.rrule, anchorDay, $t, navigator.language) }] : []),
  ]

  function pickRepeat(value: string): void {
    if (value === 'custom') {
      return
    }
    const rule = value === 'none' ? '' : presetRule(value as Exclude<RepeatPreset, 'custom'>, anchorDay)
    void commit({ rrule: rule, due: task.due || dayKey(new Date()) })
  }

  // --- reminders -------------------------------------------------------------------

  let addingReminder = false
  let reminderButton: HTMLButtonElement

  $: unusedReminders = reminderChoices.filter((m) => !task.reminders.some((r) => !r.absolute && r.minutesBefore === m))

  function reminderText(r: Reminder): string {
    if (r.absolute) {
      return new Date(r.absolute).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
    }
    return r.minutesBefore === 0 ? $t('tasks.reminder.atDue') : reminderLabel(r.minutesBefore, $t)
  }

  function addReminder(minutes: number): void {
    addingReminder = false
    void commit({ reminders: [...task.reminders, { minutesBefore: minutes, relatedEnd: true, absolute: '', action: '' }] })
  }

  function removeReminder(index: number): void {
    void commit({ reminders: task.reminders.filter((_, i) => i !== index) })
  }

  // --- priority, progress and list --------------------------------------------------

  $: priorityItems = [
    { value: '0', label: $t('tasks.priority.none') },
    { value: '1', label: $t('tasks.priority.high') },
    { value: '5', label: $t('tasks.priority.medium') },
    { value: '9', label: $t('tasks.priority.low') },
  ]

  function priorityValue(p: number): string {
    if (p === 0) {
      return '0'
    }
    return p < 5 ? '1' : p === 5 ? '5' : '9'
  }

  $: statusItems = [
    { value: 'NEEDS-ACTION', label: $t('tasks.status.needsAction') },
    { value: 'IN-PROCESS', label: $t('tasks.status.inProcess') },
    { value: 'COMPLETED', label: $t('tasks.status.completed') },
    { value: 'CANCELLED', label: $t('tasks.status.cancelled') },
  ]

  function setPercent(value: number): void {
    const percent = Math.max(0, Math.min(100, Math.round(value)))
    void commit({ percent, status: percent > 0 && !task.status ? 'IN-PROCESS' : task.status })
  }

  $: listItems = lists.filter((l) => !l.readOnly || l.id === task.listId).map((l) => ({ value: String(l.id), label: l.name || $t('tasks.defaultList') }))

  // --- subtasks --------------------------------------------------------------------

  let newSubtask = ''

  function addSubtask(): void {
    const text = newSubtask.trim()
    if (text) {
      dispatch('addSubtask', text)
      newSubtask = ''
    }
  }

  // --- people ----------------------------------------------------------------------

  let person = ''
  let personInput: HTMLInputElement
  let suggestions: AddressBookEntry[] = []
  let highlight = -1
  let searchSeq = 0
  let debounce: ReturnType<typeof setTimeout> | undefined

  onDestroy(() => clearTimeout(debounce))

  function onPersonInput(): void {
    clearTimeout(debounce)
    const q = person.trim()
    if (!q) {
      suggestions = []
      return
    }
    debounce = setTimeout(async () => {
      const seq = ++searchSeq
      try {
        const found = await searchAddresses(q, 6)
        if (seq === searchSeq && person.trim() === q) {
          const taken = new Set(task.contacts.map((c) => c.email.toLowerCase()))
          suggestions = found.filter((e) => !taken.has(e.email.toLowerCase()))
          highlight = suggestions.length ? 0 : -1
        }
      } catch {
        suggestions = []
      }
    }, 140)
  }

  function addPerson(contact: TaskContact): void {
    person = ''
    suggestions = []
    searchSeq++
    if (!contact.email && !contact.name) {
      return
    }
    void commit({ contacts: [...task.contacts, contact] })
  }

  function typedPerson(): TaskContact {
    const text = person.trim()
    const match = text.match(/^(.*)<([^>]+)>$/)
    if (match) {
      return { name: match[1].trim().replace(/^"|"$/g, ''), email: match[2].trim() }
    }
    return text.includes('@') ? { name: '', email: text } : { name: text, email: '' }
  }

  function onPersonKey(event: KeyboardEvent): void {
    if (suggestions.length && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      highlight = (highlight + (event.key === 'ArrowDown' ? 1 : -1) + suggestions.length) % suggestions.length
    } else if (event.key === 'Enter' || event.key === ',') {
      event.preventDefault()
      const pick = suggestions[highlight]
      addPerson(pick ? { name: pick.name, email: pick.email } : typedPerson())
    } else if (event.key === 'Escape' && suggestions.length) {
      event.preventDefault()
      event.stopPropagation()
      suggestions = []
    } else if (event.key === 'Backspace' && person === '' && task.contacts.length) {
      void commit({ contacts: task.contacts.slice(0, -1) })
    }
  }

  // the notes grow with what is in them.
  function grow(el: HTMLTextAreaElement, _text: string): { update(): void } {
    const fit = (): void => {
      el.style.height = 'auto'
      el.style.height = `${el.scrollHeight}px`
    }
    fit()
    return { update: fit }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section class="detail" aria-label={$t('tasks.detail')} style={color ? `--cal:${color}` : undefined}>
  <header class="head">
    <TaskCheck checked={task.done} label={$t('tasks.toggle').replace('{title}', task.title || $t('tasks.untitled'))} {color} disabled={readOnly} on:toggle={(e) => void toggleDone(task, e.detail)} />
    <input
      class="title"
      class:done={task.done}
      bind:value={title}
      placeholder={$t('tasks.titlePlaceholder')}
      aria-label={$t('tasks.title')}
      readonly={readOnly}
      on:blur={commitText}
      on:keydown={onFieldKey}
    />
    <button type="button" class="cal-icon" aria-label={$t('tasks.closeDetail')} on:click={() => dispatch('close')}>
      <Ph svg={closeIcon} size={14} />
    </button>
  </header>

  {#if task.conflict || task.syncError}
    <p class="note warn"><Ph svg={warningIcon} size={13} />{task.conflict ? $t('tasks.conflictNote') : task.syncError}</p>
  {:else if task.pending}
    <p class="note">{$t('tasks.pendingNote')}</p>
  {/if}

  <div class="rows">
    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={clockIcon} size={16} /></span>
      <div class="cell line">
        {#if due}
          <DateField class="ctl" value={due} {weekStart} label={$t('tasks.due')} disabled={readOnly} on:change={(e) => setDueDay(e.detail)} />
          {#if timed}
            <Select class="ctl" value={String(dueMinutes)} items={timeItems} ariaLabel={$t('tasks.dueTime')} disabled={readOnly} on:change={(e) => setDueTime(Number(e.detail))} />
            <button type="button" class="link" disabled={readOnly} on:click={() => setDueDay(anchorDay)}>{$t('tasks.removeTime')}</button>
          {:else}
            <button type="button" class="link" disabled={readOnly} on:click={addTime}>{$t('tasks.addTime')}</button>
          {/if}
          <span class="spacer"></span>
          <button type="button" class="cal-icon" aria-label={$t('tasks.clearDue')} disabled={readOnly} on:click={clearDue}>
            <Ph svg={closeIcon} size={12} />
          </button>
        {:else}
          <button type="button" class="link" disabled={readOnly} on:click={addDue}>{$t('tasks.addDue')}</button>
        {/if}
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={startIcon} size={16} /></span>
      <div class="cell line">
        {#if start}
          <span class="label">{$t('tasks.starts')}</span>
          <DateField class="ctl" value={start} {weekStart} label={$t('tasks.start')} disabled={readOnly} on:change={(e) => setStart(e.detail)} />
          <span class="spacer"></span>
          <button type="button" class="cal-icon" aria-label={$t('tasks.clearStart')} disabled={readOnly} on:click={() => setStart(null)}>
            <Ph svg={closeIcon} size={12} />
          </button>
        {:else}
          <button type="button" class="link" disabled={readOnly} on:click={() => setStart(new Date())}>{$t('tasks.addStart')}</button>
        {/if}
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={repeatIcon} size={16} /></span>
      <div class="cell line">
        <Select class="ctl" value={preset} items={repeatItems} ariaLabel={$t('calendar.repeat')} disabled={readOnly} on:change={(e) => pickRepeat(e.detail)} />
        {#if task.rrule}
          <span class="label">{$t('tasks.repeatNote')}</span>
        {/if}
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={bellIcon} size={16} /></span>
      <div class="cell chips">
        {#each task.reminders as r, i (i)}
          <span class="chip">
            {reminderText(r)}
            <button type="button" aria-label={$t('tasks.removeReminder')} disabled={readOnly} on:click={() => removeReminder(i)}><Ph svg={closeIcon} size={10} /></button>
          </span>
        {/each}
        <button type="button" class="add" bind:this={reminderButton} disabled={readOnly || !due} title={due ? '' : $t('tasks.reminderNeedsDue')} on:click={() => (addingReminder = true)}>
          <Ph svg={plusIcon} size={12} />{$t('calendar.addReminder')}
        </button>
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={flagIcon} size={16} /></span>
      <div class="cell line">
        <Select class="ctl" value={priorityValue(task.priority)} items={priorityItems} ariaLabel={$t('tasks.priority')} disabled={readOnly} on:change={(e) => void commit({ priority: Number(e.detail) })} />
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={progressIcon} size={16} /></span>
      <div class="cell line">
        <Select class="ctl" value={task.status || (task.done ? 'COMPLETED' : 'NEEDS-ACTION')} items={statusItems} ariaLabel={$t('tasks.status')} disabled={readOnly} on:change={(e) => void commit({ status: e.detail })} />
        <input
          class="percent"
          type="range"
          min="0"
          max="100"
          step="10"
          value={task.percent}
          aria-label={$t('tasks.percent')}
          disabled={readOnly || task.done}
          on:change={(e) => setPercent(Number(e.currentTarget.value))}
        />
        <span class="label num">{task.percent}%</span>
      </div>
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={listIcon} size={16} /></span>
      <div class="cell line">
        <Select class="ctl" value={String(task.listId)} items={listItems} ariaLabel={$t('tasks.list')} disabled={readOnly} on:change={(e) => void commit({ listId: Number(e.detail) })} />
      </div>
    </div>

    <div class="row top">
      <span class="icon" aria-hidden="true"><Ph svg={subIcon} size={16} /></span>
      <div class="cell">
        {#each subtasks as sub (sub.id)}
          <div class="sub">
            <TaskCheck checked={sub.done} label={$t('tasks.toggle').replace('{title}', sub.title || $t('tasks.untitled'))} {color} disabled={sub.readOnly} on:toggle={(e) => void toggleDone(sub, e.detail)} />
            <button type="button" class="sub-title" class:done={sub.done} on:click={() => dispatch('open', sub.id)}>{sub.title || $t('tasks.untitled')}</button>
          </div>
        {/each}
        {#if !readOnly}
          <form class="sub-new" on:submit|preventDefault={addSubtask}>
            <Ph svg={plusIcon} size={12} />
            <input class="field" bind:value={newSubtask} placeholder={$t('tasks.addSubtask')} aria-label={$t('tasks.addSubtask')} />
          </form>
        {/if}
      </div>
    </div>

    <div class="row top">
      <span class="icon" aria-hidden="true"><Ph svg={usersIcon} size={16} /></span>
      <div class="cell">
        <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
        <div class="chips" on:click={() => personInput?.focus()}>
          {#each task.contacts as c, i (i)}
            <span class="chip" title={c.email}>
              {c.name || c.email}
              <button
                type="button"
                aria-label={$t('tasks.removePerson').replace('{name}', c.name || c.email)}
                disabled={readOnly}
                on:click={() => void commit({ contacts: task.contacts.filter((_, j) => j !== i) })}
              >
                <Ph svg={closeIcon} size={10} />
              </button>
            </span>
          {/each}
          {#if !readOnly}
            <input
              class="field inline"
              bind:this={personInput}
              bind:value={person}
              placeholder={task.contacts.length ? '' : $t('tasks.addPeople')}
              aria-label={$t('tasks.people')}
              aria-autocomplete="list"
              on:input={onPersonInput}
              on:keydown={onPersonKey}
              on:blur={() => setTimeout(() => (suggestions = []), 150)}
            />
          {/if}
        </div>
        {#if suggestions.length > 0}
          <ul class="suggest" role="listbox" aria-label={$t('tasks.people')}>
            {#each suggestions as s, i (s.email)}
              <li role="option" aria-selected={i === highlight}>
                <button type="button" class:on={i === highlight} on:mousedown|preventDefault={() => addPerson({ name: s.name, email: s.email })}>
                  <span>{s.name || s.email}</span>
                  {#if s.name}<span class="label">{s.email}</span>{/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>

    {#if task.mailLinks.length > 0 || task.eventUids.length > 0}
      <div class="row">
        <span class="icon" aria-hidden="true"><Ph svg={linkIcon} size={16} /></span>
        <div class="cell chips">
          {#each task.mailLinks as link (link)}
            <button type="button" class="pill" on:click={() => openMailLink(link)}><Ph svg={envelopeIcon} size={13} />{$t('tasks.openMail')}</button>
          {/each}
          {#each task.eventUids as uid (uid)}
            <button type="button" class="pill" on:click={() => dispatch('openEvent', uid)}><Ph svg={eventIcon} size={13} />{$t('tasks.openEvent')}</button>
          {/each}
        </div>
      </div>
    {/if}

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={linkIcon} size={16} /></span>
      <input class="field" bind:value={url} placeholder={$t('tasks.addUrl')} aria-label={$t('tasks.url')} readonly={readOnly} on:blur={commitText} on:keydown={onFieldKey} />
    </div>

    <div class="row">
      <span class="icon" aria-hidden="true"><Ph svg={tagIcon} size={16} /></span>
      <input class="field" bind:value={tags} placeholder={$t('tasks.addTags')} aria-label={$t('tasks.tags')} readonly={readOnly} on:blur={commitText} on:keydown={onFieldKey} />
    </div>

    {#if task.uid}
      <div class="row">
        <span class="icon" aria-hidden="true"></span>
        <div class="cell">
          <NoteMentions about={{ kind: 'task', id: task.uid, title: task.title || $t('tasks.untitled') }} />
        </div>
      </div>
    {/if}

    <div class="row top">
      <span class="icon" aria-hidden="true"><Ph svg={notesIcon} size={16} /></span>
      <textarea class="field notes" rows="3" bind:value={notes} use:grow={notes} placeholder={$t('calendar.addNotes')} aria-label={$t('calendar.notes')} readonly={readOnly} on:blur={commitText}></textarea>
    </div>
    <slot name="extra" />
  </div>

  <footer class="foot">
    {#if !readOnly}
      <button type="button" class="cal-btn danger" on:click={() => dispatch('remove', task)}>{$t('tasks.delete')}</button>
    {/if}
    <span class="spacer"></span>
    {#if !readOnly}
      <button type="button" class="cal-btn" on:click={() => dispatch('toEvent', task)}>{$t('tasks.toEvent')}</button>
    {/if}
  </footer>
</section>

{#if addingReminder && reminderButton}
  <AnchoredPopover anchor={reminderButton} align="start" label={$t('calendar.addReminder')} on:close={() => (addingReminder = false)}>
    <div class="cal-menu">
      {#each unusedReminders as m (m)}
        <button type="button" class="cal-item" on:click={() => addReminder(m)}>{m === 0 ? $t('tasks.reminder.atDue') : reminderLabel(m, $t)}</button>
      {/each}
    </div>
  </AnchoredPopover>
{/if}

<style>
  .detail {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
    background: var(--surface-raised);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-4) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .title {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-heading);
    font-weight: var(--fw-medium);
  }

  .title:focus {
    outline: none;
  }

  .title.done {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .title::placeholder,
  .field::placeholder {
    color: var(--text-tertiary);
    opacity: 1;
  }

  .note {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .note.warn {
    color: var(--warning);
  }

  .rows {
    flex: 1;
    min-height: 0;
    padding: 0 var(--space-4);
    overflow-y: auto;
  }

  /* one row per thing: the icon column, then the value, divided by hairlines
     like the event editor. */
  .row {
    position: relative;
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

  .row.top {
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
    align-items: stretch;
    gap: var(--space-1);
    min-width: 0;
  }

  .cell.line {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
  }

  .spacer {
    flex: 1;
  }

  .label {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .num {
    font-variant-numeric: tabular-nums;
    min-width: calc(var(--space-6) + var(--space-2));
  }

  .detail :global(.ctl) {
    height: var(--control-height);
    padding: 0 var(--space-2);
    border-color: transparent;
    background: transparent;
    font-size: var(--fz-list);
  }

  .detail :global(.ctl:not(:disabled):hover) {
    border-color: var(--border-default);
    background: var(--surface-raised);
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

  .link:hover:not(:disabled) {
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

  .percent {
    flex: 1;
    min-width: calc(var(--space-6) * 3);
    accent-color: var(--cal, var(--accent));
  }

  .chips {
    display: flex;
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
    min-height: var(--control-height);
    padding-inline: var(--space-2);
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
  }

  .pill:hover {
    background: var(--surface-hover);
  }

  .chip button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--chip-height) - var(--space-2));
    height: calc(var(--chip-height) - var(--space-2));
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .chip button:hover:not(:disabled) {
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

  .add:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .sub {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: var(--control-height);
    padding-inline: var(--space-2);
  }

  .sub-title {
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    text-align: start;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    cursor: var(--cursor-action);
  }

  .sub-title.done {
    color: var(--text-tertiary);
    text-decoration: line-through;
  }

  .sub-new {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding-inline-start: var(--space-2);
    color: var(--text-tertiary);
  }

  .suggest {
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .suggest button {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-list);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .suggest button.on,
  .suggest button:hover {
    background: var(--selection-bg);
  }

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .link:focus-visible,
  .pill:focus-visible,
  .add:focus-visible,
  .sub-title:focus-visible,
  .chip button:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
