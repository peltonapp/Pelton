<script lang="ts">
  // the New design's guided import: pick a source, see what it holds and
  // pick from it, choose where mail goes, then watch it run in the
  // background with pause and cancel, ending in a report. A job left paused,
  // or stopped by quitting the app, is offered again on the first step.
  //
  // The source is only read. Accounts come over as settings: no password is
  // read from the other program, and each account asks for its own once.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import ImportSummary from './ImportSummary.svelte'
  import ImportReport from './ImportReport.svelte'
  import importIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-down-duotone.svg?raw'
  import birdIcon from '@phosphor-icons/core/assets/duotone/bird-duotone.svg?raw'
  import appleIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import filesIcon from '@phosphor-icons/core/assets/duotone/files-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import outlookIcon from '@phosphor-icons/core/assets/duotone/database-duotone.svg?raw'
  import {
    findThunderbirdProfiles,
    chooseImportSource,
    scanImportSource,
    estimateImport,
    importTargets,
    startImportJob,
    pauseImportJob,
    resumeImportJob,
    cancelImportJob,
    importJobs,
  } from '../../lib/api'
  import { onGuidedImport } from '../../lib/events'
  import {
    buildPlan,
    defaultSelection,
    isEmptyPlan,
    progressPercent,
    type ImportDestination,
    type ImportEstimate,
    type ImportInventory,
    type ImportJob,
    type ImportSelection,
    type ImportSourceKind,
    type ImportTarget,
  } from '../../lib/guidedimport'
  import type { ThunderbirdProfile } from '../../lib/types'
  import { formatBytes } from '../../lib/format'
  import { loadSidebar } from '../../stores/accounts'
  import { promptForMissingPasswords } from '../../stores/passwordprompt'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void }>()

  type Step = 'source' | 'summary' | 'destination' | 'run' | 'report'

  let step: Step = 'source'
  let busy = false
  let profiles: ThunderbirdProfile[] | null = null
  let paused: ImportJob | null = null
  let inventory: ImportInventory | null = null
  let selection: ImportSelection | null = null
  let destination: ImportDestination = { mode: 'local', accountId: 0, folder: '' }
  let targets: ImportTarget[] = []
  let estimate: ImportEstimate | null = null
  let job: ImportJob | null = null

  $: running = job?.state === 'running'
  $: plan = inventory && selection ? buildPlan(inventory, selection, destination) : null
  $: target = targets.find((x) => x.accountId === destination.accountId) ?? null
  $: canStart = !!plan && !isEmptyPlan(plan) && (destination.mode === 'local' || destination.accountId > 0)

  const sources: { kind: ImportSourceKind; icon: string; label: string; hint: string }[] = [
    { kind: 'thunderbird', icon: birdIcon, label: 'import.guided.source.thunderbird', hint: 'import.guided.source.thunderbirdHint' },
    { kind: 'applemail', icon: appleIcon, label: 'import.guided.source.applemail', hint: 'import.guided.source.applemailHint' },
    { kind: 'files', icon: filesIcon, label: 'import.guided.source.files', hint: 'import.guided.source.filesHint' },
    { kind: 'folder', icon: folderIcon, label: 'import.guided.source.folder', hint: 'import.guided.source.folderHint' },
    { kind: 'pst', icon: outlookIcon, label: 'import.guided.source.pst', hint: 'import.guided.source.pstHint' },
  ]

  const stop = onGuidedImport((next) => {
    if (!job || next.id !== job.id) {
      return
    }
    job = next
    if (next.state !== 'running') {
      if (next.state === 'paused') {
        return
      }
      step = 'report'
      void loadSidebar()
      if (next.plan.accounts.length > 0) {
        void promptForMissingPasswords()
      }
    }
  })
  onDestroy(stop)

  onMount(async () => {
    try {
      const jobs = await importJobs()
      const latest = jobs[0]
      if (latest?.state === 'running') {
        job = latest
        step = 'run'
      } else if (latest?.state === 'paused') {
        paused = latest
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  })

  async function pickSource(kind: ImportSourceKind): Promise<void> {
    if (kind === 'thunderbird' && profiles === null) {
      busy = true
      try {
        profiles = await findThunderbirdProfiles()
      } catch (err) {
        toastError(errorMessage(err))
        profiles = []
      } finally {
        busy = false
      }
      if (profiles.length > 0) {
        return
      }
    }
    try {
      const paths = await chooseImportSource(kind)
      if (paths.length > 0) {
        await scan(kind, paths)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function chooseProfileFolder(): Promise<void> {
    try {
      const paths = await chooseImportSource('thunderbird')
      if (paths.length > 0) {
        await scan('thunderbird', paths)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function scan(kind: ImportSourceKind, paths: string[]): Promise<void> {
    busy = true
    try {
      inventory = await scanImportSource(kind, paths)
      selection = defaultSelection(inventory)
      step = 'summary'
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function toDestination(): Promise<void> {
    step = 'destination'
    try {
      targets = await importTargets()
    } catch (err) {
      toastError(errorMessage(err))
    }
    await refreshEstimate()
  }

  async function refreshEstimate(): Promise<void> {
    if (!plan) {
      return
    }
    try {
      estimate = await estimateImport(plan)
    } catch {
      estimate = null
    }
  }

  async function start(): Promise<void> {
    if (!plan) {
      return
    }
    busy = true
    try {
      const id = await startImportJob(plan)
      job = {
        id, kind: plan.kind, sourcePath: plan.paths.join('\n'), state: 'running', plan,
        progress: { phase: '', current: '', done: 0, total: estimate?.messages ?? 0, imported: 0, skipped: 0, failed: 0 },
        report: { lines: [] }, error: '', createdAt: '', updatedAt: '',
      }
      step = 'run'
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function control(action: 'pause' | 'resume' | 'cancel', target: ImportJob): Promise<void> {
    try {
      if (action === 'pause') {
        await pauseImportJob(target.id)
      } else if (action === 'resume') {
        await resumeImportJob(target.id)
        job = { ...target, state: 'running' }
        paused = null
        step = 'run'
      } else {
        await cancelImportJob(target.id)
        if (target === paused) {
          paused = null
        }
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function status(j: ImportJob): string {
    if (j.state === 'paused') {
      return $t('import.guided.state.paused')
    }
    const p = j.progress
    if (!p.phase) {
      return $t('import.guided.starting')
    }
    const phase = $t(`import.guided.phase.${p.phase}`)
    return p.current ? `${phase}: ${p.current}` : phase
  }
</script>

<Modal title={$t('import.guided.title')} hint={$t('import.guided.hint')} size="large" closable={!busy} on:close={() => dispatch('close')}>
  <span slot="icon"><Ph svg={importIcon} size={16} /></span>

  <div class="flow">
    {#if step === 'source'}
      {#if paused}
        <div class="resume" role="group" aria-label={$t('import.guided.resume.title')}>
          <span>{$t('import.guided.resume.text').replace('{n}', paused.progress.done.toLocaleString()).replace('{total}', paused.progress.total.toLocaleString())}</span>
          <FlowButton variant="primary" on:click={() => paused && void control('resume', paused)}>{$t('import.guided.resume.button')}</FlowButton>
          <FlowButton variant="quiet" on:click={() => paused && void control('cancel', paused)}>{$t('import.guided.cancel')}</FlowButton>
        </div>
      {/if}
      {#if profiles && profiles.length > 0}
        <section aria-labelledby="tb-profiles">
          <h3 id="tb-profiles">{$t('import.tb.intro')}</h3>
          <ul class="profiles">
            {#each profiles as p (p.path)}
              <li>
                <button type="button" class="choice" disabled={busy} on:click={() => void scan('thunderbird', [p.path])}>
                  <span class="choice-label">{p.name}</span>
                  <span class="choice-hint">{p.path}</span>
                </button>
              </li>
            {/each}
          </ul>
          <FlowButton variant="quiet" disabled={busy} on:click={() => void chooseProfileFolder()}>
            {$t('import.tb.choose')}
          </FlowButton>
          <FlowButton variant="quiet" on:click={() => (profiles = null)}>{$t('import.guided.back')}</FlowButton>
        </section>
      {:else}
        <ul class="sources" aria-label={$t('import.guided.source.title')}>
          {#each sources as s (s.kind)}
            <li>
              <button type="button" class="choice" disabled={busy} on:click={() => void pickSource(s.kind)}>
                <span class="choice-icon"><Ph svg={s.icon} size={18} /></span>
                <span class="choice-text">
                  <span class="choice-label">{$t(s.label)}</span>
                  <span class="choice-hint">{$t(s.hint)}</span>
                </span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      {#if busy}
        <p class="status" role="status">{$t('import.guided.scanning')}</p>
      {/if}
    {:else if step === 'summary' && inventory && selection}
      <ImportSummary {inventory} bind:selection />
    {:else if step === 'destination'}
      <fieldset class="dest">
        <legend>{$t('import.guided.dest.title')}</legend>
        <label class="radio">
          <input type="radio" name="dest" value="local" bind:group={destination.mode} />
          <span>
            <span class="choice-label">{$t('import.guided.dest.local')}</span>
            <span class="choice-hint">{$t('import.guided.dest.localHint')}</span>
          </span>
        </label>
        <label class="radio">
          <input type="radio" name="dest" value="account" bind:group={destination.mode} disabled={targets.length === 0} />
          <span>
            <span class="choice-label">{$t('import.guided.dest.account')}</span>
            <span class="choice-hint">{targets.length === 0 ? $t('import.guided.dest.noAccounts') : $t('import.guided.dest.accountHint')}</span>
          </span>
        </label>
        {#if destination.mode === 'account'}
          <div class="pickers">
            <label>
              <span>{$t('import.guided.dest.pickAccount')}</span>
              <select bind:value={destination.accountId}>
                <option value={0} disabled>{$t('import.guided.dest.pickAccount')}</option>
                {#each targets as tg (tg.accountId)}
                  <option value={tg.accountId}>{tg.email}</option>
                {/each}
              </select>
            </label>
            <label>
              <span>{$t('import.guided.dest.pickFolder')}</span>
              <select bind:value={destination.folder} disabled={!target}>
                <option value="">{$t('import.guided.dest.top')}</option>
                {#each target?.folders ?? [] as f (f)}
                  <option value={f}>{f}</option>
                {/each}
              </select>
            </label>
          </div>
        {/if}
      </fieldset>
      {#if estimate && plan && plan.folders.length > 0}
        <p class="estimate">
          {(destination.mode === 'account' ? $t('import.guided.estimate.upload') : $t('import.guided.estimate.local'))
            .replace('{n}', estimate.messages.toLocaleString())
            .replace('{size}', formatBytes(estimate.bytes))}
        </p>
      {/if}
      {#if plan && (plan.addressBooks.length > 0 || plan.calendars.length > 0)}
        <p class="note">{$t('import.guided.dest.localParts')}</p>
      {/if}
    {:else if step === 'run' && job}
      <section class="run" aria-labelledby="import-run-title">
        <h3 id="import-run-title">{$t('import.guided.running')}</h3>
        <div
          class="bar"
          role="progressbar"
          aria-labelledby="import-run-title"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={progressPercent(job)}
        >
          <div class="fill" style:width="{progressPercent(job)}%"></div>
        </div>
        <p class="status" role="status" aria-live="polite">{status(job)}</p>
        <p class="counts">
          {$t('import.guided.counts')
            .replace('{done}', job.progress.done.toLocaleString())
            .replace('{total}', job.progress.total.toLocaleString())
            .replace('{imported}', job.progress.imported.toLocaleString())
            .replace('{skipped}', job.progress.skipped.toLocaleString())}
        </p>
        <p class="note">{$t('import.guided.background')}</p>
      </section>
    {:else if step === 'report' && job}
      <ImportReport {job} />
    {/if}
  </div>

  <svelte:fragment slot="footer">
    {#if step === 'summary'}
      <FlowButton variant="quiet" on:click={() => (step = 'source')}>{$t('import.guided.back')}</FlowButton>
      <FlowButton variant="primary" disabled={!plan || isEmptyPlan(plan)} on:click={() => void toDestination()}>{$t('import.guided.next')}</FlowButton>
    {:else if step === 'destination'}
      <FlowButton variant="quiet" on:click={() => (step = 'summary')}>{$t('import.guided.back')}</FlowButton>
      <FlowButton variant="primary" disabled={!canStart || busy} on:click={() => void start()}>{$t('import.guided.start')}</FlowButton>
    {:else if step === 'run' && job}
      {#if running}
        <FlowButton variant="quiet" on:click={() => job && void control('cancel', job)}>{$t('import.guided.cancel')}</FlowButton>
        <FlowButton on:click={() => job && void control('pause', job)}>{$t('import.guided.pause')}</FlowButton>
      {:else}
        <FlowButton variant="quiet" on:click={() => job && void control('cancel', job)}>{$t('import.guided.cancel')}</FlowButton>
        <FlowButton variant="primary" on:click={() => job && void control('resume', job)}>{$t('import.guided.resume.button')}</FlowButton>
      {/if}
      <FlowButton variant="quiet" on:click={() => dispatch('close')}>{$t('modal.close')}</FlowButton>
    {:else}
      <FlowButton variant="primary" disabled={busy} on:click={() => dispatch('close')}>
        {step === 'report' ? $t('wizard.done.button') : $t('modal.close')}
      </FlowButton>
    {/if}
  </svelte:fragment>
</Modal>

<style>
  .flow {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-height: 18rem;
  }

  h3 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .sources,
  .profiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(14em, 1fr));
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .profiles {
    grid-template-columns: 1fr;
    margin-bottom: var(--space-2);
  }

  .choice {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    width: 100%;
    min-width: 0;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-tertiary);
    text-align: start;
    cursor: var(--cursor-action);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .choice:hover:not(:disabled),
  .choice:focus-visible {
    border-color: var(--accent);
  }

  .choice:disabled {
    cursor: default;
    opacity: 0.6;
  }

  .profiles .choice {
    flex-direction: column;
    gap: var(--space-1);
  }

  .choice-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--accent);
  }

  .choice-text,
  .radio > span {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .choice-label {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .choice-hint {
    font-size: var(--fz-meta);
    line-height: 1.35;
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
  }

  .resume {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 2px solid var(--accent);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .resume > span {
    flex: 1;
  }

  .dest {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin: 0;
    padding: 0;
    border: none;
  }

  .dest legend {
    padding: 0 0 var(--space-2);
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .radio {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    cursor: var(--cursor-action);
  }

  .pickers {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    padding-inline-start: calc(var(--space-4) + var(--space-2));
  }

  .pickers label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .pickers select {
    min-width: 14em;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
  }

  .estimate,
  .note,
  .status,
  .counts {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-secondary);
  }

  .estimate {
    color: var(--text-primary);
  }

  .note {
    color: var(--text-tertiary);
  }

  .run {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .bar {
    height: var(--space-2);
    border-radius: var(--space-1);
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent);
    transition: width var(--motion-base) var(--ease-out);
  }

  .counts {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }
</style>
