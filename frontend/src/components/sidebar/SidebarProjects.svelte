<script lang="ts">
  // the Projects group of the mail sidebar in the new design: the active
  // projects, each a place to drop mail on, which files the whole
  // conversation under it. a click opens the project. the group only shows
  // when there are active projects.
  import SidebarRow from './SidebarRow.svelte'
  import { projectColor } from '../../lib/projectlogic'
  import { maildrop } from '../../stores/maildrag'
  import { activeProjects, fileMail, openProject } from '../../stores/projects'
  import { t } from '../../lib/i18n'
  import type { Project } from '../../lib/types'

  function dropFor(p: Project) {
    return {
      onDrop: (items: { id: number }[]) => void fileMail(p.id, items.map((m) => m.id)),
      hint: () => $t('projects.drop.into').replace('{name}', p.name),
    }
  }
</script>

{#if $activeProjects.length > 0}
  <nav class="projects" aria-label={$t('space.projects')}>
    <header class="group-head">{$t('space.projects')}</header>
    {#each $activeProjects as p (p.id)}
      <div class="target" use:maildrop={dropFor(p)}>
        <SidebarRow label={p.name} title={p.description} on:select={() => openProject(p.id, 'mail')}>
          <span class="dot" style={`background:${projectColor(p)}`}></span>
        </SidebarRow>
      </div>
    {/each}
  </nav>
{/if}

<style>
  .projects {
    display: flex;
    flex-direction: column;
    gap: 1px;
    margin: var(--space-2) 0 var(--space-3);
  }

  .group-head {
    padding: var(--space-2) var(--space-3);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .target {
    border-radius: var(--radius-control);
  }

  .dot {
    display: inline-block;
    width: var(--space-2);
    height: var(--space-2);
    margin: 0 calc(var(--space-1) / 2);
    border-radius: 50%;
  }
</style>
