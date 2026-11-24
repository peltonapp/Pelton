<script lang="ts">
  // a quiet line at the top of the reader about projects: which project the
  // conversation is filed under, or, when its sender is one of a project's
  // people, an offer to file it there. the offer is taken with one click or
  // the add-to-project key, and waved away for good per conversation. the
  // matching runs here, on what is already loaded; nothing leaves the app.
  import { onDestroy } from 'svelte'
  import { fly } from 'svelte/transition'
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { projectColor, suggestProjects } from '../../lib/projectlogic'
  import { shownSuggestion } from '../../lib/projectpicker'
  import { dismissSuggestion, fileMail, openProject, projectDismissals, projectMailMarks, projects } from '../../stores/projects'
  import { prefs } from '../../stores/prefs'
  import { menuHint } from '../../stores/shortcuts'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { MessageDetail } from '../../lib/types'

  /** The message open in the reader. */
  export let detail: MessageDetail

  $: filedIds = $projectMailMarks.get(detail.threadId) ?? []
  $: filed = $projects.filter((p) => filedIds.includes(p.id))
  $: offers = $prefs.projectSuggestions ? suggestProjects(detail.fromAddress, detail.threadId, $projects, $projectMailMarks, $projectDismissals) : []
  $: offer = offers[0] ?? null
  $: shownSuggestion.set(offer ? { threadId: detail.threadId, projectId: offer.id, messageIds: [detail.id] } : null)
  $: key = menuHint('add-to-project')

  onDestroy(() => shownSuggestion.set(null))

  function accept(id: number): void {
    void fileMail(id, [detail.id])
  }
</script>

{#if filed.length > 0}
  <div class="hint filed" in:fly={{ y: -4, duration: dur(motion.fast) }}>
    <Ph svg={kanbanIcon} size={13} />
    <span class="lead">{$t('projects.hint.in')}</span>
    {#each filed as p (p.id)}
      <button type="button" class="chip" style={`--project:${projectColor(p)}`} on:click={() => openProject(p.id, 'mail')}>
        <span class="dot" aria-hidden="true"></span>{p.name}
      </button>
    {/each}
  </div>
{:else if offer}
  <div class="hint offer" role="status" in:fly={{ y: -4, duration: dur(motion.base) }}>
    <Ph svg={kanbanIcon} size={13} />
    <span class="lead">
      {$t('projects.hint.offer').replace('{name}', offer.name)}
    </span>
    <button type="button" class="act" style={`--project:${projectColor(offer)}`} on:click={() => accept(offer.id)}>
      <span class="dot" aria-hidden="true"></span>{$t('projects.hint.add')}
      {#if key}<kbd>{key}</kbd>{/if}
    </button>
    {#each offers.slice(1, 3) as other (other.id)}
      <button type="button" class="alt" on:click={() => accept(other.id)}>{$t('projects.hint.or').replace('{name}', other.name)}</button>
    {/each}
    <span class="spacer"></span>
    <button type="button" class="dismiss" aria-label={$t('projects.hint.dismiss')} title={$t('projects.hint.dismiss')} on:click={() => void dismissSuggestion(detail.threadId)}>
      <Ph svg={closeIcon} size={11} />
    </button>
  </div>
{/if}

<style>
  .hint {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-3);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .hint > :global(svg) {
    color: var(--text-tertiary);
  }

  .filed {
    padding-block: var(--space-1);
  }

  .offer {
    border: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
  }

  .lead {
    min-width: 0;
  }

  .chip,
  .act,
  .alt {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .chip:hover,
  .act:hover,
  .alt:hover {
    border-color: var(--border-strong);
    background: var(--surface-hover);
  }

  .alt {
    border-color: transparent;
    background: transparent;
    color: var(--text-secondary);
  }

  .dot {
    width: var(--space-2);
    height: var(--space-2);
    border-radius: 50%;
    background: var(--project);
  }

  kbd {
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .spacer {
    flex: 1;
  }

  .dismiss {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--chip-height);
    height: var(--chip-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }

  .dismiss:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
