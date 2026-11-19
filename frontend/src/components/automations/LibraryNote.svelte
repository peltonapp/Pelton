<script lang="ts">
  // on a script's page: the script was switched off because a library it
  // loads changed, with what changed since it was approved. it goes away once
  // the script is switched on again.
  import LibraryDiff from './LibraryDiff.svelte'
  import { t } from '../../lib/i18n'
  import { scriptLibraryNote } from '../../lib/api'
  import type { Script, ScriptLibraryNote } from '../../lib/scripting'

  /** The script open in the editor. */
  export let script: Script

  let note: ScriptLibraryNote | null = null

  $: void load(script.id, script.enabled, script.updatedAt)

  async function load(id: number, enabled: boolean, _updated: string): Promise<void> {
    if (enabled) {
      note = null
      return
    }
    try {
      const n = await scriptLibraryNote(id)
      if (id === script.id) {
        note = n.exists ? n : null
      }
    } catch {
      note = null
    }
  }
</script>

{#if note}
  <section class="note" aria-label={$t('scripts.library.note.label')}>
    <p class="text">
      {$t(note.deleted ? 'scripts.library.note.deleted' : 'scripts.library.note.changed').replace('{name}', note.library).replace('{when}', note.when)}
    </p>
    {#if note.diff.length > 0}
      <details>
        <summary>{$t('scripts.library.note.show')}</summary>
        <LibraryDiff {note} />
      </details>
    {/if}
  </section>
{/if}

<style>
  .note {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-inline-start: 2px solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
  }

  .text {
    margin: 0;
  }

  summary {
    cursor: pointer;
    font-size: var(--fz-meta);
  }
</style>
