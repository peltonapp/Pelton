<script lang="ts">
  // the Automations space: scripts and libraries on the left, the open script,
  // library or the template gallery in the middle and the run log on the
  // right. a script file dropped on it opens the import dialog; "new"
  // (compose) makes a script.
  import { onDestroy, onMount, type ComponentType } from 'svelte'
  import pickIcon from '@phosphor-icons/core/assets/duotone/cursor-click-duotone.svg?raw'
  import errorIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import EmptyState from '../common/EmptyState.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ScriptList from './ScriptList.svelte'
  import ScriptEditor from './ScriptEditor.svelte'
  import LibraryEditor from './LibraryEditor.svelte'
  import TemplateGallery from './TemplateGallery.svelte'
  import RunLog from './RunLog.svelte'
  import ApproveDialog from './ApproveDialog.svelte'
  import BlockEditor from './blocks/BlockEditor.svelte'
  import { t } from '../../lib/i18n'
  import { newInput, type ScriptKind } from '../../lib/scripting'
  import { registerSpaceActions } from '../../stores/space'
  import {
    approvalFor,
    approveAndEnable,
    blockEditor,
    catalog,
    createLibrary,
    createScript,
    galleryOpen,
    libraries,
    loadCatalog,
    loadLibraries,
    loadRuns,
    loadScripts,
    pickImport,
    selectedLibrary,
    selectedScript,
  } from '../../stores/scripts'
  import { errorMessage, toastError } from '../../stores/toast'

  let width = 0
  let narrowAt = 0
  let failed = ''
  let stopActions: (() => void) | null = null

  blockEditor.set(BlockEditor as unknown as ComponentType)

  onMount(async () => {
    narrowAt = parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--automation-narrow')) || 0
    stopActions = registerSpaceActions('automations', { compose: () => void create('blocks') }, { compose: 'scripts.new' })
    try {
      await Promise.all([loadCatalog(), loadScripts(), loadRuns(), loadLibraries()])
    } catch (err) {
      failed = errorMessage(err)
    }
  })

  onDestroy(() => stopActions?.())

  async function create(kind: ScriptKind): Promise<void> {
    try {
      await createScript(newInput(kind, $t(kind === 'lua' ? 'scripts.untitledLua' : 'scripts.untitledBlocks')))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function newLibrary(): Promise<void> {
    const taken = new Set($libraries.map((l) => l.name))
    let name = 'helpers'
    for (let n = 2; taken.has(name); n++) {
      name = `helpers_${n}`
    }
    try {
      await createLibrary({ name, description: '', source: '-- functions other scripts load with require\nlocal M = {}\n\nreturn M\n' })
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function approve(): Promise<void> {
    const s = $approvalFor
    if (!s) {
      return
    }
    try {
      await approveAndEnable(s)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<section class="space" class:narrow={width > 0 && width < narrowAt} aria-label={$t('space.automations')} data-file-drop-target="automations" bind:clientWidth={width}>
  {#if failed}
    <EmptyState kind="error" icon={errorIcon} title={$t('scripts.unavailable')} detail={failed} />
  {:else if !$catalog}
    <div class="loading"><Spinner /></div>
  {:else}
    <div class="panes">
      <ScriptList on:create={(e) => void create(e.detail)} on:import={() => void pickImport().catch((err) => toastError(errorMessage(err)))} on:library={() => void newLibrary()} />
      {#if $selectedScript}
        <ScriptEditor script={$selectedScript} catalog={$catalog} />
      {:else if $selectedLibrary}
        <LibraryEditor library={$selectedLibrary} catalog={$catalog} />
      {:else if $galleryOpen}
        <TemplateGallery catalog={$catalog} />
      {:else}
        <div class="placeholder">
          <EmptyState icon={pickIcon} title={$t('scripts.pick.title')} detail={$t('scripts.pick.detail')} />
        </div>
      {/if}
      <RunLog />
    </div>
    {#if $approvalFor}
      <ApproveDialog script={$approvalFor} catalog={$catalog} on:close={() => approvalFor.set(null)} on:approve={() => void approve()} />
    {/if}
  {/if}
</section>

<style>
  .space {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    min-width: 0;
    background: var(--surface-base);
    color: var(--text-primary);
  }

  .panes {
    flex: 1;
    display: grid;
    grid-template-columns: var(--automation-list-width) minmax(0, 1fr) var(--automation-log-width);
    min-height: 0;
  }

  .narrow .panes {
    grid-template-columns: var(--automation-list-width) minmax(0, 1fr);
  }

  .narrow .panes > :global(:last-child) {
    display: none;
  }

  .placeholder {
    display: flex;
    min-width: 0;
    min-height: 0;
  }

  .loading {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .space:global(.file-drop-target-active) {
    outline: 2px dashed var(--accent);
    outline-offset: calc(-1 * var(--space-2));
  }
</style>
