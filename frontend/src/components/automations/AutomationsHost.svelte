<script lang="ts">
  // what the automations need on screen whichever space or design is showing:
  // the questions running scripts ask (allow a risky call, their own
  // questions, catch up on mail that came while Pelton was closed), one at a
  // time, the import dialog, the result panel, the progress of running
  // scripts, and a script file dropped anywhere on the window. it also keeps
  // the script events and the script list live for the command palette.
  import { onDestroy, onMount } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import ScriptImportDialog from './ScriptImportDialog.svelte'
  import AskDialog from './AskDialog.svelte'
  import ResultPanel from './ResultPanel.svelte'
  import RunProgress from './RunProgress.svelte'
  import { t } from '../../lib/i18n'
  import { onFilesDropped } from '../../lib/events'
  import { scriptExtension } from '../../lib/scripting'
  import {
    answerAsk,
    answerCatchUp,
    answerConfirm,
    catalog,
    confirmImport,
    dismissResult,
    importPreview,
    loadCatalog,
    loadScripts,
    pendingAsks,
    pendingCatchUps,
    pendingConfirms,
    previewImport,
    scriptResults,
    startScriptEvents,
  } from '../../stores/scripts'
  import { errorMessage, toastError } from '../../stores/toast'
  import '../../theme/controls.css'

  let always = false
  let stops: (() => void)[] = []

  onMount(() => {
    stops = [
      startScriptEvents(),
      onFilesDropped((e) => {
        const file = e.paths.find((p) => p.toLowerCase().endsWith(scriptExtension))
        if (file && (e.target === 'app' || e.target === 'automations')) {
          void previewImport(file)
        }
      }),
    ]
    void loadScripts().catch(() => undefined)
  })

  onDestroy(() => stops.forEach((stop) => stop()))

  $: confirm = $pendingConfirms[0] ?? null
  $: ask = $pendingAsks[0] ?? null
  $: catchUp = $pendingCatchUps[0] ?? null
  $: result = $scriptResults[0] ?? null
  $: if (($importPreview || result) && !$catalog) {
    void loadCatalog().catch((err) => toastError(errorMessage(err)))
  }

  function reply(id: string, value: unknown, cancelled: boolean): void {
    void answerAsk(id, value, cancelled).catch((err) => toastError(errorMessage(err)))
  }

  async function answer(allow: boolean): Promise<void> {
    if (!confirm) {
      return
    }
    const id = confirm.id
    const keep = always && !confirm.everyTime
    always = false
    await answerConfirm(id, allow, keep).catch((err) => toastError(errorMessage(err)))
  }

  async function doImport(): Promise<void> {
    try {
      await confirmImport()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

{#if confirm}
  <Modal title={$t('scripts.confirm.title').replace('{name}', confirm.scriptName)} hint={$t('scripts.confirm.hint')} size="small" on:close={() => void answer(false)}>
    <pre class="summary">{confirm.summary}</pre>
    {#if !confirm.everyTime}
      <label class="always">
        <input type="checkbox" bind:checked={always} />
        {$t('scripts.confirm.always')}
      </label>
    {/if}
    <svelte:fragment slot="footer">
      <button type="button" class="cal-btn" on:click={() => void answer(false)}>{$t('scripts.confirm.deny')}</button>
      <button type="button" class="cal-btn primary" on:click={() => void answer(true)}>{$t('scripts.confirm.allow')}</button>
    </svelte:fragment>
  </Modal>
{:else if ask}
  {#key ask.id}
    <AskDialog {ask} on:answer={(e) => reply(ask.id, e.detail, false)} on:cancel={() => reply(ask.id, null, true)} />
  {/key}
{:else if catchUp}
  <Modal title={$t('scripts.catchUpAsk.title').replace('{name}', catchUp.name)} size="small" on:close={() => void answerCatchUp(catchUp.scriptId, false)}>
    <p class="text">{$t('scripts.catchUpAsk.body').replace('{n}', String(catchUp.count))}</p>
    <svelte:fragment slot="footer">
      <button type="button" class="cal-btn" on:click={() => void answerCatchUp(catchUp.scriptId, false)}>{$t('scripts.catchUpAsk.skip')}</button>
      <button type="button" class="cal-btn primary" on:click={() => void answerCatchUp(catchUp.scriptId, true)}>{$t('scripts.catchUpAsk.run')}</button>
    </svelte:fragment>
  </Modal>
{:else if $importPreview && $catalog}
  <ScriptImportDialog file={$importPreview} catalog={$catalog} on:close={() => importPreview.set(null)} on:import={() => void doImport()} />
{:else if result && $catalog}
  {#key result.id}
    <ResultPanel {result} catalog={$catalog} on:close={() => dismissResult(result.id)} />
  {/key}
{/if}

<RunProgress />

<style>
  .summary {
    margin: 0;
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .always {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-3);
    font-size: var(--fz-label);
  }

  .always input {
    accent-color: var(--accent);
  }

  .text {
    margin: 0;
    font-size: var(--fz-label);
  }
</style>
