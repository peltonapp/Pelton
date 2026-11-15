<script lang="ts">
  // the About page: the mark, version and update state up top, the project's
  // links as tiles, then updates and support. links open in the system
  // browser, never an in-app webview.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import globeIcon from '@phosphor-icons/core/assets/duotone/globe-duotone.svg?raw'
  import docsIcon from '@phosphor-icons/core/assets/duotone/book-open-duotone.svg?raw'
  import githubIcon from '@phosphor-icons/core/assets/duotone/github-logo-duotone.svg?raw'
  import bugIcon from '@phosphor-icons/core/assets/duotone/bug-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-duotone.svg?raw'
  import scalesIcon from '@phosphor-icons/core/assets/duotone/scales-duotone.svg?raw'
  import scrollIcon from '@phosphor-icons/core/assets/duotone/scroll-duotone.svg?raw'
  import certificateIcon from '@phosphor-icons/core/assets/duotone/certificate-duotone.svg?raw'
  import folderIcon from '@phosphor-icons/core/assets/duotone/folder-open-duotone.svg?raw'
  import clipboardIcon from '@phosphor-icons/core/assets/duotone/clipboard-text-duotone.svg?raw'
  import refreshIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import outIcon from '@phosphor-icons/core/assets/regular/arrow-up-right.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import Modal from '../../common/Modal.svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import peltonLogo from '../../../assets/images/icons/pelton-logo.png'
  import nightlyLogo from '../../../assets/images/icons/pelton-nightly-logo.png'
  import { seasonalImage } from '../../../lib/seasonalmarkimages'
  import { openURL } from '../../../lib/browser'
  import { APP } from '../../../lib/app-info'
  import {
    appVersion,
    checkForUpdates,
    defaultMailClientStatus,
    setDefaultMailClient,
    isNightly,
    getDiagnostics,
    openLogFolder,
    type UpdateCheckResult,
  } from '../../../lib/api'
  import { onUpdateAvailable } from '../../../lib/events'
  import { prefs, setUpdateCheckFrequency } from '../../../stores/prefs'
  import { toastError, errorMessage } from '../../../stores/toast'
  import { termsUrl } from '../../../lib/liability'
  import { t } from '../../../lib/i18n'
  import '../../../theme/settings2-sections-system.css'

  const dispatch = createEventDispatcher<{ rerunOnboarding: void }>()

  let version: string = APP.version
  let nightly = false
  let mailDefault: { known: boolean; isDefault: boolean } | null = null
  let settingDefault = false
  let copiedDiagnostics = false
  let updateResult: UpdateCheckResult | null = null
  let checkingUpdate = false
  let showLicenses = false
  let showViewportInfo = false
  let viewportInfo = ''

  // on a few days of the year the mark is a different one.
  const season = seasonalImage()
  $: mark = season ? season.src : nightly ? nightlyLogo : peltonLogo
  $: markTile = season?.tile === true

  $: showSetDefault = mailDefault?.known === true && mailDefault.isDefault === false
  $: updateAvailable = updateResult?.checked === true && updateResult.available
  $: updateText = updateAvailable
    ? $t('about.update.available').replace('{version}', updateResult?.latestVersion ?? '')
    : updateResult?.checked
      ? $t('about.update.upToDate')
      : updateResult?.error
        ? $t('about.update.checkFailed')
        : $t('about.update.neverChecked')

  $: updateCheckOptions = [
    { key: 'off', label: $t('about.update.off') },
    { key: 'startup', label: $t('about.update.startup') },
    { key: 'weekly', label: $t('about.update.weekly') },
    { key: 'monthly', label: $t('about.update.monthly') },
  ]

  $: links = [
    { label: $t('about.website'), icon: globeIcon, open: () => openURL(APP.website) },
    { label: $t('about.docs'), icon: docsIcon, open: () => openURL(APP.docs) },
    { label: 'GitHub', icon: githubIcon, open: () => openURL(APP.repo) },
    { label: $t('about.reportIssue'), icon: bugIcon, open: () => openURL(APP.issues) },
    { label: $t('about.contributors'), icon: usersIcon, open: () => openURL(APP.contributors) },
    { label: APP.license, icon: certificateIcon, open: () => openURL(APP.licenseUrl) },
    { label: $t('about.licenses'), icon: scrollIcon, open: () => (showLicenses = true), local: true },
    { label: $t('about.liability'), icon: scalesIcon, open: () => openURL(termsUrl()) },
  ]

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

  const offUpdate = onUpdateAvailable((e) => {
    updateResult = e
  })
  onDestroy(offUpdate)

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
      await refreshMailDefault()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      settingDefault = false
    }
  }

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

  // clicking the version shows the webview's layout viewport, for debugging
  // platform rendering from a screenshot. diagnostic output, so untranslated.
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
</script>

<svelte:window on:resize={showViewportInfo ? refreshViewportInfo : undefined} />

<section class="hero" data-setting="about">
  <img class="mark" class:tile={markTile} src={mark} alt="" draggable="false" />
  <div class="identity">
    <h3 class="name">{nightly ? $t('nightly.appName') : APP.name}</h3>
    <p class="tagline">{$t('about.tagline')}</p>
    <div class="meta">
      <button type="button" class="version" on:click={toggleViewportInfo}>
        {version.startsWith('v') ? version : `v${version}`}
      </button>
      <span class="state" class:available={updateAvailable}>
        <span class="dot" aria-hidden="true"></span>
        {updateText}
      </span>
    </div>
    {#if showViewportInfo}
      <p class="viewport-info">{viewportInfo}</p>
    {/if}
  </div>
</section>

{#if nightly}
  <p class="nightly-note">{$t('common.statusBar.nightlyTitle')}</p>
{/if}

<Card title={$t('about.section.project')} bare>
  <div class="tiles">
    {#each links as link (link.label)}
      <button type="button" class="tile" on:click={link.open}>
        <Ph svg={link.icon} size={18} />
        <span class="tile-label">{link.label}</span>
        {#if !link.local}
          <span class="out"><Ph svg={outIcon} size={12} /></span>
        {/if}
      </button>
    {/each}
  </div>
</Card>

<Card title={$t('about.section.updates')}>
  <Row id="about-updates" label={$t('about.update.label')} hint={$t('about.update.hint')}>
    <Segmented
      label={$t('about.update.label')}
      value={$prefs.updateCheckFrequency}
      options={updateCheckOptions}
      on:change={(e) => setUpdateCheckFrequency(e.detail)}
    />
  </Row>
  <Row id="about-check-now" label={updateText}>
    {#if updateAvailable && updateResult}
      {@const releaseUrl = updateResult.releaseUrl}
      <button type="button" class="s2-btn" on:click={() => openURL(releaseUrl)}>
        {$t('about.update.viewRelease')}
      </button>
    {/if}
    <button type="button" class="s2-btn" disabled={checkingUpdate} on:click={checkNow}>
      <span class="spin" class:on={checkingUpdate}><Ph svg={refreshIcon} size={15} /></span>
      {checkingUpdate ? $t('about.update.checking') : $t('about.update.checkNow')}
    </button>
  </Row>
</Card>

<Card title={$t('about.section.support')}>
  {#if showSetDefault}
    <Row id="about-default-mail" label={$t('about.defaultMail.label')}>
      <button type="button" class="s2-btn" disabled={settingDefault} on:click={makeDefaultMail}>
        {$t('about.defaultMail.setButton')}
      </button>
    </Row>
  {/if}
  <Row id="about-diagnostics" label={$t('about.diagnostics.label')} hint={$t('about.diagnostics.hint')}>
    <button type="button" class="s2-btn" on:click={onOpenLogFolder}>
      <Ph svg={folderIcon} size={15} />
      {$t('settingsPanel.button.openLogFolder')}
    </button>
    <button type="button" class="s2-btn" on:click={onCopyDiagnostics}>
      <Ph svg={clipboardIcon} size={15} />
      {copiedDiagnostics ? $t('about.diagnostics.copied') : $t('about.diagnostics.copy')}
    </button>
  </Row>
  <Row id="about-tour" label={$t('about.rerunTourLabel')}>
    <button type="button" class="s2-btn" on:click={() => dispatch('rerunOnboarding')}>
      {$t('about.rerunOnboarding')}
    </button>
  </Row>
</Card>

<p class="credit">{$t('about.madeBy')} {APP.author}. {$t('about.builtWith')}</p>

{#if showLicenses}
  <Modal title={$t('about.licenses')} size="large" on:close={() => (showLicenses = false)}>
    {#await import('../../settings/LicensesView.svelte') then m}
      <svelte:component this={m.default} />
    {/await}
  </Modal>
{/if}

<style>
  .hero {
    display: flex;
    align-items: center;
    gap: var(--space-5);
    margin-bottom: var(--space-6);
    padding: var(--space-5);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .mark {
    flex-shrink: 0;
    height: 68px;
    width: auto;
    animation: settle var(--motion-slow, 320ms) var(--ease-out) both;
  }

  .mark.tile {
    height: 84px;
    border-radius: var(--radius-card);
    background: #000;
  }

  @keyframes settle {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.94);
    }
  }

  .identity {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .name {
    margin: 0;
    font-size: var(--fz-title, 22px);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.01em;
    color: var(--text-primary);
  }

  .tagline {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }

  .version {
    padding: 2px var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    cursor: default;
    user-select: text;
    -webkit-user-select: text;
  }

  .state {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text-tertiary);
  }

  .state.available {
    color: var(--accent);
  }

  .state.available .dot {
    background: var(--accent);
  }

  .viewport-info {
    margin: var(--space-1) 0 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    user-select: text;
    -webkit-user-select: text;
  }

  .nightly-note {
    margin: 0 0 var(--space-6);
    padding: var(--space-3) var(--space-4);
    border-radius: var(--radius-control);
    background: var(--nightly-bg);
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: var(--space-2);
  }

  .tile {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-width: 0;
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .tile:hover {
    border-color: var(--border-strong, var(--border-default));
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .tile-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .out {
    display: inline-flex;
    color: var(--text-tertiary);
    transition: transform var(--motion-fast) var(--ease-out);
  }

  .tile:hover .out {
    transform: translate(1px, -1px);
  }

  .spin {
    display: inline-flex;
  }

  .spin.on {
    animation: turn 900ms linear infinite;
  }

  @keyframes turn {
    to {
      transform: rotate(360deg);
    }
  }

  .credit {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  @media (prefers-reduced-motion: reduce) {
    .mark,
    .spin.on {
      animation: none;
    }
  }
</style>
