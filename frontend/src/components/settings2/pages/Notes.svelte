<script lang="ts">
  // the Notes page: which folder holds the notes, where daily notes go and
  // how they are named, the template a daily note starts from, the folders of
  // templates and pasted images, how a note opens and how wide its text runs.
  import { onMount } from 'svelte'
  import '../../calendar/controls.css'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import Select from '../../common/Select.svelte'
  import { vaultChooseFolder, vaultUseDefaultFolder } from '../../../lib/api'
  import { dailyNotePath } from '../../../lib/notetemplate'
  import { prefs, setNotesEditorMode, setNotesLineWidth, setNotesPref, setNotesVaultPref, type NotesPrefKey } from '../../../stores/prefs'
  import { reloadVault, templateNotes, vaultIndex, vaultSwitched } from '../../../stores/notes'
  import { errorMessage, toastError } from '../../../stores/toast'
  import { locale, t } from '../../../lib/i18n'
  import type { NoteEditorMode, ReaderWidth } from '../../../lib/types'

  onMount(() => {
    void reloadVault()
  })

  $: folder = $vaultIndex?.folder ?? ''
  $: example = dailyNotePath(new Date(), $prefs.notesDailyFolder, $prefs.notesDailyFormat, $locale)
  $: templateItems = [{ value: '', label: $t('notesSettings.dailyTemplate.builtin') }, ...$templateNotes.map((n) => ({ value: n.path, label: n.title }))]
  $: modeOptions = (['live', 'source', 'reading'] as const).map((key) => ({ key, label: $t(`notes.mode.${key}`) }))
  $: widthOptions = (['narrow', 'medium', 'full'] as const).map((key) => ({ key, label: $t(`notesSettings.width.${key}`) }))

  async function choose(): Promise<void> {
    try {
      const info = await vaultChooseFolder()
      if (info.error) {
        toastError(info.error)
      }
      if (info.folder) {
        setNotesVaultPref(info.folder)
        await vaultSwitched(info)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function useDefault(): Promise<void> {
    try {
      const info = await vaultUseDefaultFolder()
      setNotesVaultPref('')
      await vaultSwitched(info)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function text(key: NotesPrefKey, value: string): void {
    const clean = value.trim().replace(/^\/+|\/+$/g, '')
    if (clean !== $prefs[key]) {
      setNotesPref(key, clean)
    }
  }
</script>

<Card title={$t('notesSettings.folder')}>
  <Row id="notes-vault" label={$t('notesSettings.vault')} hint={$t('notesSettings.vault.hint')} stacked>
    <div class="vault">
      <code class="path" title={folder}>{folder || $t('notesSettings.vault.none')}</code>
      <div class="buttons">
        <button type="button" class="cal-btn" on:click={() => void choose()}>{$t('notes.chooseFolder')}</button>
        {#if $prefs.notesVault}
          <button type="button" class="cal-btn" on:click={() => void useDefault()}>{$t('notesSettings.vault.default')}</button>
        {/if}
      </div>
    </div>
  </Row>
  <Row id="notes-templates" label={$t('notesSettings.templates')} hint={$t('notesSettings.templates.hint')}>
    <input class="cal-field field" value={$prefs.notesTemplatesFolder} aria-label={$t('notesSettings.templates')} on:change={(e) => text('notesTemplatesFolder', e.currentTarget.value)} />
  </Row>
  <Row id="notes-attachments" label={$t('notesSettings.attachments')} hint={$t('notesSettings.attachments.hint')}>
    <input class="cal-field field" value={$prefs.notesAttachmentsFolder} aria-label={$t('notesSettings.attachments')} on:change={(e) => text('notesAttachmentsFolder', e.currentTarget.value)} />
  </Row>
</Card>

<Card title={$t('notesSettings.daily')}>
  <Row id="notes-daily-folder" label={$t('notesSettings.dailyFolder')} hint={$t('notesSettings.dailyFolder.hint')}>
    <input class="cal-field field" value={$prefs.notesDailyFolder} aria-label={$t('notesSettings.dailyFolder')} on:change={(e) => text('notesDailyFolder', e.currentTarget.value)} />
  </Row>
  <Row id="notes-daily-format" label={$t('notesSettings.dailyFormat')} hint={$t('notesSettings.dailyFormat.hint').replace('{example}', example)}>
    <input
      class="cal-field field mono"
      value={$prefs.notesDailyFormat}
      aria-label={$t('notesSettings.dailyFormat')}
      on:change={(e) => setNotesPref('notesDailyFormat', e.currentTarget.value.trim() || 'YYYY-MM-DD')}
    />
  </Row>
  <Row id="notes-daily-template" label={$t('notesSettings.dailyTemplate')} hint={$t('notesSettings.dailyTemplate.hint')}>
    <Select value={$prefs.notesDailyTemplate} items={templateItems} ariaLabel={$t('notesSettings.dailyTemplate')} on:change={(e) => setNotesPref('notesDailyTemplate', e.detail)} />
  </Row>
</Card>

<Card title={$t('notesSettings.editor')}>
  <Row id="notes-editor-mode" label={$t('notesSettings.mode')} hint={$t('notesSettings.mode.hint')}>
    <Segmented value={$prefs.notesEditorMode} options={modeOptions} label={$t('notesSettings.mode')} on:change={(e) => setNotesEditorMode(e.detail as NoteEditorMode)} />
  </Row>
  <Row id="notes-line-width" label={$t('notesSettings.width')} hint={$t('notesSettings.width.hint')}>
    <Segmented value={$prefs.notesLineWidth} options={widthOptions} label={$t('notesSettings.width')} on:change={(e) => setNotesLineWidth(e.detail as ReaderWidth)} />
  </Row>
</Card>

<style>
  .vault {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
  }

  .path {
    flex: 1;
    min-width: 0;
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .buttons {
    display: flex;
    gap: var(--space-2);
  }

  .field {
    width: calc(var(--cal-sidebar-width) * 0.9);
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .mono {
    font-family: var(--font-mono);
  }
</style>
