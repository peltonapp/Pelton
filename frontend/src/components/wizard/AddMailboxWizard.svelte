<script lang="ts">
  // the fullscreen add-mailbox wizard. it walks: provider -> (oauth client id |
  // password + servers) -> test/sign-in -> done. autodiscovery prefills servers
  // for custom providers; oauth uses the per-user PKCE flow (the user supplies
  // their own client id). this component is code-split and only loaded when the
  // user opens it, so its cost is not paid at startup.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import InfoTip from '../common/InfoTip.svelte'
  import { IconX, IconArrowLeft, IconCheck, IconArrowRight, IconMailbox, IconPlus } from '@tabler/icons-svelte'
  import WizardProviders from './WizardProviders.svelte'
  import Spinner from '../common/Spinner.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
  import CertificateReview from '../common/CertificateReview.svelte'
  import AccountRouteFields from '../common/AccountRouteFields.svelte'
  import { blankAccountProxy } from '../../lib/proxyroute'
  import { discoverConfig, testConnection, addPasswordAccount, addOAuthAccount, listFolders, setFolderSyncExcluded, startAccountSync, chooseCAFile } from '../../lib/api'
  import { errorMessage, toastError } from '../../stores/toast'
  import { providerPresets, type ProviderPreset } from '../../lib/providers'
  import type { AddAccountRequest, Account, Folder, TLSMode, UntrustedCert } from '../../lib/types'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ close: void; added: Account }>()

  // when set, the wizard skips the provider grid and opens straight into that
  // provider's setup. used by the onboarding provider cards.
  export let initialProviderId: string | null = null

  // whether to open on the start step, which asks whether the user is setting up
  // a mailbox or coming from another client. Onboarding turns it off: it offers
  // importing as a step of its own, so asking again here would be a dead end
  // the user has already been past.
  export let offerImport = true

  // when set, the wizard syncs everything as soon as the account is added and
  // never shows the folder picker. Onboarding needs this: it closes the wizard
  // the moment 'added' fires, so the picker could not be answered there.
  export let skipFolderPicker = false

  type Step = 'start' | 'provider' | 'config' | 'oauth' | 'working' | 'folders' | 'done' | 'error'
  let step: Step = offerImport ? 'start' : 'provider'

  // the import modal, opened over the wizard from the start step.
  let showImport = false
  // the form the last submit came from, so the error screen returns there
  // (gmail can submit from either form since oauth is optional for it).
  let formStep: 'config' | 'oauth' = 'config'
  let preset: ProviderPreset | null = null
  let error = ''
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

  // the account draft being assembled across steps.
  let draft: AddAccountRequest = blankDraft()

  // server fields the user edited by hand. Autodiscovery never fills these, even
  // when it lands later or runs again, so it cannot undo a tested setup. Reset
  // only when the provider changes.
  type ServerField = 'imapHost' | 'imapPort' | 'imapTls' | 'smtpHost' | 'smtpPort' | 'smtpTls'
  let touched = new Set<ServerField>()
  function touch(...fields: ServerField[]): void {
    for (const f of fields) touched.add(f)
  }

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

  // whether the advanced section (tls mode, oauth secret) is expanded.
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
    touch('imapTls', 'imapPort')
    if (draft.imapPort === imapPorts[draft.imapTls]) {
      draft.imapPort = imapPorts[mode]
    }
    draft.imapTls = mode
  }

  function setSMTPTLS(mode: TLSMode): void {
    touch('smtpTls', 'smtpPort')
    if (draft.smtpPort === smtpPorts[draft.smtpTls]) {
      draft.smtpPort = smtpPorts[mode]
    }
    draft.smtpTls = mode
  }


  function selectPreset(p: ProviderPreset): void {
    preset = p
    draft = blankDraft()
    touched = new Set()
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

  // switchToGoogle moves a Google-hosted address from the custom form to the
  // Google sign-in form, keeping what the user already typed about themselves.
  function switchToGoogle(): void {
    const google = providerPresets.find((x) => x.id === 'gmail')
    if (!google) {
      return
    }
    const { email, displayName, localLabel, useLocalLabel } = draft
    selectPreset(google)
    draft = { ...draft, email, displayName, localLabel, useLocalLabel }
    step = 'oauth'
  }

  function pick(event: CustomEvent<ProviderPreset>): void {
    selectPreset(event.detail)
  }

  // jump straight to a provider when the caller requested one.
  onMount(() => {
    if (initialProviderId) {
      const p = providerPresets.find((x) => x.id === initialProviderId)
      if (p) {
        selectPreset(p)
      }
    }
  })

  // the newest discovery request; an older one that resolves later is stale.
  let discoverySeq = 0

  // for custom providers, try autodiscovery once a full address is present.
  // discovery is slow and can rerun on every blur, so the user may type the
  // servers, or even test them, before it lands. Overwriting those would
  // invalidate the passed test, so a result only fills fields never hand-edited.
  async function maybeDiscover(): Promise<void> {
    if (!preset?.custom || !draft.email.includes('@')) {
      return
    }
    const seq = ++discoverySeq
    const email = draft.email
    discoveredOAuth = ''
    try {
      const d = await discoverConfig(email)
      if (seq !== discoverySeq || draft.email !== email) {
        return
      }
      discoveredOAuth = d.oauthProvider
      // assign only real changes: any write to draft re-runs the block below
      // that clears a passed test, even when the value is the same.
      const fill = <K extends ServerField>(key: K, value: AddAccountRequest[K]): void => {
        if (!touched.has(key) && draft[key] !== value) draft[key] = value
      }
      fill('imapHost', d.imapHost)
      fill('imapPort', d.imapPort)
      fill('smtpHost', d.smtpHost)
      fill('smtpPort', d.smtpPort)
      // autoconfig states the security outright; empty means it said nothing
      // usable, so the current choice stands rather than being overwritten.
      if (d.imapTls) fill('imapTls', d.imapTls as TLSMode)
      if (d.smtpTls) fill('smtpTls', d.smtpTls as TLSMode)
    } catch {
      // leave fields for manual entry; discovery is best effort.
    }
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
    } finally {
      testing = false
    }
  }

  // trustAndRetest accepts the reviewed certificates for the new mailbox and
  // runs the test again, which now gets past them and logs in.
  async function trustAndRetest(event: CustomEvent<UntrustedCert[]>): Promise<void> {
    draft.trustedCerts = [...new Set([...draft.trustedCerts, ...event.detail.map((c) => c.fingerprint)])]
    await test()
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

  // the folder picker (#173). The account exists and its folders are discovered
  // by now, but nothing has synced yet: AddAccount deliberately does not start,
  // so a 30k-message archive can be unchecked before it is ever fetched.
  let addedAccount: Account | null = null
  let folders: Folder[] = []
  // ids the user unchecked. Everything starts checked, which is what the issue
  // asked for and keeps the default behaviour unchanged.
  let unchecked = new Set<number>()
  let applying = false
  // beginSync is reachable from several routes (buttons, teardown); the first
  // sync must start exactly once whichever gets there first.
  let syncStarted = false

  // Leaving the folder step by any route the wizard does not control (Escape in
  // the host window, window close) never confirmed a folder choice, so sync
  // everything, the old default, rather than leave the saved account idle.
  onDestroy(() => {
    if (addedAccount && !syncStarted) {
      syncStarted = true
      void startAccountSync(addedAccount.id).catch((err) => toastError(errorMessage(err)))
    }
  })

  async function finish(account: Account): Promise<void> {
    addedAccount = account
    if (skipFolderPicker) {
      // start before announcing: the embedder may tear the wizard down on 'added'.
      await beginSync()
      dispatch('added', account)
      return
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
    if (syncStarted) {
      return
    }
    syncStarted = true
    applying = true
    try {
      for (const id of unchecked) {
        await setFolderSyncExcluded(id, true)
      }
      await startAccountSync(account.id)
    } catch (err) {
      // the account exists either way, so a failure here is not worth throwing
      // the user back to the form. It syncs everything, which is the old
      // behaviour, and the folders stay unswitchable only until settings.
      toastError(errorMessage(err))
    } finally {
      applying = false
      step = 'done'
    }
  }

  function fail(err: unknown): void {
    error = errorMessage(err)
    step = 'error'
  }

  function back(): void {
    error = ''
    step = 'provider'
    preset = null
  }

  // the start step is only ever reachable backwards from the provider grid.
  function backToStart(): void {
    error = ''
    step = 'start'
    preset = null
  }

  $: canSubmitPassword = draft.email.includes('@') && draft.password !== '' && draft.imapHost !== ''
  $: canSignIn = draft.email.includes('@') && draft.clientId !== '' && (!preset?.requireClientSecret || draft.clientSecret !== '')

  // the Google Cloud Console pages the client setup steps link to, in order.
  const googleSetup: { text: string; link: string; url: string }[] = [
    { text: 'wizard.google.setup.project', link: 'wizard.google.setup.projectLink', url: 'https://console.cloud.google.com/projectcreate' },
    { text: 'wizard.google.setup.api', link: 'wizard.google.setup.apiLink', url: 'https://console.cloud.google.com/apis/library/gmail.googleapis.com' },
    { text: 'wizard.google.setup.consent', link: 'wizard.google.setup.consentLink', url: 'https://console.cloud.google.com/auth/overview' },
    { text: 'wizard.google.setup.client', link: 'wizard.google.setup.clientLink', url: 'https://console.cloud.google.com/auth/clients/create' },
  ]

  // a passing "test connection" is required before the account can actually be
  // added: nothing here validates that imapHost is a real, reachable server
  // otherwise (createAccount deliberately keeps the account even if the later
  // folder-discovery connection fails, so garbage like a random url would
  // otherwise sail straight through). any edit to a tested field invalidates
  // the prior result so a stale "ok" can't wave through a since-changed value.
  $: {
    draft.email
    draft.username
    draft.imapHost
    draft.imapPort
    draft.imapTls
    draft.password
    draft.smtpHost
    draft.smtpPort
    draft.smtpTls
    draft.caPem
    draft.proxy
    testOk = null
    untrusted = []
  }
  $: canAddAccount = canSubmitPassword && testOk === true
</script>

<div class="screen" role="dialog" aria-modal="true" aria-label={$t('addMailbox.cta')}>
  <header class="head">
    {#if step === 'config' || step === 'oauth'}
      <button type="button" class="icon" aria-label={$t('wizard.back')} on:click={back}>
        <IconArrowLeft size={18} stroke={1.8} />
      </button>
    {:else if step === 'provider' && offerImport}
      <button type="button" class="icon" aria-label={$t('wizard.back')} on:click={backToStart}>
        <IconArrowLeft size={18} stroke={1.8} />
      </button>
    {:else}
      <span class="icon-spacer"></span>
    {/if}
    <span class="title">{$t('addMailbox.cta')}</span>
    <button type="button" class="icon" aria-label={$t('wizard.close')} on:click={() => dispatch('close')}>
      <IconX size={18} stroke={1.8} />
    </button>
  </header>

  <div class="body">
    <div class="content">
      {#if step === 'start'}
        <h3>{$t('wizard.start.title')}</h3>
        <p class="note">{$t('wizard.start.sub')}</p>
        <div class="start-choices">
          <button type="button" class="choice" on:click={() => (step = 'provider')}>
            <span class="choice-icon"><IconPlus size={22} stroke={1.6} /></span>
            <span class="choice-text">
              <span class="choice-title">{$t('wizard.start.addTitle')}</span>
              <span class="choice-sub">{$t('wizard.start.addSub')}</span>
            </span>
            <IconArrowRight size={16} stroke={1.8} />
          </button>
          <button type="button" class="choice" on:click={() => (showImport = true)}>
            <span class="choice-icon"><IconMailbox size={22} stroke={1.6} /></span>
            <span class="choice-text">
              <span class="choice-title">{$t('wizard.start.importTitle')}</span>
              <span class="choice-sub">{$t('wizard.start.importSub')}</span>
            </span>
            <IconArrowRight size={16} stroke={1.8} />
          </button>
        </div>
      {:else if step === 'provider'}
        <WizardProviders on:pick={pick} />
      {:else if step === 'config'}
        <h3>{preset?.label}</h3>
        {#if preset?.note}<p class="note">{preset.note}</p>{/if}

        <label class="field">
          <span>{$t('wizard.field.email')}</span>
          <input type="email" bind:value={draft.email} on:blur={maybeDiscover} placeholder={$t('wizard.field.emailPlaceholder')} />
        </label>
        {#if discoveredOAuth === 'google'}
          <div class="provider-hint">
            <span>{$t('wizard.workspace.detected')}</span>
            <button type="button" class="app-password-link" on:click={switchToGoogle}>
              {$t('wizard.workspace.useGoogle')}
            </button>
          </div>
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
          <span>{$t('wizard.field.password')}</span>
          <input
            type="password"
            bind:value={draft.password}
            placeholder={preset?.appPasswordUrl ? $t('wizard.field.appPasswordPlaceholder') : $t('wizard.field.passwordPlaceholder')}
          />
        </label>
        {#if preset?.appPasswordUrl}
          <div class="app-password-warning">
            <span>{preset?.id === 'gmail' ? $t('wizard.gmail.appPasswordWarning') : $t('wizard.appPassword.warning')}</span>
            <button type="button" class="app-password-link" on:click={() => BrowserOpenURL(preset?.appPasswordUrl ?? '')}>
              {$t('wizard.appPassword.link')}
            </button>
          </div>
        {/if}

        <div class="servers">
          <label class="field"><span>{$t('wizard.field.imapHost')}</span><input type="text" bind:value={draft.imapHost} on:input={() => touch('imapHost')} /></label>
          <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.imapPort} on:input={() => touch('imapPort')} /></label>
        </div>
        <div class="servers">
          <label class="field"><span>{$t('wizard.field.smtpHost')}</span><input type="text" bind:value={draft.smtpHost} on:input={() => touch('smtpHost')} /></label>
          <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.smtpPort} on:input={() => touch('smtpPort')} /></label>
        </div>

        <button type="button" class="disclosure" on:click={() => (showAdvanced = !showAdvanced)}>
          {showAdvanced ? $t('wizard.advanced.hide') : $t('wizard.advanced.show')} {$t('wizard.advanced.connectionSettings')}
        </button>
        {#if showAdvanced}
          <div class="advanced">
            <label class="field">
              <span>{$t('wizard.field.username')}</span>
              <input type="text" bind:value={draft.username} placeholder={draft.email || $t('wizard.field.usernamePlaceholder')} />
            </label>
            <p class="adv-hint">
              {$t('wizard.advanced.usernameHint')}
            </p>

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
            <p class="adv-hint">
              {$t('wizard.advanced.tlsHint')}
            </p>

            <span class="adv-label">{$t('certs.ca.label')}</span>
            {#if draft.caPem}
              <p class="adv-hint">{caSubjects.join(', ')}</p>
              <button type="button" class="ghost" on:click={removeCA}>{$t('certs.ca.remove')}</button>
            {:else}
              <button type="button" class="ghost" on:click={pickCA}>{$t('certs.ca.choose')}</button>
            {/if}
            <p class="adv-hint">{$t('certs.ca.hint')}</p>

            <AccountRouteFields bind:route={draft.proxy} />
          </div>
        {/if}

        {#if untrusted.length > 0}
          <CertificateReview certs={untrusted} busy={testing} on:trust={trustAndRetest} />
        {/if}

        {#if testOk === true}<p class="ok"><IconCheck size={14} stroke={2} /> {$t('wizard.connectionWorks')}</p>{/if}
        {#if testOk === null && canSubmitPassword}<p class="note">{$t('wizard.testRequiredHint')}</p>{/if}
        {#if error}<p class="err">{error}</p>{/if}

        <div class="actions">
          <button type="button" class="ghost" on:click={test} disabled={testing || !canSubmitPassword}>
            {testing ? $t('wizard.testing') : $t('wizard.testConnection')}
          </button>
          <button type="button" class="primary" on:click={addPassword} disabled={!canAddAccount}>
            {$t('addMailbox.cta')}
          </button>
        </div>
        {#if preset?.oauthOptional}
          <button type="button" class="disclosure" on:click={() => { error = ''; step = 'oauth' }}>
            {$t('wizard.gmail.useOauthInstead')}
          </button>
        {/if}
      {:else if step === 'oauth'}
        <h3>{$t('wizard.step.oauth.title')} {preset?.label}</h3>
        <p class="note">
          {$t('wizard.step.oauth.note')}
        </p>
        {#if preset?.oauthProvider === 'google'}
          <div class="provider-hint">
            <span class="setup-title">{$t('wizard.google.setup.title')}</span>
            <ol class="setup-steps">
              {#each googleSetup as s}
                <li>
                  <span>{$t(s.text)}</span>
                  <button type="button" class="app-password-link" on:click={() => BrowserOpenURL(s.url)}>
                    {$t(s.link)}
                  </button>
                </li>
              {/each}
            </ol>
            <span>{$t('wizard.google.setup.admin')}</span>
          </div>
        {/if}

        <label class="field">
          <span>{$t('wizard.field.email')}</span>
          <input type="email" bind:value={draft.email} placeholder={$t('wizard.field.emailPlaceholder')} />
        </label>
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
        <!-- the route is here too: a mailbox only reachable through its own
             proxy needs it for the sign-in and for the first connection. -->
        <button type="button" class="disclosure" on:click={() => (showAdvanced = !showAdvanced)}>
          {showAdvanced ? $t('wizard.advanced.hide') : $t('wizard.advanced.show')}
        </button>
        {#if showAdvanced}
          <div class="advanced">
            {#if preset?.allowClientSecret && !preset?.requireClientSecret}
              <label class="field">
                <span>{$t('wizard.field.oauthClientSecret')}</span>
                <input type="password" bind:value={draft.clientSecret} placeholder={$t('wizard.field.oauthClientSecretPlaceholder')} />
              </label>
              <p class="adv-hint">
                {$t('wizard.advanced.clientSecretHint')}
              </p>
            {/if}
            <AccountRouteFields bind:route={draft.proxy} oauth />
          </div>
        {/if}

        {#if error}<p class="err">{error}</p>{/if}

        <div class="actions">
          <button type="button" class="primary" on:click={signIn} disabled={!canSignIn}>
            {$t('wizard.signInWith')} {preset?.label}
          </button>
        </div>
        {#if preset?.oauthOptional}
          <button type="button" class="disclosure" on:click={() => { error = ''; step = 'config' }}>
            {$t('wizard.gmail.useAppPasswordInstead')}
          </button>
        {/if}
      {:else if step === 'working'}
        <Spinner label={workingMessage} />
      {:else if step === 'folders'}
        <div class="folders-step">
          <h3>{$t('wizard.folders.title')}</h3>
          <p class="note">{$t('wizard.folders.body')}</p>
          <ul class="folder-list">
            {#each folders as folder (folder.id)}
              <li>
                <label>
                  <input
                    type="checkbox"
                    checked={!unchecked.has(folder.id)}
                    on:change={() => toggleFolder(folder.id)}
                  />
                  <span class="folder-name">{folder.name}</span>
                </label>
              </li>
            {/each}
          </ul>
          <div class="folder-actions">
            <button type="button" class="ghost" disabled={applying} on:click={() => { unchecked = new Set(); void beginSync() }}>
              {$t('wizard.folders.skip')}
            </button>
            <button type="button" class="primary" disabled={applying} on:click={() => void beginSync()}>
              {applying ? $t('wizard.folders.applying') : $t('wizard.folders.confirm')}
            </button>
          </div>
        </div>
      {:else if step === 'done'}
        <div class="result">
          <IconCheck size={32} stroke={1.6} />
          <h3>{$t('wizard.done.title')}</h3>
          <p class="note">{draft.email} {$t('wizard.done.syncing')}</p>
          <button type="button" class="primary" on:click={() => dispatch('close')}>{$t('wizard.done.button')}</button>
        </div>
      {:else if step === 'error'}
        <div class="result">
          <h3>{$t('wizard.error.title')}</h3>
          <p class="err">{error}</p>
          <button type="button" class="ghost" on:click={() => (step = formStep)}>
            {$t('wizard.back')}
          </button>
        </div>
      {/if}
    </div>
  </div>
</div>

<!-- the import flow is the same modal Settings uses, code-split so the wizard
     does not carry it unless the user asks for it. -->
{#if showImport}
  {#await import('../settings/ImportThunderbirdModal.svelte') then m}
    <svelte:component this={m.default} on:close={() => (showImport = false)} />
  {/await}
{/if}

<style>
  /* the folder picker: a plain checklist, since the point is to scan a long
     list of server folders and untick the heavy ones. */
  .folders-step {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .folder-list {
    list-style: none;
    margin: 0;
    padding: var(--space-2);
    max-height: 260px;
    overflow-y: auto;
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
  }

  .folder-list label {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-2);
    font-size: var(--fz-body);
    cursor: pointer;
  }

  .folder-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .folder-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  /* the start step: two equal choices, styled like the onboarding provider rows
     so the two entry points into this flow look the same. */
  .start-choices {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .choice {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-primary);
    text-align: start;
    cursor: pointer;
  }
  .choice:hover {
    background: var(--surface-hover);
    border-color: var(--accent);
  }

  .choice-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--text-secondary);
  }

  .choice-text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
  }
  .choice-title {
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
  }
  .choice-sub {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .screen {
    position: fixed;
    inset: 0;
    /* above the onboarding overlay (120) so it is usable when launched from the
       onboarding mailbox step. */
    z-index: 150;
    display: flex;
    flex-direction: column;
    background: var(--surface-base);
    /* covers the whole window, so it has to keep the macOS traffic lights clear
       itself; zero on every other platform. */
    padding-top: var(--titlebar-lights);
  }

  .head {
    display: grid;
    grid-template-columns: 40px 1fr 40px;
    align-items: center;
    padding: var(--space-3) var(--space-5);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .title {
    text-align: center;
    font-weight: var(--fw-semibold);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  /* keep the back button off the very edge and the close button flush right so
     they sit balanced inside the header padding. */
  .head .icon:first-child {
    justify-self: start;
  }

  .head .icon:last-child {
    justify-self: end;
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon-spacer {
    width: 40px;
  }

  .body {
    flex: 1;
    overflow-y: auto;
  }

  .content {
    max-width: 480px;
    margin: 0 auto;
    padding: var(--space-6);
  }

  h3 {
    margin: 0 0 var(--space-2);
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }

  .note {
    margin: 0 0 var(--space-4);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    line-height: 1.5;
  }

  .app-password-warning {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
    margin: 0 0 var(--space-4);
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--warning-bg);
    color: var(--text-primary);
    font-size: var(--fz-label);
    line-height: 1.5;
  }

  .provider-hint {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
    margin: 0 0 var(--space-4);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-label);
    line-height: 1.5;
  }

  .provider-hint .app-password-link {
    color: var(--accent);
  }

  .setup-title {
    font-weight: var(--fw-medium);
  }

  .setup-steps {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding-left: var(--space-4);
  }

  .setup-steps li {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
  }

  .app-password-link {
    border: none;
    background: transparent;
    padding: 0;
    color: var(--warning);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    text-decoration: underline;
    cursor: var(--cursor-action);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-bottom: var(--space-3);
  }

  .field span {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .field input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    outline: none;
  }

  .field input:focus {
    border-color: var(--accent);
  }

  .servers {
    display: flex;
    gap: var(--space-3);
  }

  .servers .field {
    flex: 1;
  }

  .servers .field.narrow {
    flex: 0 0 88px;
  }

  /* the switch that reveals the local label field, laid out like a field row
     so it sits in the same rhythm as the inputs around it. */
  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    margin-bottom: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  /* a lightweight text toggle that reveals the advanced settings. */
  .disclosure {
    border: none;
    background: transparent;
    color: var(--accent);
    cursor: var(--cursor-action);
    font-size: var(--fz-label);
    padding: var(--space-1) 0;
    margin-bottom: var(--space-2);
  }

  .disclosure:hover {
    text-decoration: underline;
  }

  .advanced {
    margin-bottom: var(--space-3);
  }

  .adv-label {
    display: block;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    margin-bottom: var(--space-2);
  }

  /* space the segmented controls from a following label so imap and smtp
     security read as two distinct rows. */
  .advanced .seg {
    margin-bottom: var(--space-3);
  }

  /* a small two-option segmented control for the tls mode. */
  .seg {
    display: inline-flex;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }

  .seg button {
    border: none;
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    padding: var(--space-2) var(--space-4);
    font-size: var(--fz-label);
  }

  .seg button + button {
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .seg button.on {
    background: var(--accent);
    color: var(--accent-fg);
  }


  .adv-hint {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-3);
    margin-top: var(--space-4);
  }

  .primary,
  .ghost {
    padding: var(--space-2) var(--space-5);
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-default);
    cursor: var(--cursor-action);
    font-size: var(--fz-label);
  }

  .primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }

  .primary:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .ghost {
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .ghost:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .ok {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--success);
    font-size: var(--fz-label);
  }

  .err {
    color: var(--danger);
    font-size: var(--fz-label);
    word-break: break-word;
  }

  .result {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    text-align: center;
    color: var(--success);
    padding: var(--space-6) 0;
  }

  .result h3 {
    color: var(--text-primary);
  }
</style>
