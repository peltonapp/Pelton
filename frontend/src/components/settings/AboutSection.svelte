<script lang="ts">
  // the about block at the bottom of settings: app name, version, links, and a
  // couple of dev/getting-started actions that do not deserve their own left-nav
  // category. links open in the system browser via the wails runtime, never an
  // in-app webview.
  import { onDestroy, onMount } from 'svelte'
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import { IconBrandGithub, IconBug, IconLicense, IconScale, IconRefresh, IconUsers, IconWorld, IconBook2, IconFolderOpen, IconClipboard } from '@tabler/icons-svelte'
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
  import { APP } from '../../lib/app-info'
  import { appVersion, checkForUpdates, defaultMailClientStatus, setDefaultMailClient, isNightly, getDiagnostics, openLogFolder, type UpdateCheckResult } from '../../lib/api'
  import nightlyLogo from '../../assets/images/icons/pelton-nightly-logo.png'
  import { onUpdateAvailable } from '../../lib/events'
  import { prefs, setUpdateCheckFrequency } from '../../stores/prefs'
  import { toastError, errorMessage } from '../../stores/toast'
  import SegmentedSetting from './SegmentedSetting.svelte'
  import { t } from '../../lib/i18n'
  import { termsUrl } from '../../lib/liability'

  const dispatch = createEventDispatcher<{ rerunOnboarding: void }>()

  // the version is injected into the binary at build time; fall back to the
  // static value if the binding is unavailable (e.g. a stale dev build).
  let version: string = APP.version
  // nightly is fixed at build time; it swaps the name and mark here so the
  // about block cannot be mistaken for a real install's.
  let nightly = false
  onMount(async () => {
    nightly = await isNightly().catch(() => false)
    try {
      const v = await appVersion()
      if (v) {
        version = v
      }
    } catch {
      // keep the static fallback.
    }
    void refreshMailDefault()
  })

  // whether Pelton is the default mailto handler. null until checked; known is
  // false where the platform cannot answer, in which case the passive line
  // below stays hidden rather than guessing wrong.
  let mailDefault: { known: boolean; isDefault: boolean } | null = null
  let settingDefault = false
  $: showSetDefault = mailDefault?.known === true && mailDefault.isDefault === false

  async function refreshMailDefault(): Promise<void> {
    try {
      mailDefault = await defaultMailClientStatus()
    } catch {
      mailDefault = null
    }
  }

  async function makeDefaultMail(): Promise<void> {
    settingDefault = true
    try {
      await setDefaultMailClient()
      // macOS shows a confirmation sheet, so the change may land after this
      // resolves; re-checking keeps the line accurate on the platforms that
      // apply it synchronously and is harmless where it does not.
      await refreshMailDefault()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      settingDefault = false
    }
  }

  // diagnostics (#211): the build and platform summary that starts a bug
  // report, and a shortcut to the log folder. Copying is the only way any of
  // this leaves the machine, and it takes a click to do it.
  let copiedDiagnostics = false

  async function onCopyDiagnostics(): Promise<void> {
    try {
      await navigator.clipboard.writeText(await getDiagnostics())
      copiedDiagnostics = true
      setTimeout(() => (copiedDiagnostics = false), 2000)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function onOpenLogFolder(): Promise<void> {
    try {
      await openLogFolder()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // update checking is off by default; it only ever talks to GitHub's public
  // releases api (no telemetry, no other endpoint) to compare tags.
  $: updateCheckOptions = [
    { key: 'off', label: $t('about.update.off') },
    { key: 'startup', label: $t('about.update.startup') },
    { key: 'weekly', label: $t('about.update.weekly') },
    { key: 'monthly', label: $t('about.update.monthly') },
  ]

  let updateResult: UpdateCheckResult | null = null
  let checkingUpdate = false

  const offUpdate = onUpdateAvailable((e) => {
    updateResult = e
  })
  onDestroy(offUpdate)

  function onUpdateFrequency(event: CustomEvent<string>): void {
    setUpdateCheckFrequency(event.detail)
  }

  async function checkNow(): Promise<void> {
    checkingUpdate = true
    try {
      updateResult = await checkForUpdates()
    } catch {
      updateResult = { checked: false, available: false, currentVersion: version, latestVersion: '', releaseUrl: '', error: 'failed' }
    } finally {
      checkingUpdate = false
    }
  }

  // the licenses view is heavy (it pulls the embedded manifest), so it is
  // code-split and only mounted (in a modal) when the user opens it.
  let showLicenses = false

  // clicking the version toggles a small technical readout of the webview's
  // layout viewport. It exists to debug platform rendering issues (a screenshot
  // of it tells us whether the engine's viewport matches the real window, which
  // WebKitGTK has gotten wrong) without needing a dev build on the affected
  // machine. Untranslated on purpose: it is diagnostic output, not ui copy.
  let showViewportInfo = false
  let viewportInfo = ''
  function refreshViewportInfo(): void {
    const de = document.documentElement
    viewportInfo =
      `inner ${window.innerWidth}x${window.innerHeight}` +
      ` / client ${de.clientWidth}x${de.clientHeight}` +
      ` / outer ${window.outerWidth}x${window.outerHeight}` +
      ` / dpr ${window.devicePixelRatio}` +
      ` / zoom ${$prefs.uiScale}`
  }
  function toggleViewportInfo(): void {
    showViewportInfo = !showViewportInfo
    if (showViewportInfo) {
      refreshViewportInfo()
    }
  }

  function open(url: string): void {
    BrowserOpenURL(url)
  }

</script>

<svelte:window on:resize={showViewportInfo ? refreshViewportInfo : undefined} />

<div class="about">
  <div class="identity">
    {#if nightly}
      <img class="mark" src={nightlyLogo} alt="" draggable="false" />
    {/if}
    <span class="name">{nightly ? $t('nightly.appName') : APP.name}</span>
    <button type="button" class="version" on:click={toggleViewportInfo}>{version.startsWith('v') ? version : `v${version}`}</button>
  </div>
  <p class="tagline">{$t('about.tagline')}</p>
  {#if nightly}
    <p class="nightly-note">{$t('common.statusBar.nightlyTitle')}</p>
  {/if}
  {#if showViewportInfo}
    <p class="viewport-info">{viewportInfo}</p>
  {/if}

  <div class="links">
    <button type="button" on:click={() => open(APP.website)}>
      <IconWorld size={15} stroke={1.6} />
      {$t('about.website')}
    </button>
    <button type="button" on:click={() => open(APP.docs)}>
      <IconBook2 size={15} stroke={1.6} />
      {$t('about.docs')}
    </button>
    <button type="button" on:click={() => open(APP.repo)}>
      <IconBrandGithub size={15} stroke={1.6} />
      GitHub
    </button>
    <button type="button" on:click={() => open(APP.issues)}>
      <IconBug size={15} stroke={1.6} />
      {$t('about.reportIssue')}
    </button>
    <button type="button" on:click={() => open(APP.contributors)}>
      <IconUsers size={15} stroke={1.6} />
      {$t('about.contributors')}
    </button>
    <button type="button" on:click={() => open(APP.licenseUrl)}>
      <IconLicense size={15} stroke={1.6} />
      {APP.license}
    </button>
    <button type="button" on:click={() => (showLicenses = true)}>
      <IconLicense size={15} stroke={1.6} />
      {$t('about.licenses')}
    </button>
    <button type="button" on:click={() => open(termsUrl())}>
      <IconScale size={15} stroke={1.6} />
      {$t('about.liability')}
    </button>
  </div>

  {#if showSetDefault}
    <div class="row">
      <span class="row-label">{$t('about.defaultMail.label')}</span>
      <button type="button" class="action-btn" disabled={settingDefault} on:click={makeDefaultMail}>
        {$t('about.defaultMail.setButton')}
      </button>
    </div>
  {/if}

  <div class="row">
    <span class="row-label">{$t('about.rerunTourLabel')}</span>
    <button type="button" class="action-btn" on:click={() => dispatch('rerunOnboarding')}>
      {$t('about.rerunOnboarding')}
    </button>
  </div>

  <div class="row">
    <span class="row-label">{$t('about.diagnostics.label')}</span>
    <div class="diag-actions">
      <button type="button" class="action-btn" on:click={onOpenLogFolder}>
        <IconFolderOpen size={14} stroke={1.8} />
        {$t('settingsPanel.button.openLogFolder')}
      </button>
      <button type="button" class="action-btn" on:click={onCopyDiagnostics}>
        <IconClipboard size={14} stroke={1.8} />
        {copiedDiagnostics ? $t('about.diagnostics.copied') : $t('about.diagnostics.copy')}
      </button>
    </div>
  </div>
  <p class="hint">{$t('about.diagnostics.hint')}</p>

  <div class="update-block">
    <SegmentedSetting label={$t('about.update.label')} value={$prefs.updateCheckFrequency} options={updateCheckOptions} on:change={onUpdateFrequency} />
    <p class="hint">{$t('about.update.hint')}</p>

    <div class="row">
      <span class="row-label">
        {#if updateResult?.checked && updateResult.available}
          {$t('about.update.available').replace('{version}', updateResult.latestVersion)}
        {:else if updateResult?.checked}
          {$t('about.update.upToDate')}
        {:else if updateResult && updateResult.error}
          {$t('about.update.checkFailed')}
        {:else}
          {$t('about.update.neverChecked')}
        {/if}
      </span>
      <div class="update-actions">
        {#if updateResult?.checked && updateResult.available}
          {@const releaseUrl = updateResult.releaseUrl}
          <button type="button" class="action-btn" on:click={() => open(releaseUrl)}>
            {$t('about.update.viewRelease')}
          </button>
        {/if}
        <button type="button" class="action-btn" disabled={checkingUpdate} on:click={checkNow}>
          <IconRefresh size={14} stroke={1.8} />
          {checkingUpdate ? $t('about.update.checking') : $t('about.update.checkNow')}
        </button>
      </div>
    </div>
  </div>

  <p class="credit">{$t('about.madeBy')} {APP.author}. {$t('about.builtWith')}</p>
</div>

{#if showLicenses}
  <Modal title={$t('about.licenses')} size="large" on:close={() => (showLicenses = false)}>
    {#await import('./LicensesView.svelte') then m}
      <svelte:component this={m.default} />
    {/await}
  </Modal>
{/if}

<style>
  .about {
    padding: var(--space-2) 0;
  }

  .identity {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
  }

  .mark {
    width: 28px;
    height: 28px;
    align-self: center;
  }

  .nightly-note {
    margin: var(--space-3) 0 0;
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-control);
    background: var(--nightly-bg);
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .name {
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  /* the version is a button (it toggles the viewport readout) but should keep
     looking like the plain text label it always was. */
  .version {
    padding: 0;
    border: none;
    background: none;
    cursor: default;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .viewport-info {
    margin: var(--space-1) 0 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    user-select: text;
    -webkit-user-select: text;
  }

  .tagline {
    margin: var(--space-2) 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .links {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .links button {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .links button:hover {
    background: var(--surface-hover);
  }

  .credit {
    margin: var(--space-4) 0 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-3) 0;
    border-top: var(--hairline) solid var(--border-subtle);
    margin-top: var(--space-3);
  }

  .row-label {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: var(--fz-body);
    color: var(--text-primary);
  }

  .update-block {
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
    margin-top: var(--space-3);
  }

  .update-block .row {
    border-top: none;
    margin-top: 0;
    padding-top: var(--space-3);
  }

  .hint {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .update-actions,
  .diag-actions {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .action-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    white-space: nowrap;
  }

  .action-btn:hover {
    background: var(--surface-hover);
  }

  .action-btn:disabled {
    opacity: 0.6;
    cursor: default;
  }

</style>
