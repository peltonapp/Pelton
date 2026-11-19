<script lang="ts">
  // the one place to bring things into Pelton: a Thunderbird profile, mail
  // files picked or dropped onto the dialog, or a Pelton backup. The add-mailbox
  // wizard, onboarding and settings all open this same dialog. A mail import
  // runs in the backend and reports here, row by row, until it ends.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import ThunderbirdSource from './ThunderbirdSource.svelte'
  import FilesSource from './FilesSource.svelte'
  import BackupSource from './BackupSource.svelte'
  import ImportRun, { type Run, type RunItem } from './ImportRun.svelte'
  import importIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-down-duotone.svg?raw'
  import birdIcon from '@phosphor-icons/core/assets/duotone/bird-duotone.svg?raw'
  import filesIcon from '@phosphor-icons/core/assets/duotone/files-duotone.svg?raw'
  import backupIcon from '@phosphor-icons/core/assets/duotone/archive-duotone.svg?raw'
  import { importMailFiles, importThunderbirdFolders } from '../../lib/api'
  import { onImportProgress, onFilesDropped } from '../../lib/events'
  import { loadSidebar } from '../../stores/accounts'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  type Source = 'thunderbird' | 'files' | 'backup'

  /** The source shown first. */
  export let initialSource: Source = 'thunderbird'

  const dispatch = createEventDispatcher<{ close: void }>()
  const dropTarget = 'mail-import'

  let source: Source = initialSource
  let run: Run | null = null
  $: running = run !== null && !run.finished

  $: sources = [
    { key: 'thunderbird' as Source, icon: birdIcon, label: $t('import.source.thunderbird'), hint: $t('import.source.thunderbirdHint') },
    { key: 'files' as Source, icon: filesIcon, label: $t('import.files.title'), hint: $t('import.source.filesHint') },
    { key: 'backup' as Source, icon: backupIcon, label: $t('import.source.backup'), hint: $t('import.source.backupHint') },
  ]

  const stopProgress = onImportProgress((event) => {
    if (!run) {
      return
    }
    run = { ...run, progress: event, finished: !event.running }
    // the Local Folders section appears the moment the first import lands.
    if (!event.running && !event.error) {
      void loadSidebar()
    }
  })

  const stopDrop = onFilesDropped((event) => {
    if (event.target !== dropTarget || running) {
      return
    }
    source = 'files'
    void importFiles(event.paths)
  })

  onDestroy(() => {
    stopProgress()
    stopDrop()
  })

  async function startRun(items: RunItem[], start: () => Promise<void>): Promise<void> {
    run = { items, progress: null, finished: false }
    try {
      await start()
    } catch (err) {
      run = null
      toastError(errorMessage(err))
    }
  }

  function baseName(path: string): string {
    return path.split(/[\\/]/).pop() || path
  }

  function importFiles(paths: string[]): Promise<void> {
    return startRun(
      paths.map((p) => ({ key: p, label: baseName(p) })),
      () => importMailFiles(paths),
    )
  }

  function importFolders(items: RunItem[]): Promise<void> {
    return startRun(items, () => importThunderbirdFolders(items.map((i) => i.key)))
  }

  // arrow keys move between the sources, as in a native tab bar.
  function onTabKey(event: KeyboardEvent, index: number): void {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0
    if (step === 0) {
      return
    }
    event.preventDefault()
    const dir = document.documentElement.dir === 'rtl' && (event.key === 'ArrowRight' || event.key === 'ArrowLeft') ? -step : step
    const next = sources[(index + dir + sources.length) % sources.length]
    source = next.key
    const list = (event.currentTarget as HTMLElement).parentElement
    list?.querySelector<HTMLElement>(`[data-source="${next.key}"]`)?.focus()
  }
</script>

<Modal title={$t('importExport.importTitle')} hint={$t('import.dialog.hint')} size="large" closable={!running} on:close={() => dispatch('close')}>
  <span slot="icon"><Ph svg={importIcon} size={16} /></span>

  <div class="dialog" data-file-drop-target={dropTarget}>
    <div class="sources" role="tablist" aria-label={$t('importExport.importTitle')}>
      {#each sources as s, index (s.key)}
        <button
          type="button"
          role="tab"
          class="tab"
          class:on={source === s.key}
          aria-selected={source === s.key}
          tabindex={source === s.key ? 0 : -1}
          data-source={s.key}
          on:click={() => (source = s.key)}
          on:keydown={(e) => onTabKey(e, index)}
        >
          <span class="tab-icon"><Ph svg={s.icon} size={18} /></span>
          <span class="tab-text">
            <span class="tab-label">{s.label}</span>
            <span class="tab-hint">{s.hint}</span>
          </span>
        </button>
      {/each}
    </div>

    <div class="panel" role="tabpanel">
      {#if source === 'thunderbird'}
        <ThunderbirdSource {running} on:importFolders={(e) => void importFolders(e.detail)} />
      {:else if source === 'files'}
        <FilesSource {running} on:importFiles={(e) => void importFiles(e.detail)} />
      {:else}
        <BackupSource />
      {/if}
    </div>

    {#if run && source !== 'backup'}
      <ImportRun {run} />
    {/if}

    <div class="drop-veil" aria-hidden="true">{$t('import.drop.release')}</div>
  </div>

  <svelte:fragment slot="footer">
    <FlowButton variant="primary" disabled={running} on:click={() => dispatch('close')}>
      {run?.finished ? $t('wizard.done.button') : $t('modal.close')}
    </FlowButton>
  </svelte:fragment>
</Modal>

<style>
  .dialog {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    min-height: 18rem;
  }

  .sources {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(11em, 1fr));
    gap: var(--space-2);
  }

  .tab {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .tab:hover {
    border-color: var(--border-strong);
  }

  .tab:active {
    transform: scale(0.985);
  }

  .tab.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--accent);
    color: var(--accent);
  }

  .tab-icon {
    display: inline-flex;
    flex-shrink: 0;
  }

  .tab-text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .tab-label {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .tab-hint {
    font-size: var(--fz-meta);
    line-height: 1.35;
    color: var(--text-tertiary);
  }

  .panel {
    animation: panel-in var(--motion-base) var(--ease-out);
  }

  @keyframes panel-in {
    from {
      opacity: 0;
      transform: translateY(var(--space-2));
    }
  }

  /* the wails runtime marks the drop target while files are dragged over it. */
  .drop-veil {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border: var(--hairline) dashed var(--accent);
    border-radius: var(--radius-card);
    background: color-mix(in srgb, var(--surface-overlay) 88%, transparent);
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--accent);
    opacity: 0;
    pointer-events: none;
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  .dialog:global(.file-drop-target-active) > .drop-veil {
    opacity: 1;
  }
</style>
