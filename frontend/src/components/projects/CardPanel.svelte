<script lang="ts">
  // a card opened beside its board: the task's own detail pane, with what the
  // card holds under it: the conversations, events and notes attached to it.
  // notes and events are attached from here; mail by dropping it on the card.
  // the card can leave the board without its task being deleted.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import { get } from 'svelte/store'
  import { fade } from 'svelte/transition'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import eventIcon from '@phosphor-icons/core/assets/duotone/calendar-check-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import clipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import TaskDetail from '../tasks/TaskDetail.svelte'
  import { linkEventToCard, listCardLinks, listOccurrences, removeProjectLink } from '../../lib/api'
  import { openLinkedMail, showEvent } from '../../lib/projectlinks'
  import { noteTitle, projectNotes, type ProjectNote } from '../../lib/projectnotes'
  import { formatWhen } from '../../lib/calformat'
  import { addDays, parseEventTime, toRFC3339 } from '../../lib/caldate'
  import { onProjectsChanged } from '../../lib/events'
  import { tasks } from '../../stores/tasks'
  import { reloadVault, vaultIndex, vaultNotes } from '../../stores/notes'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Occurrence, ProjectCard, ProjectLink, TaskList } from '../../lib/types'

  /** The card open. */
  export let card: ProjectCard
  /** Task lists the task can move to. */
  export let lists: TaskList[]
  export let weekStart = 1
  export let timeFormat: TimeFormat = 'auto'
  export let defaultDueMinutes = -1

  const dispatch = createEventDispatcher<{
    close: void
    removeCard: ProjectCard
    deleteTask: ProjectCard
    openNote: string
    openSubtask: number
    addSubtask: string
    toEvent: ProjectCard
  }>()

  let links: ProjectLink[] = []
  let loading = true
  let shownUid = ''
  let picking: 'note' | 'event' | null = null
  let noteButton: HTMLButtonElement
  let eventButton: HTMLButtonElement
  let notes: ProjectNote[] = []
  let events: Occurrence[] = []
  let pickQuery = ''

  $: task = card.task
  $: subtasks = $tasks.filter((x) => x.listId === task.listId && x.parent === task.uid)
  $: if (task.uid !== shownUid) {
    shownUid = task.uid
    void loadLinks()
  }
  $: mail = links.filter((l) => l.kind === 'mail')
  $: eventLinks = links.filter((l) => l.kind === 'event')
  $: noteLinks = links.filter((l) => l.kind === 'note')
  $: attachedNotes = new Set(noteLinks.map((l) => l.ref))
  $: attachedEvents = new Set(eventLinks.map((l) => l.ref))
  $: pickNotes = notes.filter((n) => !attachedNotes.has(n.id) && match(noteTitle(n, ''), pickQuery)).slice(0, 50)
  $: pickEvents = events.filter((e) => !attachedEvents.has(e.uid) && match(e.summary, pickQuery)).slice(0, 30)

  function match(text: string, q: string): boolean {
    return !q.trim() || text.toLocaleLowerCase().includes(q.trim().toLocaleLowerCase())
  }

  async function loadLinks(): Promise<void> {
    const uid = task.uid
    try {
      const next = await listCardLinks(uid)
      if (uid === task.uid) {
        links = next
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loading = false
    }
  }

  const off = onProjectsChanged((e) => {
    if (e.projectIds.length === 0 || e.projectIds.includes(card.projectId)) {
      void loadLinks()
    }
  })
  onDestroy(off)

  async function unlink(link: ProjectLink): Promise<void> {
    links = links.filter((l) => l.id !== link.id)
    try {
      await removeProjectLink(link.id)
    } catch (err) {
      toastError(errorMessage(err))
      void loadLinks()
    }
  }

  async function openPicker(kind: 'note' | 'event'): Promise<void> {
    pickQuery = ''
    picking = kind
    try {
      if (kind === 'note') {
        const list = await projectNotes().list(card.projectId)
        notes = list.notes
        if (list.files) {
          // any note of the notes folder can be attached, the project's own first.
          if (!get(vaultIndex)) {
            await reloadVault()
          }
          const own = new Set(notes.map((n) => n.id))
          const others = get(vaultNotes)
            .filter((n) => !own.has(n.path))
            .map((n): ProjectNote => ({ id: n.path, projectId: card.projectId, title: n.title, body: '', snippet: n.snippet, path: n.path, updatedAt: '' }))
          notes = [...notes, ...others]
        }
      } else {
        const from = new Date()
        events = (await listOccurrences(toRFC3339(addDays(from, -7)), toRFC3339(addDays(from, 60)))).filter(
          (o, i, all) => all.findIndex((x) => x.uid === o.uid) === i,
        )
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function attachNote(note: ProjectNote): Promise<void> {
    picking = null
    try {
      await projectNotes().attach(note, task.uid)
      await loadLinks()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function attachEvent(o: Occurrence): Promise<void> {
    picking = null
    try {
      await linkEventToCard(task.uid, o.uid, o.summary)
      await loadLinks()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function when(o: Occurrence): string {
    return formatWhen(parseEventTime(o.start, o.allDay), parseEventTime(o.end, o.allDay), o.allDay, timeFormat)
  }
</script>

<div class="panel">
  <TaskDetail
    {task}
    {lists}
    {subtasks}
    {weekStart}
    {timeFormat}
    {defaultDueMinutes}
    on:close={() => dispatch('close')}
    on:remove={() => dispatch('deleteTask', card)}
    on:open={(e) => dispatch('openSubtask', e.detail)}
    on:addSubtask={(e) => dispatch('addSubtask', e.detail)}
    on:toEvent={() => dispatch('toEvent', card)}
    on:openEvent={(e) => void showEvent(e.detail)}
  >
    <div slot="extra" class="attached">
      <div class="head">
        <span class="icon" aria-hidden="true"><Ph svg={clipIcon} size={16} /></span>
        <h3>{$t('projects.card.attached')}</h3>
        {#if loading}
          <PieProgress label={$t('projects.loading')} />
        {/if}
      </div>
      {#if !loading && links.length === 0}
        <p class="empty" in:fade={{ duration: dur(motion.fast) }}>{$t('projects.card.nothingAttached')}</p>
      {/if}
      <ul class="links">
        {#each mail as link (link.id)}
          <li>
            <button type="button" class="item" on:click={() => openLinkedMail(link)}>
              <Ph svg={mailIcon} size={14} /><span class="text">{link.label || $t('messageList.noSubject')}</span>
            </button>
            <button type="button" class="cal-icon" aria-label={$t('projects.card.detach').replace('{name}', link.label)} on:click={() => void unlink(link)}><Ph svg={closeIcon} size={11} /></button>
          </li>
        {/each}
        {#each eventLinks as link (link.id)}
          <li>
            <button type="button" class="item" on:click={() => void showEvent(link.ref)}>
              <Ph svg={eventIcon} size={14} /><span class="text">{link.label || $t('calendar.untitled')}</span>
            </button>
            <button type="button" class="cal-icon" aria-label={$t('projects.card.detach').replace('{name}', link.label)} on:click={() => void unlink(link)}><Ph svg={closeIcon} size={11} /></button>
          </li>
        {/each}
        {#each noteLinks as link (link.id)}
          <li>
            <button type="button" class="item" on:click={() => dispatch('openNote', link.ref)}>
              <Ph svg={noteIcon} size={14} /><span class="text">{link.label || $t('projects.notes.untitled')}</span>
            </button>
            <button type="button" class="cal-icon" aria-label={$t('projects.card.detach').replace('{name}', link.label)} on:click={() => void unlink(link)}><Ph svg={closeIcon} size={11} /></button>
          </li>
        {/each}
      </ul>
      <div class="adders">
        <button type="button" class="add" bind:this={noteButton} on:click={() => void openPicker('note')}>
          <Ph svg={plusIcon} size={12} />{$t('projects.card.attachNote')}
        </button>
        <button type="button" class="add" bind:this={eventButton} on:click={() => void openPicker('event')}>
          <Ph svg={plusIcon} size={12} />{$t('projects.card.attachEvent')}
        </button>
      </div>
      <p class="tip">{$t('projects.card.mailTip')}</p>
      <button type="button" class="cal-btn leave" on:click={() => dispatch('removeCard', card)}>{$t('projects.card.remove')}</button>
    </div>
  </TaskDetail>
</div>

{#if picking}
  <AnchoredPopover
    anchor={picking === 'note' ? noteButton : eventButton}
    align="start"
    label={picking === 'note' ? $t('projects.card.attachNote') : $t('projects.card.attachEvent')}
    on:close={() => (picking = null)}
  >
    <div class="picker">
      <!-- svelte-ignore a11y-autofocus -->
      <input class="cal-field" bind:value={pickQuery} placeholder={$t('projects.card.pickFilter')} aria-label={$t('projects.card.pickFilter')} autofocus />
      <div class="options">
        {#if picking === 'note'}
          {#each pickNotes as n (n.id)}
            <button type="button" class="cal-item" on:click={() => void attachNote(n)}>
              <Ph svg={noteIcon} size={14} /><span class="text">{noteTitle(n, $t('projects.notes.untitled'))}</span>
            </button>
          {:else}
            <p class="none">{notes.length === 0 ? $t('projects.card.noNotes') : $t('projects.noMatches')}</p>
          {/each}
        {:else}
          {#each pickEvents as o (o.uid)}
            <button type="button" class="cal-item event" on:click={() => void attachEvent(o)}>
              <span class="dot" style={`background:${o.calendarColor || 'var(--cal-1)'}`} aria-hidden="true"></span>
              <span class="text">{o.summary || $t('calendar.untitled')}</span>
              <span class="when">{when(o)}</span>
            </button>
          {:else}
            <p class="none">{$t('projects.card.noEvents')}</p>
          {/each}
        {/if}
      </div>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .panel {
    height: 100%;
  }

  .attached {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3) 0 var(--space-5);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .head .icon {
    display: inline-flex;
    width: var(--space-6);
    justify-content: center;
    color: var(--text-tertiary);
  }

  h3 {
    flex: 1;
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  .empty,
  .tip {
    margin: 0;
    padding-inline-start: calc(var(--space-6) + var(--space-3));
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .links {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0 0 0 calc(var(--space-6) + var(--space-3));
    list-style: none;
  }

  .links li {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    border-radius: var(--radius-control);
  }

  .links li:hover {
    background: var(--surface-hover);
  }

  .links li .cal-icon {
    opacity: 0;
  }

  .links li:hover .cal-icon,
  .links li:focus-within .cal-icon {
    opacity: 1;
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
    padding: var(--space-1) var(--space-2);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .item :global(svg) {
    color: var(--text-tertiary);
    flex-shrink: 0;
  }

  .text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .adders {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding-inline-start: calc(var(--space-6) + var(--space-3));
  }

  .add {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) dashed var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .add:hover {
    border-color: var(--border-strong);
    color: var(--text-primary);
  }

  .leave {
    align-self: flex-start;
    margin: var(--space-3) 0 0 calc(var(--space-6) + var(--space-3));
  }

  .picker {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    width: var(--cal-popover-width);
    padding: var(--space-3);
  }

  .options {
    display: flex;
    flex-direction: column;
    max-height: var(--snippet-list-height);
    overflow-y: auto;
  }

  .cal-item .text {
    flex: 1;
  }

  .when {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .dot {
    flex-shrink: 0;
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
  }

  .none {
    margin: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }
</style>
