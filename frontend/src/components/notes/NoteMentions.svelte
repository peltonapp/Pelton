<script lang="ts">
  // a quiet line under a mail, event, contact or task: the notes that link to
  // it, each opening in Notes, and a button that makes a new note about it
  // with the link filled in. with no notes yet it is only the button.
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import plusIcon from '@phosphor-icons/core/assets/regular/plus.svg?raw'
  import Ph from '../common/Ph.svelte'
  import { parsePeltonUrl } from '../../lib/peltonlinks'
  import { newNoteAbout, openNote, vaultNotes, type NoteAbout } from '../../stores/notes'
  import { openSpace } from '../../stores/space'
  import { t } from '../../lib/i18n'

  /** What the line is about. id empty hides it: there is nothing to link to. */
  export let about: NoteAbout
  /** Other kinds that count as links to the same object, such as a mail's conversation. */
  export let alsoKinds: NoteAbout['kind'][] = []

  $: kinds = new Set([about.kind, ...alsoKinds])
  $: linked = about.id
    ? $vaultNotes.filter((n) =>
        n.links.some((l) => {
          if (l.kind !== 'pelton') {
            return false
          }
          const p = parsePeltonUrl(l.target)
          return !!p && kinds.has(p.kind) && p.id === about.id
        }),
      )
    : []

  function show(path: string): void {
    openNote(path)
    openSpace('notes')
  }
</script>

{#if about.id}
  <div class="mentions" aria-label={$t('notes.mentions.label')}>
    <Ph svg={noteIcon} size={13} />
    {#each linked as n (n.path)}
      <button type="button" class="note" title={n.path} on:click={() => show(n.path)}>{n.title}</button>
    {/each}
    <button type="button" class="new" on:click={() => void newNoteAbout(about)}>
      <Ph svg={plusIcon} size={11} />
      <span>{about.kind === 'event' ? $t('notes.mentions.meeting') : $t('notes.mentions.new')}</span>
    </button>
  </div>
{/if}

<style>
  .mentions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .note,
  .new {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    max-width: 100%;
    padding: 0 var(--space-2);
    height: var(--chip-height);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    cursor: var(--cursor-action);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .note:hover,
  .new:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .new {
    border-style: dashed;
    color: var(--text-tertiary);
  }
</style>
