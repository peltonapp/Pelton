<script lang="ts">
  // the add-mailbox wizard: from an email address to a syncing mailbox in as
  // few decisions as possible. it walks address -> what that provider needs
  // (a password, or an oauth client) -> connection test -> name and colour ->
  // folders -> done. the provider comes from the address, by the presets'
  // domains first and autodiscovery second; the grid of providers stays one
  // click away. every server setting stays editable behind the advanced
  // disclosure, prefilled from discovery. this component is code-split and only
  // loaded when the user opens it.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { get } from 'svelte/store'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/duotone/arrow-left-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import warnIcon from '@phosphor-icons/core/assets/duotone/warning-circle-duotone.svg?raw'
  import importIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-down-duotone.svg?raw'
  import linkIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import InfoTip from '../common/InfoTip.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import CertificateReview from '../common/CertificateReview.svelte'
  import AccountRouteFields from '../common/AccountRouteFields.svelte'
  import WizardProviders from './WizardProviders.svelte'
  import AccountColorPick from './AccountColorPick.svelte'
  import { providerIcon } from './providerIcons'
  import { openURL } from '../../lib/browser'
  import { blankAccountProxy } from '../../lib/proxyroute'
  import {
    discoverConfig,
    testConnection,
    addPasswordAccount,
    addOAuthAccount,
    listFolders,
    setFolderSyncExcluded,
    startAccountSync,
    chooseCAFile,
    setAccountColor,
  } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'
  import { presetById, presetForEmail, isEmailAddress, type ProviderPreset } from '../../lib/providers'
  import { classifyConnectionError, type ConnectionProblem } from '../../lib/connectionError'
  import type { AddAccountRequest, Account, Discovered, Folder, TLSMode, UntrustedCert } from '../../lib/types'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void; added: Account; done: Account }>()

  /** Opens straight into this preset's form instead of asking for the address first. */
  export let initialProviderId: string | null = null
  /** Whether the first step offers importing from another client. */
  export let offerImport = true
  /**
   * Renders inline without its own window and header, for a host that has its
   * own (onboarding). The host then hears "done" once the first sync started
   * instead of the wizard showing its own done screen.
   */
  export let embedded = false

  type Step = 'email' | 'config' | 'oauth' | 'verify' | 'details' | 'working' | 'folders' | 'done' | 'error'
  let step: Step = 'email'

  let showImport = false
  // the form the last submit came from, so the error screen returns there.
  let formStep: 'config' | 'oauth' = 'config'
  let preset: ProviderPreset | null = null
  let error = ''
  let problem: ConnectionProblem = 'unknown'
  let workingMessage = ''
  let testing = false
  let testOk: boolean | null = null
  // the oauth provider autodiscovery says a custom address signs in with, so
  // a Google Workspace domain typed under "Other" is steered to Google sign-in.
  let discoveredOAuth = ''
  // server certificates the last test could not verify, shown for review. The
  // test only logs in once every one of them is trusted or fixed (#446).
  let untrusted: UntrustedCert[] = []
  // the subjects of the CA file in draft.caPem, for showing what was picked.
  let caSubjects: string[] = []
  let color = ''

  let draft: AddAccountRequest = blankDraft()

  function blankDraft(): AddAccountRequest {
    return {
      email: '',
      displayName: '',
      localLabel: '',
      useLocalLabel: false,
      username: '',
      imapHost: '',
      imapPort: 993,
      smtpHost: '',
      smtpPort: 465,
      imapTls: 'ssl',
      smtpTls: 'ssl',
      password: '',
      provider: '',
      clientId: '',
      clientSecret: '',
      trustedCerts: [],
      caPem: '',
      proxy: blankAccountProxy(),
    }
  }

  let showAdvanced = false

  // the transport is its own field, not read back off the port. deriving it
  // meant STARTTLS only existed on 143 and 587, so a server on any other port
  // could not be reached at all (#237).
  //
  // picking a mode still fills in that mode's usual port, but only when the
  // current one is the other mode's default, so it never overwrites a port the
  // user typed. changing the port never touches the mode.
  const imapPorts: Record<string, number> = { ssl: 993, starttls: 143 }
  const smtpPorts: Record<string, number> = { ssl: 465, starttls: 587 }

  function setTLS(mode: TLSMode): void {
    if (draft.imapPort === imapPorts[draft.imapTls]) {
      draft.imapPort = imapPorts[mode]
    }
    draft.imapTls = mode
  }

  function setSMTPTLS(mode: TLSMode): void {
    if (draft.smtpPort === smtpPorts[draft.smtpTls]) {
      draft.smtpPort = smtpPorts[mode]
    }
    draft.smtpTls = mode
  }

  function selectPreset(p: ProviderPreset): void {
    preset = p
    draft = blankDraft()
    if (p.imapHost) draft.imapHost = p.imapHost
    if (p.imapPort) draft.imapPort = p.imapPort
    if (p.imapTls) draft.imapTls = p.imapTls
    if (p.smtpHost) draft.smtpHost = p.smtpHost
    if (p.smtpPort) draft.smtpPort = p.smtpPort
    if (p.smtpTls) draft.smtpTls = p.smtpTls
    if (p.oauthProvider) draft.provider = p.oauthProvider
    testOk = null
    error = ''
    showAdvanced = false
    discoveredOAuth = ''
    untrusted = []
    caSubjects = []
    step = p.kind === 'oauth' ? 'oauth' : 'config'
  }

  // applyDiscovered fills the servers autodiscovery found. autoconfig states
  // the security outright; empty means it said nothing usable, so the current
  // choice stands rather than being overwritten.
  function applyDiscovered(d: Discovered): void {
    draft.imapHost = d.imapHost
    draft.imapPort = d.imapPort
    draft.smtpHost = d.smtpHost
    draft.smtpPort = d.smtpPort
    if (d.imapTls) draft.imapTls = d.imapTls as TLSMode
    if (d.smtpTls) draft.smtpTls = d.smtpTls as TLSMode
  }

  // switchToGoogle moves a Google-hosted address from the custom form to the
  // Google sign-in form, keeping what the user already typed about themselves.
  function switchToGoogle(): void {
    const google = presetById('gmail')
    if (!google) {
      return
    }
    const { email, displayName, localLabel, useLocalLabel } = draft
    selectPreset(google)
    draft = { ...draft, email, displayName, localLabel, useLocalLabel }
    step = 'oauth'
  }

  if (initialProviderId) {
    const p = presetById(initialProviderId)
    if (p) {
      selectPreset(p)
    }
  }

  // --- step one: the address -------------------------------------------------

  // what the address says about its provider: a preset, and for an address no
  // preset hosts, what autodiscovery found (null when it found nothing).
  interface Detection {
    email: string
    preset: ProviderPreset
    discovered: Discovered | null
    workspace: boolean
  }

  let address = ''
  // whether the address came from the first step, so later steps show it
  // rather than asking for it again.
  let addressGiven = false
  let detection: Detection | null = null
  let detecting = false
  let manual = false
  let detectTimer: ReturnType<typeof setTimeout> | undefined
  // the lookup in flight and the address it is for.
  let pending: { email: string; done: Promise<void> } | null = null

  $: scheduleDetect(address)
  onDestroy(() => clearTimeout(detectTimer))

  // a preset's own domain is known at once. anything else asks autodiscovery,
  // once typing has paused, so a half-typed domain is not looked up.
  function scheduleDetect(value: string): void {
    clearTimeout(detectTimer)
    const email = value.trim()
    if (!isEmailAddress(email)) {
      detection = null
      detecting = false
      return
    }
    if (detection?.email === email) {
      return
    }
    const known = presetForEmail(email)
    if (known) {
      detection = { email, preset: known, discovered: null, workspace: false }
      detecting = false
      return
    }
    detection = null
    detecting = true
    detectTimer = setTimeout(() => void lookUp(email), 600)
  }

  function lookUp(email: string): Promise<void> {
    if (pending?.email !== email) {
      pending = { email, done: discover(email) }
    }
    return pending.done
  }

  async function discover(email: string): Promise<void> {
    let found: Detection
    try {
      const d = await discoverConfig(email)
      const google = d.oauthProvider === 'google' ? presetById('gmail') : null
      found = google
        ? { email, preset: google, discovered: d, workspace: true }
        : { email, preset: presetById('custom') as ProviderPreset, discovered: d.imapHost ? d : null, workspace: false }
    } catch {
      found = { email, preset: presetById('custom') as ProviderPreset, discovered: null, workspace: false }
    }
    if (email === address.trim()) {
      detection = found
      detecting = false
    }
  }

  $: canContinue = isEmailAddress(address)

  async function continueFromAddress(): Promise<void> {
    if (!canContinue) {
      return
    }
    const email = address.trim()
    if (detection?.email !== email && !presetForEmail(email)) {
      clearTimeout(detectTimer)
      detecting = true
      await lookUp(email)
    }
    scheduleDetect(email)
    const found = detection
    if (!found || found.email !== email) {
      return
    }
    usePreset(found.preset, email)
    if (found.discovered && found.preset.custom) {
      applyDiscovered(found.discovered)
    }
    // with nothing found, the servers have to be typed, so they are shown.
    if (found.preset.custom && !found.discovered) {
      showAdvanced = true
    }
  }

  function usePreset(p: ProviderPreset, email: string): void {
    selectPreset(p)
    draft.email = email
    addressGiven = true
  }

  // a tile from the grid. with an address already typed it carries over, and an
  // address under Other is looked up as before.
  async function pick(event: CustomEvent<ProviderPreset>): Promise<void> {
    const email = address.trim()
    const p = event.detail
    if (!isEmailAddress(email)) {
      selectPreset(p)
      addressGiven = false
      return
    }
    usePreset(p, email)
    if (p.custom) {
      await maybeDiscover()
      if (!draft.imapHost) {
        showAdvanced = true
      }
    }
  }

  function changeAddress(): void {
    address = draft.email || address
    error = ''
    step = 'email'
  }

  // for custom providers typed into the form itself, try autodiscovery once a
  // full address is present.
  async function maybeDiscover(): Promise<void> {
    if (!preset?.custom || !draft.email.includes('@')) {
      return
    }
    discoveredOAuth = ''
    try {
      const d = await discoverConfig(draft.email)
      discoveredOAuth = d.oauthProvider
      applyDiscovered(d)
    } catch {
      // leave fields for manual entry; discovery is best effort.
    }
  }

  // --- step two and three: credentials and the connection test --------------

  function startTest(): void {
    formStep = 'config'
    step = 'verify'
    void test()
  }

  async function test(): Promise<void> {
    testing = true
    testOk = null
    untrusted = []
    error = ''
    try {
      const result = await testConnection({
        email: draft.email,
        username: draft.username,
        imapHost: draft.imapHost,
        imapPort: draft.imapPort,
        imapTls: draft.imapTls,
        password: draft.password,
        smtpHost: draft.smtpHost,
        smtpPort: draft.smtpPort,
        smtpTls: draft.smtpTls,
        trustedCerts: draft.trustedCerts,
        caPem: draft.caPem,
        proxy: draft.proxy,
      })
      untrusted = result.untrusted ?? []
      testOk = untrusted.length === 0 ? true : null
    } catch (err) {
      testOk = false
      error = errorMessage(err)
      problem = classifyConnectionError(error)
    } finally {
      testing = false
    }
    if (testOk === true && step === 'verify') {
      step = 'details'
    }
  }

  // trustAndRetest accepts the reviewed certificates for the new mailbox and
  // runs the test again, which now gets past them and logs in.
  async function trustAndRetest(event: CustomEvent<UntrustedCert[]>): Promise<void> {
    draft.trustedCerts = [...new Set([...draft.trustedCerts, ...event.detail.map((c) => c.fingerprint)])]
    await test()
  }

  // back from a failed test to the form, with the server settings open when
  // the fix is in them.
  async function editSettings(): Promise<void> {
    showAdvanced = problem !== 'auth'
    step = 'config'
    await tick()
  }

  async function pickCA(): Promise<void> {
    try {
      const ca = await chooseCAFile()
      if (ca.pem !== '') {
        draft.caPem = ca.pem
        caSubjects = ca.subjects
      }
    } catch (err) {
      error = errorMessage(err)
    }
  }

  function removeCA(): void {
    draft.caPem = ''
    caSubjects = []
  }

  async function addPassword(): Promise<void> {
    formStep = 'config'
    step = 'working'
    workingMessage = get(t)('wizard.working.connecting')
    try {
      const account = await addPasswordAccount(draft)
      await finish(account)
    } catch (err) {
      fail(err)
    }
  }

  async function signIn(): Promise<void> {
    formStep = 'oauth'
    step = 'working'
    workingMessage = get(t)('wizard.working.signIn')
    try {
      const account = await addOAuthAccount(draft)
      await finish(account)
    } catch (err) {
      fail(err)
    }
  }

  // --- after the account exists: folders and the first sync -----------------

  // the folder picker (#173). The account exists and its folders are discovered
  // by now, but nothing has synced yet: AddAccount does not start, so a
  // 30k-message archive can be unchecked before it is ever fetched.
  let addedAccount: Account | null = null
  let folders: Folder[] = []
  // ids the user unchecked. Everything starts checked.
  let unchecked = new Set<number>()
  let applying = false

  async function finish(account: Account): Promise<void> {
    addedAccount = account
    if (color) {
      try {
        await setAccountColor(account.id, color)
        account = { ...account, color }
      } catch (err) {
        toastError(errorMessage(err))
      }
    }
    dispatch('added', account)
    try {
      folders = await listFolders(account.id)
    } catch {
      // discovery failed or the server has no folders worth choosing from.
      // Never block the wizard on it: sync and let the next one find them.
      folders = []
    }
    if (folders.length < 2) {
      // one folder, or none, is not a choice worth a screen.
      await beginSync()
      return
    }
    unchecked = new Set()
    step = 'folders'
  }

  function toggleFolder(id: number): void {
    const next = new Set(unchecked)
    if (next.has(id)) {
      next.delete(id)
    } else {
      next.add(id)
    }
    unchecked = next
  }

  // beginSync applies the folder choice and starts the first sync. It runs on
  // every route out of the folder step, including skipping it, because the
  // account is already saved and would otherwise sit there never syncing.
  async function beginSync(): Promise<void> {
    const account = addedAccount
    if (!account) {
      step = 'done'
      return
    }
    applying = true
    try {
      for (const id of unchecked) {
        await setFolderSyncExcluded(id, true)
      }
      await startAccountSync(account.id)
    } catch (err) {
      // the account exists either way, so a failure here is not worth throwing
      // the user back to the form. It syncs everything, and the folders stay
      // switchable in settings.
      toastError(errorMessage(err))
    } finally {
      applying = false
      step = 'done'
      if (embedded) {
        dispatch('done', account)
      }
    }
  }

  function fail(err: unknown): void {
    error = errorMessage(err)
    problem = classifyConnectionError(error)
    step = 'error'
  }

  // the header's back: one step towards the address.
  function back(): void {
    error = ''
    if (step === 'verify' || step === 'details') {
      step = 'config'
    } else if (step === 'error') {
      step = formStep
    } else {
      changeAddress()
    }
  }

  $: canBack = step === 'config' || step === 'oauth' || step === 'verify' || step === 'details' || step === 'error'
  $: stepNumber = step === 'email' ? 1 : step === 'config' || step === 'oauth' || step === 'verify' || step === 'error' ? 2 : 3

  $: canSubmitPassword = draft.email.includes('@') && draft.password !== '' && draft.imapHost !== ''
  $: canSignIn = draft.email.includes('@') && draft.clientId !== '' && (!preset?.requireClientSecret || draft.clientSecret !== '')

  // the Google Cloud Console pages the client setup steps link to, in order.
  const googleSetup: { text: string; link: string; url: string }[] = [
    { text: 'wizard.google.setup.project', link: 'wizard.google.setup.projectLink', url: 'https://console.cloud.google.com/projectcreate' },
    { text: 'wizard.google.setup.api', link: 'wizard.google.setup.apiLink', url: 'https://console.cloud.google.com/apis/library/gmail.googleapis.com' },
    { text: 'wizard.google.setup.consent', link: 'wizard.google.setup.consentLink', url: 'https://console.cloud.google.com/auth/overview' },
    { text: 'wizard.google.setup.client', link: 'wizard.google.setup.clientLink', url: 'https://console.cloud.google.com/auth/clients/create' },
  ]

  // a passing connection test is required before the account can be added:
  // nothing else checks that imapHost is a real, reachable server, and the
  // account is kept even if the later folder discovery fails. any edit to a
  // tested field invalidates the prior result so a stale "ok" cannot wave
  // through a since-changed value. the fields are listed by value so editing
  // the name or label after the test leaves it standing.
  $: tested = JSON.stringify([
    draft.email,
    draft.username,
    draft.imapHost,
    draft.imapPort,
    draft.imapTls,
    draft.password,
    draft.smtpHost,
    draft.smtpPort,
    draft.smtpTls,
    draft.caPem,
    draft.proxy,
  ])
  let lastTested = ''
  $: if (tested !== lastTested) {
    lastTested = tested
    testOk = null
    untrusted = []
  }
  $: canAddAccount = canSubmitPassword && testOk === true

  $: providerName = preset ? (preset.labelKey ? $t(preset.labelKey) : preset.label) : ''
  $: tryingLine = $t('wizard.verify.trying')
    .replace('{host}', `${draft.imapHost}:${draft.imapPort}`)
    .replace('{user}', draft.username || draft.email)

  function focusOnMount(node: HTMLElement): void {
    if (!embedded) {
      node.focus()
    }
  }
</script>

<div class="wizard" class:embedded role={embedded ? 'region' : 'dialog'} aria-modal={embedded ? undefined : true} aria-label={$t('addMailbox.cta')}>
  {#if !embedded}
    <header class="head">
      {#if canBack}
        <button type="button" class="icon" aria-label={$t('wizard.back')} on:click={back}>
          <Ph svg={backIcon} size={16} />
        </button>
      {:else}
        <span class="icon-spacer"></span>
      {/if}
      <span class="title">{$t('addMailbox.cta')}</span>
      <button type="button" class="icon" aria-label={$t('wizard.close')} on:click={() => dispatch('close')}>
        <Ph svg={closeIcon} size={16} />
      </button>
    </header>
  {/if}

  <div class="body">
    <div class="content">
      {#if embedded}
        {#if canBack}
          <div class="meta">
            <button type="button" class="link" on:click={back}>{$t('wizard.back')}</button>
          </div>
        {/if}
      {:else if step !== 'done'}
        <div class="meta">
          <span class="counter">{$t('common.stepCounter').replace('{n}', String(stepNumber)).replace('{total}', '3')}</span>
        </div>
      {/if}

      {#key step}
        <div class="pane">
          {#if step === 'email'}
            {#if !embedded}
              <h3>{$t('wizard.step.provider.title')}</h3>
              <p class="note">{$t('wizard.email.lead')}</p>
            {/if}
            <form class="address" on:submit|preventDefault={continueFromAddress}>
              <label class="field">
                <span>{$t('wizard.field.email')}</span>
                <input
                  type="email"
                  bind:value={address}
                  placeholder={$t('wizard.field.emailPlaceholder')}
                  autocomplete="email"
                  spellcheck="false"
                  use:focusOnMount
                />
              </label>
              <div class="detected" aria-live="polite">
                {#if detecting}
                  <PieProgress size={14} label={$t('wizard.email.detecting')} />
                  <span class="muted">{$t('wizard.email.detecting')}</span>
                {:else if detection}
                  <span class="badge"><Ph svg={providerIcon(detection.preset.id)} size={16} /></span>
                  <span class="det-text">
                    <span class="det-name">
                      {detection.preset.custom ? detection.email.split('@')[1] : detection.preset.label}
                    </span>
                    <span class="muted">
                      {#if detection.preset.custom && detection.discovered?.source === 'guess'}
                        <span class="unsure">{$t('wizard.email.guessed').replace('{host}', detection.discovered.imapHost)}</span>
                      {:else if detection.preset.custom && detection.discovered}
                        {detection.discovered.imapHost}
                      {:else if detection.preset.custom}
                        {$t('wizard.email.notFound')}
                      {:else}
                        {$t('wizard.email.detected')}
                      {/if}
                    </span>
                  </span>
                {/if}
              </div>
              <div class="row-actions">
                <button type="button" class="link" aria-expanded={manual} on:click={() => (manual = !manual)}>
                  {$t('wizard.email.chooseManually')}
                </button>
                <FlowButton variant="primary" type="submit" disabled={!canContinue}>{$t('onboarding.continue')}</FlowButton>
              </div>
            </form>
            {#if manual}
              <div class="manual">
                <WizardProviders selected={detection?.preset.id ?? null} on:pick={pick} />
              </div>
            {/if}
            {#if offerImport}
              <button type="button" class="import" on:click={() => (showImport = true)}>
                <Ph svg={importIcon} size={18} />
                <span class="import-text">
                  <span class="import-title">{$t('wizard.start.importTitle')}</span>
                  <span class="muted">{$t('import.entry.sub')}</span>
                </span>
              </button>
            {/if}
          {:else if step === 'config'}
            <h3>{providerName}</h3>

            {#if addressGiven}
              <div class="identity">
                <Ph svg={providerIcon(preset?.id ?? '')} size={16} />
                <span class="id-email">{draft.email}</span>
                <button type="button" class="link" on:click={changeAddress}>{$t('wizard.email.change')}</button>
              </div>
            {:else}
              {#if preset?.noteKey && !preset?.appPasswordUrl}<p class="note">{$t(preset.noteKey)}</p>{/if}
              <label class="field">
                <span>{$t('wizard.field.email')}</span>
                <input type="email" bind:value={draft.email} on:blur={maybeDiscover} placeholder={$t('wizard.field.emailPlaceholder')} />
              </label>
            {/if}
            {#if discoveredOAuth === 'google'}
              <div class="hint-box">
                <span>{$t('wizard.workspace.detected')}</span>
                <button type="button" class="link" on:click={switchToGoogle}>
                  {$t('wizard.workspace.useGoogle')}
                </button>
              </div>
            {/if}

            <label class="field">
              <span>{$t('wizard.field.password')}</span>
              <input
                type="password"
                bind:value={draft.password}
                placeholder={preset?.appPasswordUrl ? $t('wizard.field.appPasswordPlaceholder') : $t('wizard.field.passwordPlaceholder')}
              />
            </label>
            {#if preset?.appPasswordUrl}
              <div class="hint-box warn">
                <span>{preset?.id === 'gmail' ? $t('wizard.gmail.appPasswordWarning') : $t('wizard.appPassword.warning')}</span>
                <button type="button" class="link" on:click={() => openURL(preset?.appPasswordUrl ?? '')}>
                  {$t('wizard.appPassword.link')}
                  <Ph svg={linkIcon} size={12} />
                </button>
              </div>
            {:else if preset?.noteKey && addressGiven && !preset.custom}
              <p class="note">{$t(preset.noteKey)}</p>
            {/if}

            <button type="button" class="disclosure" aria-expanded={showAdvanced} on:click={() => (showAdvanced = !showAdvanced)}>
              <span class="caret" class:open={showAdvanced} aria-hidden="true"></span>
              {showAdvanced ? $t('wizard.advanced.hide') : $t('wizard.advanced.show')} {$t('wizard.advanced.connectionSettings')}
            </button>
            {#if showAdvanced}
              <div class="advanced">
                <div class="servers">
                  <label class="field"><span>{$t('wizard.field.imapHost')}</span><input type="text" bind:value={draft.imapHost} /></label>
                  <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.imapPort} /></label>
                </div>
                <div class="servers">
                  <label class="field"><span>{$t('wizard.field.smtpHost')}</span><input type="text" bind:value={draft.smtpHost} /></label>
                  <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.smtpPort} /></label>
                </div>

                <label class="field">
                  <span>{$t('wizard.field.username')}</span>
                  <input type="text" bind:value={draft.username} placeholder={draft.email || $t('wizard.field.usernamePlaceholder')} />
                </label>
                <p class="adv-hint">{$t('wizard.advanced.usernameHint')}</p>

                <span class="adv-label">{$t('wizard.advanced.imapSecurity')}</span>
                <div class="seg" role="radiogroup" aria-label={$t('wizard.advanced.imapSecurity')}>
                  <button type="button" class:on={draft.imapTls === 'ssl'} on:click={() => setTLS('ssl')}>SSL / TLS</button>
                  <button type="button" class:on={draft.imapTls === 'starttls'} on:click={() => setTLS('starttls')}>STARTTLS</button>
                </div>

                <span class="adv-label">{$t('wizard.advanced.smtpSecurity')}</span>
                <div class="seg" role="radiogroup" aria-label={$t('wizard.advanced.smtpSecurity')}>
                  <button type="button" class:on={draft.smtpTls === 'ssl'} on:click={() => setSMTPTLS('ssl')}>SSL / TLS</button>
                  <button type="button" class:on={draft.smtpTls === 'starttls'} on:click={() => setSMTPTLS('starttls')}>STARTTLS</button>
                </div>
                <p class="adv-hint">{$t('wizard.advanced.tlsHint')}</p>

                <span class="adv-label">{$t('certs.ca.label')}</span>
                {#if draft.caPem}
                  <p class="adv-hint">{caSubjects.join(', ')}</p>
                  <FlowButton on:click={removeCA}>{$t('certs.ca.remove')}</FlowButton>
                {:else}
                  <FlowButton on:click={pickCA}>{$t('certs.ca.choose')}</FlowButton>
                {/if}
                <p class="adv-hint">{$t('certs.ca.hint')}</p>

                <AccountRouteFields bind:route={draft.proxy} />
              </div>
            {/if}

            {#if error}<p class="err">{error}</p>{/if}

            <div class="actions">
              {#if preset?.oauthOptional}
                <button type="button" class="link" on:click={() => { error = ''; step = 'oauth' }}>
                  {$t('wizard.gmail.useOauthInstead')}
                </button>
              {/if}
              <FlowButton variant="primary" on:click={startTest} disabled={testing || !canSubmitPassword}>
                {$t('wizard.testConnection')}
              </FlowButton>
            </div>
          {:else if step === 'verify'}
            <h3>{$t('wizard.verify.title')}</h3>
            {#if testing}
              <div class="trying" role="status">
                <span class="pie"><PieProgress size={22} label={$t('wizard.testing')} /></span>
                <span class="trying-text">{tryingLine}</span>
              </div>
            {/if}
            {#if untrusted.length > 0}
              <CertificateReview certs={untrusted} busy={testing} on:trust={trustAndRetest} />
            {/if}
            {#if testOk === false}
              <div class="problem" role="alert">
                <span class="problem-icon"><Ph svg={warnIcon} size={18} /></span>
                <span class="problem-text">
                  <span class="problem-title">{$t(`wizard.problem.${problem}`)}</span>
                  <span>{$t(`wizard.problem.${problem}.fix`)}</span>
                  {#if problem === 'auth' && preset?.appPasswordUrl}
                    <button type="button" class="link" on:click={() => openURL(preset?.appPasswordUrl ?? '')}>
                      {$t('wizard.appPassword.link')}
                      <Ph svg={linkIcon} size={12} />
                    </button>
                  {/if}
                  <code class="raw">{error}</code>
                </span>
              </div>
              <div class="actions">
                <FlowButton on:click={editSettings}>{$t('wizard.verify.edit')}</FlowButton>
                <FlowButton variant="primary" on:click={() => void test()}>{$t('wizard.verify.retry')}</FlowButton>
              </div>
            {/if}
          {:else if step === 'details'}
            <p class="ok"><Ph svg={checkIcon} size={16} /> {$t('wizard.connectionWorks')}</p>
            <h3>{$t('wizard.details.title')}</h3>
            <p class="note">{$t('wizard.details.lead')}</p>
            <label class="field">
              <span>
                {$t('wizard.field.fromName')}
                <InfoTip text={$t('mailboxes.fromNameHint')} />
              </span>
              <input type="text" bind:value={draft.displayName} placeholder={$t('wizard.field.displayNamePlaceholder')} />
            </label>
            <div class="toggle">
              <span>{$t('mailboxes.localLabel.toggle')}</span>
              <ToggleSwitch
                checked={draft.useLocalLabel}
                label={$t('mailboxes.localLabel.toggle')}
                on:change={(e) => (draft.useLocalLabel = e.detail)}
              />
            </div>
            {#if draft.useLocalLabel}
              <label class="field">
                <span>
                  {$t('wizard.field.localLabel')}
                  <InfoTip text={$t('mailboxes.localLabelHint')} />
                </span>
                <input type="text" bind:value={draft.localLabel} placeholder={draft.displayName || draft.email} />
              </label>
            {/if}
            <div class="field">
              <span>{$t('views.colorLabel')}</span>
              <AccountColorPick bind:value={color} />
              <p class="adv-hint">{$t('mailboxes.colorHint')}</p>
            </div>
            <div class="actions">
              <FlowButton variant="primary" on:click={addPassword} disabled={!canAddAccount}>
                {$t('addMailbox.cta')}
              </FlowButton>
            </div>
          {:else if step === 'oauth'}
            <h3>{$t('wizard.step.oauth.title')} {preset?.label}</h3>
            <p class="note">{$t('wizard.step.oauth.note')}</p>
            {#if preset?.oauthProvider === 'google'}
              <div class="hint-box">
                <span class="setup-title">{$t('wizard.google.setup.title')}</span>
                <ol class="setup-steps">
                  {#each googleSetup as s}
                    <li>
                      <span>{$t(s.text)}</span>
                      <button type="button" class="link" on:click={() => openURL(s.url)}>
                        {$t(s.link)}
                      </button>
                    </li>
                  {/each}
                </ol>
                <span>{$t('wizard.google.setup.admin')}</span>
              </div>
            {/if}

            {#if addressGiven}
              <div class="identity">
                <Ph svg={providerIcon(preset?.id ?? '')} size={16} />
                <span class="id-email">{draft.email}</span>
                <button type="button" class="link" on:click={changeAddress}>{$t('wizard.email.change')}</button>
              </div>
            {:else}
              <label class="field">
                <span>{$t('wizard.field.email')}</span>
                <input type="email" bind:value={draft.email} placeholder={$t('wizard.field.emailPlaceholder')} />
              </label>
            {/if}
            <label class="field">
              <span>
                {$t('wizard.field.fromName')}
                <InfoTip text={$t('mailboxes.fromNameHint')} />
              </span>
              <input type="text" bind:value={draft.displayName} placeholder={$t('wizard.field.displayNamePlaceholder')} />
            </label>
            <div class="toggle">
              <span>{$t('mailboxes.localLabel.toggle')}</span>
              <ToggleSwitch
                checked={draft.useLocalLabel}
                label={$t('mailboxes.localLabel.toggle')}
                on:change={(e) => (draft.useLocalLabel = e.detail)}
              />
            </div>
            {#if draft.useLocalLabel}
              <label class="field">
                <span>
                  {$t('wizard.field.localLabel')}
                  <InfoTip text={$t('mailboxes.localLabelHint')} />
                </span>
                <input type="text" bind:value={draft.localLabel} placeholder={draft.displayName || draft.email} />
              </label>
            {/if}
            <label class="field">
              <span>{$t('wizard.field.oauthClientId')}</span>
              <input type="text" bind:value={draft.clientId} placeholder="xxxxx.apps.googleusercontent.com" />
            </label>

            {#if preset?.requireClientSecret}
              <label class="field">
                <span>{$t('wizard.field.oauthClientSecretRequired')}</span>
                <input type="password" bind:value={draft.clientSecret} />
              </label>
            {/if}
            <div class="field">
              <span>{$t('views.colorLabel')}</span>
              <AccountColorPick bind:value={color} />
            </div>
            <!-- the route is here too: a mailbox only reachable through its own
                 proxy needs it for the sign-in and for the first connection. -->
            <button type="button" class="disclosure" aria-expanded={showAdvanced} on:click={() => (showAdvanced = !showAdvanced)}>
              <span class="caret" class:open={showAdvanced} aria-hidden="true"></span>
              {showAdvanced ? $t('wizard.advanced.hide') : $t('wizard.advanced.show')}
            </button>
            {#if showAdvanced}
              <div class="advanced">
                {#if preset?.allowClientSecret && !preset?.requireClientSecret}
                  <label class="field">
                    <span>{$t('wizard.field.oauthClientSecret')}</span>
                    <input type="password" bind:value={draft.clientSecret} placeholder={$t('wizard.field.oauthClientSecretPlaceholder')} />
                  </label>
                  <p class="adv-hint">{$t('wizard.advanced.clientSecretHint')}</p>
                {/if}
                <AccountRouteFields bind:route={draft.proxy} oauth />
              </div>
            {/if}

            {#if error}<p class="err">{error}</p>{/if}

            <div class="actions">
              {#if preset?.oauthOptional}
                <button type="button" class="link" on:click={() => { error = ''; step = 'config' }}>
                  {$t('wizard.gmail.useAppPasswordInstead')}
                </button>
              {/if}
              <FlowButton variant="primary" on:click={signIn} disabled={!canSignIn}>
                {$t('wizard.signInWith')} {preset?.label}
              </FlowButton>
            </div>
          {:else if step === 'working'}
            <div class="trying" role="status">
              <span class="pie"><PieProgress size={22} label={workingMessage} /></span>
              <span class="trying-text">{workingMessage}</span>
            </div>
          {:else if step === 'folders'}
            <h3>{$t('wizard.folders.title')}</h3>
            <p class="note">{$t('wizard.folders.body')}</p>
            <ul class="folder-list">
              {#each folders as folder (folder.id)}
                <li>
                  <label>
                    <input type="checkbox" checked={!unchecked.has(folder.id)} on:change={() => toggleFolder(folder.id)} />
                    <span class="folder-name">{folder.name}</span>
                  </label>
                </li>
              {/each}
            </ul>
            <div class="actions">
              <FlowButton disabled={applying} on:click={() => { unchecked = new Set(); void beginSync() }}>
                {$t('wizard.folders.skip')}
              </FlowButton>
              <FlowButton variant="primary" disabled={applying} on:click={() => void beginSync()}>
                {applying ? $t('wizard.folders.applying') : $t('wizard.folders.confirm')}
              </FlowButton>
            </div>
          {:else if step === 'done'}
            <div class="result">
              <span class="result-icon"><Ph svg={checkIcon} size={28} /></span>
              <h3>{$t('wizard.done.title')}</h3>
              <p class="note">{draft.email} {$t('wizard.done.syncing')}</p>
              {#if !embedded}
                <FlowButton variant="primary" on:click={() => dispatch('close')}>{$t('wizard.done.button')}</FlowButton>
              {/if}
            </div>
          {:else if step === 'error'}
            <h3>{$t('wizard.error.title')}</h3>
            <div class="problem" role="alert">
              <span class="problem-icon"><Ph svg={warnIcon} size={18} /></span>
              <span class="problem-text">
                {#if problem !== 'unknown'}
                  <span class="problem-title">{$t(`wizard.problem.${problem}`)}</span>
                  <span>{$t(`wizard.problem.${problem}.fix`)}</span>
                {/if}
                <code class="raw">{error}</code>
              </span>
            </div>
            <div class="actions">
              <FlowButton on:click={() => (step = formStep)}>{$t('wizard.back')}</FlowButton>
            </div>
          {/if}
        </div>
      {/key}
    </div>
  </div>
</div>

{#if showImport}
  {#await import('../import/ImportDialog.svelte') then m}
    <svelte:component this={m.default} on:close={() => (showImport = false)} />
  {/await}
{/if}

<style>
  .wizard {
    position: fixed;
    inset: 0;
    /* above the onboarding overlay (120). */
    z-index: 150;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    /* covers the whole window, so it keeps the macOS traffic lights clear
       itself; zero on every other platform. */
    padding-top: var(--titlebar-lights);
  }

  .wizard.embedded {
    position: static;
    z-index: auto;
    padding-top: 0;
    background: transparent;
  }

  .head {
    display: grid;
    grid-template-columns: var(--control-height) 1fr var(--control-height);
    align-items: center;
    padding: var(--space-3) var(--space-5);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    text-align: center;
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    padding: 0;
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-spacer {
    width: var(--control-height);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .embedded .body {
    overflow: visible;
  }

  .content {
    max-width: 30rem;
    margin: 0 auto;
    padding: var(--space-6);
  }

  .embedded .content {
    max-width: none;
    padding: 0;
  }

  .meta {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-3);
  }

  .counter {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .pane {
    display: flex;
    flex-direction: column;
    animation: pane-in var(--motion-base) var(--ease-out);
  }

  @keyframes pane-in {
    from {
      opacity: 0;
      transform: translateY(var(--space-2));
    }
  }

  h3 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.01em;
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .note {
    margin: 0 0 var(--space-5);
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-secondary);
  }

  .muted {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    overflow-wrap: anywhere;
  }

  .address {
    display: flex;
    flex-direction: column;
  }

  .detected {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) + var(--space-3));
    margin-bottom: var(--space-3);
    color: var(--text-secondary);
  }

  .badge {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: var(--control-height);
    height: var(--control-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--accent);
    animation: pane-in var(--motion-base) var(--ease-out);
  }

  .det-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .unsure {
    color: var(--warning);
  }

  .det-name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .row-actions,
  .actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .actions {
    justify-content: flex-end;
    margin-top: var(--space-5);
  }

  .actions .link {
    margin-inline-end: auto;
  }

  .manual {
    margin-top: var(--space-5);
    animation: pane-in var(--motion-base) var(--ease-out);
  }

  .import {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    margin-top: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: transparent;
    color: var(--text-secondary);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .import:hover {
    border-color: var(--border-strong);
  }

  .import:active {
    transform: scale(0.99);
  }

  .import-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .import-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .identity {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
  }

  .id-email {
    flex: 1;
    min-width: 0;
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .hint-box {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    margin: 0 0 var(--space-4);
    padding: var(--space-3) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-primary);
  }

  .hint-box.warn {
    border-color: transparent;
    border-inline-start: 2px solid var(--warning);
    border-radius: var(--radius-none);
    background: var(--warning-bg);
  }

  .setup-title {
    font-weight: var(--fw-medium);
  }

  .setup-steps {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding-inline-start: var(--space-5);
  }

  .setup-steps li {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
  }

  .link {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-4);
  }

  .field > span {
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .field input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-list);
    outline: none;
    transition:
      border-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out);
  }

  .field input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 var(--hairline) var(--accent);
  }

  .servers {
    display: flex;
    gap: var(--space-3);
  }

  .servers .field {
    flex: 1;
    min-width: 0;
  }

  .servers .field.narrow {
    flex: 0 0 6em;
  }

  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .disclosure {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    align-self: flex-start;
    padding: var(--space-1) 0;
    margin-bottom: var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .disclosure:hover {
    color: var(--text-primary);
  }

  .caret {
    width: 0;
    height: 0;
    border-block: var(--space-1) solid transparent;
    border-inline-start: calc(var(--space-1) * 1.5) solid currentColor;
    transition: transform var(--motion-fast) var(--ease-out);
  }

  .caret.open {
    transform: rotate(90deg);
  }

  .advanced {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    margin-bottom: var(--space-3);
    padding-inline-start: var(--space-4);
    border-inline-start: var(--hairline) solid var(--border-default);
    animation: pane-in var(--motion-base) var(--ease-out);
  }

  .advanced > .field,
  .advanced > .servers {
    align-self: stretch;
  }

  .adv-label {
    display: block;
    margin: var(--space-3) 0 var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .adv-hint {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-meta);
    line-height: 1.5;
    color: var(--text-tertiary);
  }

  .seg {
    display: inline-flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .seg button {
    padding: var(--space-2) var(--space-4);
    border: none;
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .seg button + button {
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .seg button.on {
    background: var(--accent);
    color: var(--accent-fg);
  }

  .trying {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    margin: var(--space-4) 0;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .pie {
    display: inline-flex;
    color: var(--accent);
  }

  .trying-text {
    min-width: 0;
    font-size: var(--fz-list);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .problem {
    display: flex;
    gap: var(--space-3);
    margin-top: var(--space-4);
    padding: var(--space-4);
    border-inline-start: 2px solid var(--danger);
    background: var(--danger-bg);
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-primary);
  }

  .problem-icon {
    display: inline-flex;
    color: var(--danger);
  }

  .problem-text {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    min-width: 0;
  }

  .problem-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
  }

  .raw {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .ok {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--success);
  }

  .err {
    font-size: var(--fz-label);
    color: var(--danger);
    overflow-wrap: anywhere;
  }

  .folder-list {
    max-height: 16rem;
    margin: 0;
    padding: var(--space-2);
    overflow-y: auto;
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
  }

  .folder-list label {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-1) var(--space-2);
    font-size: var(--fz-list);
    cursor: var(--cursor-action);
  }

  .folder-list input {
    accent-color: var(--accent);
  }

  .folder-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .result {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-4) 0;
  }

  .result-icon {
    display: inline-flex;
    color: var(--success);
  }
</style>
