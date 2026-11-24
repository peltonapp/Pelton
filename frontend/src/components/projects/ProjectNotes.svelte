<script lang="ts">
  // a project's Notes tab. with a notes folder open the notes are Markdown
  // files there, edited with the Notes editor; without one they are kept in
  // the database and edited here in plain Markdown, until a folder is open and
  // they move into it. which of the two shows is asked again when the notes
  // folder changes.
  import { onDestroy } from 'svelte'
  import ProjectFileNotes from './ProjectFileNotes.svelte'
  import ProjectLocalNotes from './ProjectLocalNotes.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { projectNotes } from '../../lib/projectnotes'
  import { onVaultChanged } from '../../lib/events'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Project } from '../../lib/types'

  /** The project whose notes these are. */
  export let project: Project
  /** A note to open, by id, as a card's link asks. */
  export let focusId: string | null = null

  let files: boolean | null = null
  let checkedFor = 0

  $: if (project.id !== checkedFor) {
    checkedFor = project.id
    files = null
    void check()
  }

  async function check(): Promise<void> {
    const id = project.id
    try {
      const list = await projectNotes().list(id)
      if (id === project.id) {
        files = list.files
      }
    } catch (err) {
      toastError(errorMessage(err))
      files = false
    }
  }

  const off = onVaultChanged((e) => {
    if (e.reset && !files) {
      void check()
    }
  })
  onDestroy(off)
</script>

{#if files === null}
  <div class="loading"><PieProgress label={$t('projects.loading')} /></div>
{:else if files}
  <ProjectFileNotes {project} {focusId} on:focused />
{:else}
  <ProjectLocalNotes {project} {focusId} on:focused />
{/if}

<style>
  .loading {
    display: flex;
    justify-content: center;
    padding: var(--space-5);
  }
</style>
