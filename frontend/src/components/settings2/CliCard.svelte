<script lang="ts">
  // command line access: the switch that lets the pelton command talk to
  // Pelton, one switch per capability, installing the command, and the log
  // of what it did. The backend enforces every switch; this only shows them.
  import { onDestroy, onMount } from 'svelte'
  import Card from './ui/Card.svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Select from '../common/Select.svelte'
  import {
    chooseCLIInstallDir,
    clearCLILog,
    cliInstallInfo,
    cliLog,
    getCLIConfig,
    installCLI,
    onCLILog,
    setCLIEnabled,
    setCLIPermission,
    type CLIConfig,
    type CLIInstallInfo,
    type CLILogEntry,
  } from '../../lib/cliapi'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  let cfg: CLIConfig | null = null
  let log: CLILogEntry[] = []
  let install: CLIInstallInfo | null = null
  let dir = ''
  let command = ''
  let busy = false
  let stop: (() => void) | undefined

  $: dirItems = (install?.dirs ?? []).map((d) => ({ value: d.dir, label: d.dir }))
  $: if (install && !dir && install.dirs.length > 0) dir = install.dirs[0].dir

  onMount(() => {
    void reload()
    stop = onCLILog(() => void reloadLog())
  })
  onDestroy(() => stop?.())

  async function reload(): Promise<void> {
    try {
      ;[cfg, log, install] = await Promise.all([getCLIConfig(), cliLog(), cliInstallInfo()])
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function reloadLog(): Promise<void> {
    try {
      log = await cliLog()
    } catch {
      // the log stays as it was shown; the next change reloads it.
    }
  }

  async function run(change: () => Promise<unknown>): Promise<void> {
    busy = true
    try {
      await change()
      cfg = await getCLIConfig()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function choose(): Promise<void> {
    try {
      const picked = await chooseCLIInstallDir()
      if (picked) {
        dir = picked
        await doInstall()
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function doInstall(): Promise<void> {
    if (!dir) {
      return
    }
    busy = true
    command = ''
    try {
      const res = await installCLI(dir)
      if (res.command) {
        command = res.command
      } else {
        toastSuccess($t('cli.installed').replace('{path}', res.path))
        install = await cliInstallInfo()
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }
</script>

<Card title={$t('cli.title')} description={$t('cli.hint')} id="cli">
  <Row id="cli-enable" label={$t('cli.enable')}>
    <ToggleSwitch checked={cfg?.enabled ?? false} disabled={busy || !cfg} label={$t('cli.enable')} on:change={(e) => void run(() => setCLIEnabled(e.detail))} />
  </Row>
  {#if cfg?.enabled}
    <Row label={$t('cli.files')} hint={$t('cli.filesHint')} stacked>
      <code class="path">{cfg.socket}</code>
      <code class="path">{cfg.tokenFile}</code>
    </Row>
    <p class="note">{$t('cli.capsHint')}</p>
    {#each cfg.permissions as perm (perm.name)}
      <Row id={`cli-${perm.name}`} label={$t(`cli.cap.${perm.name}`)}>
        <ToggleSwitch checked={perm.allowed} disabled={busy} label={$t(`cli.cap.${perm.name}`)} on:change={(e) => void run(() => setCLIPermission(perm.name, e.detail))} />
      </Row>
    {/each}
  {/if}
  {#if install}
    <Row id="cli-install" label={$t('cli.install')} hint={$t('cli.installHint')} stacked>
      {#if !install.supported}
        <p class="text">{$t('cli.windows')}</p>
      {:else if install.onPath}
        <p class="text">{$t('cli.onPath').replace('{path}', install.onPath)}</p>
      {:else}
        <div class="buttons">
          {#if dirItems.length > 0}
            <Select bind:value={dir} items={dirItems} ariaLabel={$t('cli.install')} />
            <button type="button" class="s2-btn" disabled={busy} on:click={() => void doInstall()}>{$t('cli.installButton')}</button>
          {/if}
          <button type="button" class="s2-btn" disabled={busy} on:click={() => void choose()}>{$t('cli.chooseFolder')}</button>
        </div>
        {#if command}
          <p class="text">{$t('cli.installCommand')}</p>
          <code class="path">{command}</code>
        {/if}
      {/if}
    </Row>
  {/if}
  <Row id="cli-log" label={$t('cli.log')} stacked>
    {#if log.length === 0}
      <p class="text">{$t('cli.logEmpty')}</p>
    {:else}
      <ul class="log" aria-label={$t('cli.log')}>
        {#each log as entry (entry.id)}
          <li>
            <span class="when">{entry.when}</span>
            <span class="what">{entry.command}{entry.touched ? `: ${entry.touched}` : ''}</span>
            <span class="result" class:failed={entry.result !== 'ok'}>{entry.result}{entry.switch ? ` (${entry.switch})` : ''}</span>
          </li>
        {/each}
      </ul>
      <div class="buttons">
        <button type="button" class="s2-btn" on:click={() => void clearCLILog().then(reloadLog).catch((err) => toastError(errorMessage(err)))}>{$t('cli.logClear')}</button>
      </div>
    {/if}
  </Row>
</Card>

<style>
  .note,
  .text {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .note {
    padding: var(--space-3) var(--space-4);
  }

  .path {
    display: block;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    overflow-wrap: anywhere;
    user-select: text;
  }

  .buttons {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }

  .log {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-height: 240px;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
    font-size: var(--fz-meta);
  }

  .log li {
    display: flex;
    gap: var(--space-3);
  }

  .when {
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .what {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
    color: var(--text-secondary);
  }

  .result {
    flex-shrink: 0;
    font-family: var(--font-mono);
    color: var(--text-tertiary);
  }

  .result.failed {
    color: var(--danger);
  }
</style>
