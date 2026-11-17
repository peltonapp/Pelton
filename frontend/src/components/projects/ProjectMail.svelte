<script lang="ts">
  // a project's conversations as rows of the message list, the newest first.
  // a row opens in the reader; its menu takes it out of the project, which
  // leaves the mail where it is. mail dropped here joins the project.
  import { onDestroy } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fade } from 'svelte/transition'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import mailIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-open-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import MessageRow from '../list/MessageRow.svelte'
  import { listProjectThreads, removeProjectThread } from '../../lib/api'
  import { onMailNew, onProjectsChanged } from '../../lib/events'
  import { openMessageInMail } from '../../lib/projectlinks'
  import { matchesQuery } from '../../lib/spacesearch'
  import { maildrop } from '../../stores/maildrag'
  import { openContextMenu } from '../../stores/contextmenu'
  import { fileMail, loadProjectMarks } from '../../stores/projects'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { Project, ThreadSummary } from '../../lib/types'

  /** The project whose mail this is. */
  export let project: Project

  let threads: ThreadSummary[] = []
  let loaded = false
  let filter = ''
  let selected: number | null = null
  let loadedFor = 0

  $: if (project.id !== loadedFor) {
    loadedFor = project.id
    loaded = false
    threads = []
    void load()
  }
  $: shown = filter.trim()
    ? threads.filter((th) => matchesQuery(filter, [th.head.subject, th.head.fromName, th.head.fromAddress, ...th.participants]))
    : threads

  async function load(): Promise<void> {
    const id = project.id
    try {
      const next = await listProjectThreads(id)
      if (id === project.id) {
        threads = next
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loaded = true
    }
  }

  const offs = [
    onProjectsChanged((e) => {
      if (e.projectIds.length === 0 || e.projectIds.includes(project.id)) {
        void load()
      }
    }),
    onMailNew(() => void load()),
  ]
  onDestroy(() => offs.forEach((off) => off()))

  async function remove(th: ThreadSummary): Promise<void> {
    threads = threads.filter((x) => x.threadId !== th.threadId)
    try {
      await removeProjectThread(project.id, th.threadId)
      void loadProjectMarks()
    } catch (err) {
      toastError(errorMessage(err))
      void load()
    }
  }

  function menu(th: ThreadSummary, x: number, y: number): void {
    selected = th.threadId
    openContextMenu(x, y, [
      { label: $t('projects.mail.open'), action: () => openMessageInMail(th.head) },
      'separator',
      { label: $t('projects.mail.remove'), action: () => void remove(th) },
    ])
  }

  function onKey(event: KeyboardEvent): void {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp' && event.key !== 'Enter') {
      return
    }
    event.preventDefault()
    const at = shown.findIndex((th) => th.threadId === selected)
    if (event.key === 'Enter') {
      if (at >= 0) {
        openMessageInMail(shown[at].head)
      }
      return
    }
    const next = shown[Math.min(Math.max(at + (event.key === 'ArrowDown' ? 1 : -1), 0), shown.length - 1)]
    selected = next?.threadId ?? null
  }

  $: drop = {
    onDrop: (items: { id: number }[]) => void fileMail(project.id, items.map((m) => m.id)),
    hint: () => $t('projects.drop.into').replace('{name}', project.name),
  }
</script>

<section class="mail" aria-label={$t('projects.tab.mail')} use:maildrop={drop}>
  <div class="tools">
    <input
      class="cal-field filter"
      type="search"
      bind:value={filter}
      placeholder={$t('projects.mail.filter')}
      aria-label={$t('projects.mail.filter')}
      on:keydown={(e) => e.key === 'Escape' && filter && (e.stopPropagation(), (filter = ''))}
    />
    <span class="count">{$t('projects.mailCount').replace('{n}', String(threads.length))}</span>
  </div>
  {#if !loaded}
    <div class="loading"><PieProgress label={$t('projects.loading')} /></div>
  {:else if threads.length === 0}
    <EmptyState title={$t('projects.mail.emptyTitle')} detail={$t('projects.mail.emptyDetail')}>
      <Ph svg={mailIcon} size={28} />
    </EmptyState>
  {:else if shown.length === 0}
    <p class="none" in:fade={{ duration: dur(motion.fast) }}>{$t('projects.noMatches')}</p>
  {:else}
    <!-- svelte-ignore a11y-no-noninteractive-tabindex -->
    <div class="list" role="listbox" tabindex="0" aria-label={$t('projects.tab.mail')} on:keydown={onKey}>
      {#each shown as th (th.threadId)}
        <div class="item" animate:flip={{ duration: dur(motion.base) }}>
          <MessageRow
            message={th.head}
            thread={th}
            selected={selected === th.threadId}
            on:click={() => (selected = th.threadId)}
            on:dblclick={() => openMessageInMail(th.head)}
            on:contextmenu={(e) => {
              e.preventDefault()
              menu(th, e.clientX, e.clientY)
            }}
          />
          <div class="row-tools">
            <button type="button" class="cal-btn open" on:click={() => openMessageInMail(th.head)}>{$t('projects.mail.open')}</button>
            <button type="button" class="cal-icon" aria-label={$t('projects.mail.removeNamed').replace('{subject}', th.head.subject || $t('messageList.noSubject'))} title={$t('projects.mail.remove')} on:click={() => void remove(th)}>
              <Ph svg={closeIcon} size={12} />
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .mail {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-width: 0;
  }

  .mail:global(.maildrop-over) {
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .tools {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .filter {
    width: calc(var(--cal-sidebar-width) * 0.9);
    min-width: 0;
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .count {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-6);
  }

  .none {
    margin: var(--space-6) 0;
    text-align: center;
    font-size: var(--fz-list);
    color: var(--text-tertiary);
  }

  .list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    outline: none;
  }

  .item {
    position: relative;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .row-tools {
    position: absolute;
    inset-block-start: 50%;
    inset-inline-end: var(--space-3);
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    transform: translateY(-50%);
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .item:hover .row-tools,
  .item:focus-within .row-tools {
    opacity: 1;
    pointer-events: auto;
  }

  .open {
    height: calc(var(--control-height) - 6px);
  }
</style>
