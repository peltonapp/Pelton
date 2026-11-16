<script lang="ts">
  // the Prepare panel: what helps before a meeting, gathered from what is
  // stored on this device. who is invited with their address book details and
  // reply, the recent conversations with them (those with several of them or
  // with a subject like the title first), files exchanged, open tasks that
  // touch them, earlier meetings with the same people, and the call to join.
  // conversations open in the reader, files in the previewer. writing to
  // everyone and making a task for the meeting are one click each.
  import { createEventDispatcher, onMount } from 'svelte'
  import writeIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import taskIcon from '@phosphor-icons/core/assets/duotone/check-square-duotone.svg?raw'
  import videoIcon from '@phosphor-icons/core/assets/duotone/video-camera-duotone.svg?raw'
  import mapPinIcon from '@phosphor-icons/core/assets/duotone/map-pin-duotone.svg?raw'
  import clockIcon from '@phosphor-icons/core/assets/duotone/clock-duotone.svg?raw'
  import '../calendar/controls.css'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ContactAvatar from '../contacts/ContactAvatar.svelte'
  import TaskCheck from '../tasks/TaskCheck.svelte'
  import { kindIcon, asAttachment } from '../attachments/kinds'
  import { partStatKey } from './labels'
  import { prepareMeeting } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { parseEventTime } from '../../lib/caldate'
  import { formatLongDay, formatWhen } from '../../lib/calformat'
  import { attendeeDetail, attendeeName, composeFor, otherAttendees, prepTaskDraft } from '../../lib/meetingprep'
  import { openDayInCalendar } from '../../lib/mailcalendar'
  import { formatBytes, formatListDate, type TimeFormat } from '../../lib/format'
  import { dueDate } from '../../lib/tasklogic'
  import { openTask } from '../../lib/taskmail'
  import { sidebar } from '../../stores/accounts'
  import { openPreview } from '../../stores/preview'
  import { revealMessage } from '../../stores/selection'
  import { openSpace } from '../../stores/space'
  import { prefs } from '../../stores/prefs'
  import { defaultListId, loadTasks, localZone, saveTask, taskLists, taskSettings, toggleDone } from '../../stores/tasks'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MeetingPrep, PrepAttendee } from '../../lib/types'

  /** The meeting to prepare for: an event, or one occurrence of a series. */
  export let target: { eventId: number; recurrenceId: string }

  const dispatch = createEventDispatcher<{ close: void; compose: { to: string; subject: string } }>()

  let prep: MeetingPrep | null = null
  let error = ''
  let making = false

  $: timeFormat = $prefs.timeFormat as TimeFormat
  $: title = prep?.title || $t('calendar.untitled')
  $: others = prep ? otherAttendees(prep) : []
  $: when = prep ? formatWhen(parseEventTime(prep.start, prep.allDay), parseEventTime(prep.end, prep.allDay), prep.allDay, timeFormat) : ''

  onMount(async () => {
    try {
      prep = await prepareMeeting(target.eventId, target.recurrenceId)
    } catch (err) {
      error = errorMessage(err)
    }
  })

  function openConversation(m: MeetingPrep['threads'][number]['message']): void {
    revealMessage(m.id, m.accountId, m.folderId, $sidebar.data ?? null)
    openSpace('mail')
    dispatch('close')
  }

  function openMeeting(start: string, allDay: boolean): void {
    openDayInCalendar(parseEventTime(start, allDay))
    dispatch('close')
  }

  function writeToAll(): void {
    if (prep && others.length > 0) {
      dispatch('compose', composeFor(prep))
    }
  }

  async function makeTask(): Promise<void> {
    if (!prep || making) {
      return
    }
    making = true
    try {
      if ($taskLists.length === 0) {
        await loadTasks()
      }
      const listId = defaultListId($taskLists, $taskSettings)
      if (listId === 0) {
        toastError($t('tasks.noWritable'))
        return
      }
      if (await saveTask(prepTaskDraft(prep, listId, $t('prep.taskTitle'), localZone()))) {
        toastSuccess($t('prep.taskMade'))
      }
    } finally {
      making = false
    }
  }

  function replyLabel(p: PrepAttendee): string {
    return $t(partStatKey(p.partStat))
  }

  function peopleLine(names: string[]): string {
    return $t('prep.withPeople').replace('{people}', names.join(', '))
  }
</script>

<Modal title={$t('prep.title').replace('{title}', title)} hint={$t('prep.hint')} size="large" on:close={() => dispatch('close')}>
  {#if error}
    <p class="problem" role="alert">{error}</p>
  {:else if !prep}
    <p class="state"><PieProgress label={$t('prep.loading')} /> {$t('prep.loading')}</p>
  {:else}
    <div class="prep">
      <section class="meeting" aria-label={$t('prep.meeting')}>
        <p class="line"><Ph svg={clockIcon} size={15} /><span>{when}</span></p>
        {#if prep.location}
          <p class="line"><Ph svg={mapPinIcon} size={15} /><span class="wrap">{prep.location}</span></p>
        {/if}
        {#if prep.join}
          <p class="line">
            <Ph svg={videoIcon} size={15} />
            <button type="button" class="cal-btn primary" on:click={() => prep?.join && openURL(prep.join.url)}>
              {prep.join.provider ? $t('prep.joinWith').replace('{provider}', prep.join.provider) : $t('prep.join')}
            </button>
          </p>
        {/if}
        {#if prep.description}
          <p class="notes">{prep.description}</p>
        {/if}
      </section>

      <section class="block" aria-label={$t('prep.attendees')}>
        <h3>{$t('prep.attendees')}</h3>
        {#if others.length === 0}
          <p class="none">{$t('prep.nobody')}</p>
        {/if}
        {#if prep.attendees.length > 0}
          <ul class="people">
            {#each prep.attendees as p (p.email)}
              <li>
                {#if p.contact}
                  <ContactAvatar contact={p.contact} size={28} />
                {:else}
                  <span class="initial" aria-hidden="true">{attendeeName(p).slice(0, 1).toUpperCase()}</span>
                {/if}
                <span class="who">
                  <span class="name">
                    {attendeeName(p)}
                    {#if p.self}<span class="tag">{$t('prep.you')}</span>{/if}
                    {#if p.organizer}<span class="tag">{$t('calendar.organizer')}</span>{/if}
                  </span>
                  <span class="detail" dir="auto">{attendeeDetail(p)}</span>
                  {#if p.contact && p.contact.phones.length > 0}
                    <span class="detail" dir="ltr">{p.contact.phones[0].value}</span>
                  {/if}
                </span>
                <span class="reply {p.partStat.toLowerCase()}">{replyLabel(p)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block" aria-label={$t('prep.conversations')}>
        <h3>{$t('prep.conversations')}</h3>
        {#if prep.threads.length === 0}
          <p class="none">{$t('prep.conversationsNone')}</p>
        {:else}
          <ul class="rows">
            {#each prep.threads as th (th.message.id)}
              <li>
                <button type="button" class="row" on:click={() => openConversation(th.message)}>
                  <span class="main" dir="auto">{th.subject || $t('detail.noSubject')}</span>
                  <span class="aside">{formatListDate(th.last, timeFormat)}</span>
                  <span class="sub">
                    {#if th.people.length > 0}{peopleLine(th.people)}{/if}
                    {#if th.titleMatch}<span class="tag">{$t('prep.titleMatch')}</span>{/if}
                    {#if th.messages > 1}<span class="count">{$t('prep.messages').replace('{n}', String(th.messages))}</span>{/if}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block" aria-label={$t('prep.files')}>
        <h3>{$t('prep.files')}</h3>
        {#if prep.files.length === 0}
          <p class="none">{$t('prep.filesNone')}</p>
        {:else}
          <ul class="rows">
            {#each prep.files as f (f.id)}
              <li>
                <button type="button" class="row file" on:click={() => openPreview(f.messageId, asAttachment(f))}>
                  <Ph svg={kindIcon[f.kind] ?? kindIcon.other} size={15} />
                  <span class="main" dir="auto" title={f.filename}>{f.filename}</span>
                  <span class="aside">{formatBytes(f.sizeBytes)}</span>
                  <span class="sub">{f.fromName || f.fromAddress}, {formatListDate(f.date, timeFormat)}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block" aria-label={$t('prep.tasks')}>
        <h3>{$t('prep.tasks')}</h3>
        {#if prep.tasks.length === 0}
          <p class="none">{$t('prep.tasksNone')}</p>
        {:else}
          <ul class="rows">
            {#each prep.tasks as task (task.id)}
              <li class="task">
                <TaskCheck
                  checked={task.done}
                  label={$t('tasks.toggle').replace('{title}', task.title || $t('tasks.untitled'))}
                  color={task.listColor}
                  disabled={task.readOnly}
                  on:toggle={() => void toggleDone(task)}
                />
                <button type="button" class="row inline" on:click={() => openTask(task.id)}>
                  <span class="main" dir="auto">{task.title || $t('tasks.untitled')}</span>
                  {#if task.due}
                    <span class="aside">{dueDate(task)?.toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}</span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <section class="block" aria-label={$t('prep.previous')}>
        <h3>{$t('prep.previous')}</h3>
        {#if prep.previous.length === 0}
          <p class="none">{$t('prep.previousNone')}</p>
        {:else}
          <ul class="rows">
            {#each prep.previous as m (`${m.eventId}|${m.recurrenceId}`)}
              <li>
                <button type="button" class="row" on:click={() => openMeeting(m.start, m.allDay)}>
                  <span class="main" dir="auto">{m.title || $t('calendar.untitled')}</span>
                  <span class="aside">{formatLongDay(parseEventTime(m.start, m.allDay), undefined, new Date())}</span>
                  <span class="sub">
                    {#if m.sameSeries}<span class="tag">{$t('prep.sameSeries')}</span>{/if}
                    {#if !m.sameSeries && m.shared.length > 0}{peopleLine(m.shared)}{/if}
                  </span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>
    </div>
  {/if}

  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" disabled={!prep || making} on:click={makeTask}>
      <Ph svg={taskIcon} size={15} />{$t('prep.task')}
    </button>
    <button type="button" class="cal-btn primary" disabled={!prep || others.length === 0} on:click={writeToAll}>
      <Ph svg={writeIcon} size={15} />{$t('prep.write')}
    </button>
  </svelte:fragment>
</Modal>

<style>
  .prep {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(calc(var(--fz-body) * 20), 1fr));
    gap: var(--space-5) var(--space-5);
    width: 100%;
    min-width: 0;
  }

  .meeting {
    grid-column: 1 / -1;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .line {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-body);
  }

  .wrap {
    overflow-wrap: anywhere;
  }

  .notes {
    max-height: calc(var(--fz-body) * 9);
    margin: var(--space-2) 0 0;
    padding: var(--space-3);
    overflow-y: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .block {
    min-width: 0;
  }

  h3 {
    margin: 0 0 var(--space-2);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .none {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    margin: var(--space-6) 0;
    color: var(--text-secondary);
  }

  .problem {
    margin: 0;
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
    color: var(--danger);
    font-size: var(--fz-label);
  }

  .people {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .people li {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }

  .initial {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: calc(var(--fz-body) * 2.1);
    height: calc(var(--fz-body) * 2.1);
    border: var(--hairline) solid var(--border-default);
    border-radius: 50%;
    background: var(--surface-sunken);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .who {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    overflow-wrap: anywhere;
  }

  .detail {
    color: var(--text-secondary);
    font-size: var(--fz-label);
    overflow-wrap: anywhere;
  }

  .tag {
    margin-inline-start: var(--space-2);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-regular);
  }

  .reply {
    flex-shrink: 0;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
  }

  .reply.accepted {
    color: var(--success);
  }

  .reply.declined {
    color: var(--danger);
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    column-gap: var(--space-3);
    width: 100%;
    padding: var(--space-2) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -1px;
  }

  .row.file {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .row.file .sub {
    grid-column: 2 / -1;
  }

  .row.inline {
    flex: 1;
    min-width: 0;
  }

  .main {
    min-width: 0;
    overflow: hidden;
    font-size: var(--fz-body);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .aside {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .sub {
    grid-column: 1 / -1;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    overflow-wrap: anywhere;
  }

  .count {
    margin-inline-start: var(--space-2);
    color: var(--text-tertiary);
  }

  .task {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
</style>
