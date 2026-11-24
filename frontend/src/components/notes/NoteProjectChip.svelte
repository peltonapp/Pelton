<script lang="ts">
  // a quiet chip beside a note's title naming the project its front matter
  // puts it in. it opens the project on its notes. a note in no project, or
  // in one Pelton does not know, shows nothing.
  import kanbanIcon from '@phosphor-icons/core/assets/duotone/kanban-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { projectOfNote } from '../../lib/noteprojects'
  import { projectColor } from '../../lib/projectlogic'
  import { openProject, projects } from '../../stores/projects'
  import { t } from '../../lib/i18n'
  import type { VaultProp } from '../../lib/types'

  /** The note's front matter properties. */
  export let props: VaultProp[] | undefined

  $: project = projectOfNote(props, $projects)
</script>

{#if project}
  <button
    type="button"
    class="chip"
    style={`--chip-color:${projectColor(project)}`}
    title={$t('notes.project.open').replace('{name}', project.name)}
    on:click={() => project && openProject(project.id, 'notes')}
  >
    <Ph svg={kanbanIcon} size={13} />
    <span class="name">{project.name}</span>
  </button>
{/if}

<style>
  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    flex-shrink: 1;
    min-width: 0;
    max-width: calc(var(--cal-sidebar-width) * 0.8);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .chip :global(.ph) {
    flex-shrink: 0;
    color: var(--chip-color);
  }

  .chip:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
