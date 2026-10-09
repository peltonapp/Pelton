<script lang="ts">
  // the mailbox manager: lists configured accounts and lets the user edit an
  // account's display name and server settings, or delete it outright. Deleting
  // is destructive (it drops the cached mail and the keyring secret), so it goes
  // through an inline confirm. Email is not editable here, it identifies the
  // account; changing it is a re-add.
  import { onMount } from 'svelte'
  import { IconPencil, IconTrash, IconCheck, IconPlus, IconAlertTriangle, IconChevronRight } from '@tabler/icons-svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import Modal from '../common/Modal.svelte'
  import InfoTip from '../common/InfoTip.svelte'
  import StepSlider from './StepSlider.svelte'
  import {
    listAccounts,
    updateAccount,
    deleteAccount,
    getLogStatus,
    deleteLogs,
    chooseArchiveExportFolder,
    previewArchiveExportName,
    accountOAuthProvider,
    reauthorizeOAuthAccount,
    probeAccountCertificates,
    trustAccountCertificate,
    removeAccountTrustedCertificate,
    chooseCAFile,
    setAccountCA,
    accountProxyPasswordStored,
    testAccountRoute,
    getProxyConfig,
  } from '../../lib/api'
  import CertificateReview from '../common/CertificateReview.svelte'
  import AccountRouteFields from '../common/AccountRouteFields.svelte'
  import { routeSummary } from '../../lib/proxyroute'
  import { refreshSidebar } from '../../stores/accounts'
  import { prefs } from '../../stores/prefs'
  import { missingPassword, askForPassword, refreshMissingPasswords } from '../../stores/passwordprompt'
  import { retrySync } from '../../stores/syncfailures'
  import { errorMessage, toastError, toastSuccess, pushAction } from '../../stores/toast'
  import { accountLabel } from '../../lib/format'
  import { connectionSummary } from '../../lib/connection'
  import type { Account, ProxyConfig, TLSMode, UntrustedCert } from '../../lib/types'
  import { t } from '../../lib/i18n'

  let accounts: Account[] = []
  let loading = true
  let editingId: number | null = null
  let confirmingId: number | null = null
  let saving = false
  // the working copy of the account being edited, so cancelling discards edits.
  let draft: Account | null = null
  // the password is not part of the account row (it lives in the keyring and is
  // never sent back), so it is drafted separately. Empty means "leave it".
  let passwordDraft = ''
  // the oauth provider the edited account signs in with, empty for a password
  // account. Such an account gets "sign in again" instead of a password field.
  let oauthProvider = ''
  let reauthorizing = false
  const providerLabels: Record<string, string> = { google: 'Google', microsoft: 'Microsoft' }
  // the certificate check in the editor (#446): what the servers present that
  // the mailbox does not trust, null until the check has run.
  let probed: UntrustedCert[] | null = null
  let certBusy = false
  // the add-mailbox wizard is code-split like the other settings modals, so
  // it only loads once the user actually asks to add a mailbox.
  let wizardOpen = false
  // the sample file name the export settings show, rendered by the backend so
  // the ui never has a second copy of the naming rules.
  let namePreview = ''
  // the archiving and encryption settings, over the editor rather than in it:
  // they are two features deep and most edits here are a server or a password.
  let showAdvanced = false
  // the draft as it opened, so closing knows whether there is anything to lose.
  let opened = ''
  // the app-wide proxy, so a mailbox following it can say where that goes.
  let globalProxy: ProxyConfig | null = null
  let testingRoute = false

  $: dirty = draft !== null && (JSON.stringify(draft) !== opened || passwordDraft !== '')

  // the subfolder modes, in the order the picker shows them.
  const subfolderModes = ['none', 'year', 'month'] as const

  // how this mailbox starts a new message: unprotected, signed, or signed and
  // encrypted whenever every recipient has a key.
  const pgpDefaults = ['', 'sign', 'auto'] as const

  // the connection counts the per-mailbox slider offers; matches the global one.
  const parallelOptions = ['1', '2', '3', '4', '5'].map((n) => ({ key: n, label: n }))

  // switching the default off starts the override at the current global value.
  function setUseDefaultParallel(useDefault: boolean): void {
    if (draft) {
      draft.syncMaxParallel = useDefault ? null : $prefs.syncMaxParallel
    }
  }

  function setPGPDefault(value: string): void {
    if (draft) {
      draft.pgpDefault = value
    }
  }

  onMount(() => {
    void load()
    void loadGlobalProxy()
  })

  async function loadGlobalProxy(): Promise<void> {
    try {
      globalProxy = await getProxyConfig()
    } catch {
      // the route line then reads the app-wide setting as direct.
    }
  }

  function onMailboxAdded(): void {
    wizardOpen = false
    void load()
    void refreshSidebar()
  }

  async function load(): Promise<void> {
    loading = true
    try {
      accounts = await listAccounts()
      await refreshMissingPasswords()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      loading = false
    }
  }

  function startEdit(account: Account): void {
    confirmingId = null
    editingId = account.id
    // the backend already reports the security an account actually connects
    // with, resolving the empty value an older account carries, so the control
    // shows the truth and saving pins it.
    draft = { ...account, proxy: { ...account.proxy, password: '' } }
    opened = JSON.stringify(draft)
    showAdvanced = false
    passwordDraft = ''
    oauthProvider = ''
    probed = null
    void refreshPreview()
    void loadOAuthProvider(account.id)
    void loadProxyPasswordStored(account.id)
  }

  // loadProxyPasswordStored asks the keyring whether the mailbox's own proxy
  // has a password, which is only a placeholder here: the secret stays behind.
  // The answer is folded into what the draft opened with, so it is not an edit.
  async function loadProxyPasswordStored(id: number): Promise<void> {
    try {
      const stored = await accountProxyPasswordStored(id)
      if (editingId !== id || !draft) {
        return
      }
      draft.proxy.hasPassword = stored
      const base = JSON.parse(opened) as Account
      opened = JSON.stringify({ ...base, proxy: { ...base.proxy, hasPassword: stored } })
    } catch {
      // no placeholder; an empty field still keeps whatever is stored.
    }
  }

  async function testRoute(): Promise<void> {
    if (!draft) {
      return
    }
    testingRoute = true
    try {
      await testAccountRoute({
        accountId: draft.id,
        proxy: draft.proxy,
        imapHost: draft.imapHost,
        imapPort: draft.imapPort,
        smtpHost: draft.smtpHost,
        smtpPort: draft.smtpPort,
      })
      toastSuccess($t('mailboxes.route.testOk'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      testingRoute = false
    }
  }

  // loadOAuthProvider asks the keyring how the account signs in. A failure
  // leaves the password field, which is what the editor showed before.
  async function loadOAuthProvider(id: number): Promise<void> {
    try {
      const provider = await accountOAuthProvider(id)
      if (editingId === id) {
        oauthProvider = provider
      }
    } catch {
      // keep the password field.
    }
  }

  async function reauthorize(): Promise<void> {
    if (!draft) {
      return
    }
    reauthorizing = true
    try {
      await reauthorizeOAuthAccount(draft.id)
      toastSuccess($t('mailboxes.oauth.reauthDone'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      reauthorizing = false
    }
  }

  // certificate trust is applied straight away rather than on save, like the
  // password check: it is about the server, not an edit to the form. The
  // stored account is read back so the editor shows what was kept.
  async function reloadTrust(id: number): Promise<void> {
    accounts = await listAccounts()
    const fresh = accounts.find((a) => a.id === id)
    if (!fresh || !draft || draft.id !== id) {
      return
    }
    draft.trustedCerts = fresh.trustedCerts
    draft.caSubjects = fresh.caSubjects
    const base = JSON.parse(opened) as Account
    opened = JSON.stringify({ ...base, trustedCerts: fresh.trustedCerts, caSubjects: fresh.caSubjects })
  }

  async function withTrust(action: (id: number) => Promise<void>): Promise<void> {
    if (!draft) {
      return
    }
    const id = draft.id
    certBusy = true
    try {
      await action(id)
      await reloadTrust(id)
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      certBusy = false
    }
  }

  function checkCerts(): Promise<void> {
    return withTrust(async (id) => {
      probed = await probeAccountCertificates(id)
    })
  }

  function trustProbed(event: CustomEvent<UntrustedCert[]>): Promise<void> {
    return withTrust(async (id) => {
      for (const fp of new Set(event.detail.map((c) => c.fingerprint))) {
        await trustAccountCertificate(id, fp)
      }
      probed = null
      toastSuccess($t('certs.trusted'))
    })
  }

  function removePin(fp: string): Promise<void> {
    return withTrust((id) => removeAccountTrustedCertificate(id, fp))
  }

  function pickCA(): Promise<void> {
    return withTrust(async (id) => {
      const ca = await chooseCAFile()
      if (ca.pem !== '') {
        await setAccountCA(id, ca.pem)
      }
    })
  }

  function removeCA(): Promise<void> {
    return withTrust((id) => setAccountCA(id, ''))
  }

  // refreshPreview renders the current template through the backend. A failure
  // only costs the preview line, so it falls back to empty rather than a toast.
  async function refreshPreview(): Promise<void> {
    if (!draft) {
      return
    }
    try {
      namePreview = await previewArchiveExportName(draft.exportNameTemplate, draft.exportSubfolders)
    } catch {
      namePreview = ''
    }
  }

  function setUseLocalLabel(on: boolean): void {
    if (draft) {
      draft.useLocalLabel = on
    }
  }

  function setExportOnArchive(on: boolean): void {
    if (draft) {
      draft.exportOnArchive = on
    }
  }

  function setSubfolders(mode: string): void {
    if (draft) {
      draft.exportSubfolders = mode
      void refreshPreview()
    }
  }

  // pickExportFolder opens the native directory picker. Choosing a folder turns
  // the export on, since that is plainly what the click meant.
  async function pickExportFolder(): Promise<void> {
    if (!draft) {
      return
    }
    try {
      const dir = await chooseArchiveExportFolder()
      if (dir && draft) {
        draft.exportDir = dir
        draft.exportOnArchive = true
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // the security setters take the null check out of the template: inside an
  // event handler the draft's narrowing from the enclosing block is lost.
  function setIMAPTLS(mode: TLSMode): void {
    if (draft) {
      draft.imapTls = mode
    }
  }

  function setSMTPTLS(mode: TLSMode): void {
    if (draft) {
      draft.smtpTls = mode
    }
  }

  function cancelEdit(): void {
    editingId = null
    draft = null
    passwordDraft = ''
  }

  async function save(): Promise<void> {
    if (!draft) {
      return
    }
    saving = true
    try {
      const updated = await updateAccount({
        id: draft.id,
        displayName: draft.displayName,
        localLabel: draft.localLabel,
        useLocalLabel: draft.useLocalLabel,
        username: draft.username,
        imapHost: draft.imapHost,
        imapPort: draft.imapPort,
        smtpHost: draft.smtpHost,
        smtpPort: draft.smtpPort,
        password: passwordDraft,
        imapTls: draft.imapTls as TLSMode,
        smtpTls: draft.smtpTls as TLSMode,
        exportOnArchive: draft.exportOnArchive,
        exportDir: draft.exportDir,
        exportSubfolders: draft.exportSubfolders,
        exportNameTemplate: draft.exportNameTemplate,
        pgpDefault: draft.pgpDefault,
        syncMaxParallel: draft.syncMaxParallel ?? null,
        proxy: draft.proxy,
      })
      accounts = accounts.map((a) => (a.id === updated.id ? updated : a))
      if (passwordDraft !== '') {
        void refreshMissingPasswords()
        // a mailbox that had no password has not synced yet; waiting for the
        // next sync would leave it looking broken after the fix.
        void retrySync(updated.id)
      }
      void refreshSidebar()
      cancelEdit()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      saving = false
    }
  }

  async function confirmDelete(id: number): Promise<void> {
    try {
      await deleteAccount(id)
      accounts = accounts.filter((a) => a.id !== id)
      confirmingId = null
      void refreshSidebar()
      void offerLogCleanup()
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // a log written while a mailbox existed is still a record of using it, so
  // deleting the mailbox offers to take the logs with it (#211). Nothing is
  // removed without the click: the logs may be the reason logging was on.
  async function offerLogCleanup(): Promise<void> {
    let status
    try {
      status = await getLogStatus()
    } catch {
      return
    }
    if (status.sizeBytes === 0) {
      return
    }
    pushAction(
      'info',
      $t('mailboxes.deleteLogsOffer'),
      {
        label: $t('settingsPanel.button.deleteLogs'),
        run: () => {
          deleteLogs().catch((err) => toastError(errorMessage(err)))
        },
      },
      8000,
    )
  }
</script>

<div class="head">
  <div>
    <h3>{$t('settingsPanel.category.mailboxes')}</h3>
    <p class="hint">{$t('mailboxes.hint')}</p>
  </div>
  <button type="button" class="add-btn" on:click={() => (wizardOpen = true)}>
    <IconPlus size={14} stroke={2} />
    {$t('mailboxes.add')}
  </button>
</div>

{#if loading}
  <p class="empty">{$t('mailboxes.loading')}</p>
{:else if accounts.length === 0}
  <p class="empty">{$t('mailboxes.empty')}</p>
{:else}
  <ul class="list">
    {#each accounts as account (account.id)}
      {@const summary = connectionSummary(account, $t('sidebar.localFolders'))}
      <li>
        <div class="who">
          <span class="name">{accountLabel(account)}</span>
          {#if accountLabel(account) !== account.email}<span class="addr">{account.email}</span>{/if}
          {#if summary !== accountLabel(account)}<span class="addr">{summary}</span>{/if}
        </div>
        {#if $missingPassword.has(account.id)}
          <button
            type="button"
            class="icon warn-icon"
            title={$t('mailboxes.passwordPrompt.marker')}
            aria-label={$t('mailboxes.passwordPrompt.marker')}
            on:click={() => askForPassword(account)}
          >
            <IconAlertTriangle size={15} stroke={1.8} />
          </button>
        {/if}
        {#if confirmingId === account.id}
          <div class="confirm">
            <span class="warn">{$t('mailboxes.deleteConfirm')}</span>
            <button type="button" class="danger" on:click={() => confirmDelete(account.id)}>{$t('action.delete')}</button>
            <button type="button" class="ghost" on:click={() => (confirmingId = null)}>{$t('mailboxes.cancel')}</button>
          </div>
        {:else}
          <button type="button" class="icon" aria-label={`${$t('mailboxes.edit')} ${account.email}`} on:click={() => startEdit(account)}>
            <IconPencil size={15} stroke={1.7} />
          </button>
          <button type="button" class="icon del" aria-label={`${$t('action.delete')} ${account.email}`} on:click={() => ((confirmingId = account.id), (editingId = null))}>
            <IconTrash size={15} stroke={1.7} />
          </button>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

{#if draft}
  <Modal title={draft.email} hint={$t('mailboxes.serverChangeHint')} size="large" {dirty} on:close={cancelEdit}>
    <div class="form">
      <label class="field">
        <span>
          {$t('wizard.field.fromName')}
          <InfoTip text={$t('mailboxes.fromNameHint')} />
        </span>
        <input type="text" bind:value={draft.displayName} />
      </label>
      <!-- the local label is the one that never leaves the machine, so it says
           so, and switching it off keeps what was typed (#326). -->
      <div class="toggle">
        <span>{$t('mailboxes.localLabel.toggle')}</span>
        <ToggleSwitch
          checked={draft.useLocalLabel}
          label={$t('mailboxes.localLabel.toggle')}
          on:change={(e) => setUseLocalLabel(e.detail)}
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
        <span>{$t('wizard.field.username')}</span>
        <input type="text" bind:value={draft.username} placeholder={draft.email} />
      </label>
      <div class="servers">
        <label class="field"><span>{$t('wizard.field.imapHost')}</span><input type="text" bind:value={draft.imapHost} /></label>
        <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.imapPort} /></label>
      </div>
      <div class="servers">
        <span class="field">
          <span>{$t('wizard.advanced.imapSecurity')}</span>
          <div class="seg" role="radiogroup" aria-label={$t('wizard.advanced.imapSecurity')}>
            <button type="button" class:on={draft.imapTls === 'ssl'} on:click={() => setIMAPTLS('ssl')}>SSL / TLS</button>
            <button type="button" class:on={draft.imapTls === 'starttls'} on:click={() => setIMAPTLS('starttls')}>STARTTLS</button>
          </div>
        </span>
      </div>
      <div class="servers">
        <label class="field"><span>{$t('wizard.field.smtpHost')}</span><input type="text" bind:value={draft.smtpHost} /></label>
        <label class="field narrow"><span>{$t('wizard.field.port')}</span><input type="number" bind:value={draft.smtpPort} /></label>
      </div>
      <div class="servers">
        <span class="field">
          <span>{$t('wizard.advanced.smtpSecurity')}</span>
          <div class="seg" role="radiogroup" aria-label={$t('wizard.advanced.smtpSecurity')}>
            <button type="button" class:on={draft.smtpTls === 'ssl'} on:click={() => setSMTPTLS('ssl')}>SSL / TLS</button>
            <button type="button" class:on={draft.smtpTls === 'starttls'} on:click={() => setSMTPTLS('starttls')}>STARTTLS</button>
          </div>
        </span>
      </div>
      {#if !draft.local}
        <div class="toggle">
          <span>{$t('mailboxes.syncParallel.useDefaultNamed').replace('{n}', String($prefs.syncMaxParallel))}</span>
          <ToggleSwitch
            checked={draft.syncMaxParallel == null}
            label={$t('mailboxes.syncParallel.useDefaultNamed').replace('{n}', String($prefs.syncMaxParallel))}
            on:change={(e) => setUseDefaultParallel(e.detail)}
          />
        </div>
        {#if draft.syncMaxParallel != null}
          <StepSlider
            label={$t('mailboxes.syncParallel.label')}
            value={String(draft.syncMaxParallel)}
            options={parallelOptions}
            on:change={(e) => {
              if (draft) draft.syncMaxParallel = Number(e.detail)
            }}
          />
        {/if}
      {/if}
      {#if oauthProvider}
        <div class="field">
          <span>{$t('mailboxes.oauth.label')}</span>
          <p class="server-hint">
            {$t('mailboxes.oauth.hint').replace('{provider}', providerLabels[oauthProvider] ?? oauthProvider)}
          </p>
          <button type="button" class="ghost reauth" on:click={reauthorize} disabled={reauthorizing}>
            {reauthorizing ? $t('mailboxes.oauth.reauthWorking') : $t('mailboxes.oauth.reauth')}
          </button>
        </div>
      {:else}
        <label class="field">
          <span>{$t('wizard.field.password')}</span>
          <input
            type="password"
            bind:value={passwordDraft}
            autocomplete="off"
            placeholder={$t(
              $missingPassword.has(draft.id)
                ? 'mailboxes.passwordMissing'
                : 'mailboxes.passwordUnchanged',
            )}
          />
        </label>
        {#if $missingPassword.has(draft.id)}
          <p class="server-hint warn">{$t('mailboxes.passwordNeededHint')}</p>
        {/if}
      {/if}

      <div class="field">
        <span>{$t('certs.section.label')}</span>
        {#if draft.trustedCerts.length > 0}
          <ul class="pins">
            {#each draft.trustedCerts as fp (fp)}
              <li>
                <code>{fp}</code>
                <button type="button" class="ghost small" disabled={certBusy} on:click={() => removePin(fp)}>{$t('certs.pin.remove')}</button>
              </li>
            {/each}
          </ul>
        {:else}
          <p class="server-hint">{$t('certs.pins.none')}</p>
        {/if}
        {#if draft.caSubjects.length > 0}
          <p class="server-hint">{$t('certs.ca.current').replace('{subjects}', draft.caSubjects.join(', '))}</p>
        {/if}
        <div class="cert-actions">
          <button type="button" class="ghost small" disabled={certBusy} on:click={checkCerts}>{$t('certs.check')}</button>
          {#if draft.caSubjects.length > 0}
            <button type="button" class="ghost small" disabled={certBusy} on:click={removeCA}>{$t('certs.ca.remove')}</button>
          {:else}
            <button type="button" class="ghost small" disabled={certBusy} on:click={pickCA}>{$t('certs.ca.choose')}</button>
          {/if}
        </div>
        {#if probed && probed.length === 0}
          <p class="server-hint">{$t('certs.allTrusted')}</p>
        {:else if probed}
          <CertificateReview certs={probed} busy={certBusy} on:trust={trustProbed} />
        {/if}
      </div>

      <!-- the route is set under Advanced, but where the mail goes is stated
           here, so a proxy is never in use without it being visible. -->
      <div class="field">
        <span>{$t('mailboxes.route.label')}</span>
        <p class="server-hint route-line">{routeSummary(draft.proxy, globalProxy, $t)}</p>
      </div>

    </div>

    <button type="button" class="more" on:click={() => (showAdvanced = true)}>
      {$t('mailboxes.advanced.title')}
      <IconChevronRight size={15} stroke={1.7} />
    </button>
    <svelte:fragment slot="footer">
      <button type="button" class="ghost" on:click={cancelEdit}>{$t('mailboxes.cancel')}</button>
      <button type="button" class="primary" disabled={saving} on:click={save}>
        <IconCheck size={14} stroke={2} />
        {saving ? $t('mailboxes.saving') : $t('mailboxes.save')}
      </button>
    </svelte:fragment>
  </Modal>

  {#if showAdvanced}
    <Modal
      title={$t('mailboxes.advanced.title')}
      hint={$t('mailboxes.advanced.hint')}
      size="medium"
      on:close={() => (showAdvanced = false)}
    >
      <section class="group">
        <h4>{$t('mailboxes.section.archiving')}</h4>
        <div class="form">
          <div class="folder-row">
            <span class="path" class:unset={!draft.exportDir}>
              {draft.exportDir || $t('mailboxes.export.noFolder')}
            </span>
            <button type="button" class="ghost" on:click={pickExportFolder}>
              {$t('mailboxes.export.choose')}
            </button>
          </div>
          <!-- there is nowhere to write without a folder, so the switch stays
               off until one is picked. -->
          <div class="toggle">
            <span class:muted={!draft.exportDir}>{$t('mailboxes.export.toggle')}</span>
            <ToggleSwitch
              checked={draft.exportOnArchive}
              disabled={!draft.exportDir}
              label={$t('mailboxes.export.toggle')}
              on:change={(e) => setExportOnArchive(e.detail)}
            />
          </div>
          <p class="server-hint" class:warn={!draft.exportDir}>
            {draft.exportDir ? $t('mailboxes.export.hint') : $t('mailboxes.export.needFolder')}
          </p>

          {#if draft.exportDir}
            <span class="field">
              <span>{$t('mailboxes.export.subfolders')}</span>
              <div class="seg" role="radiogroup" aria-label={$t('mailboxes.export.subfolders')}>
                {#each subfolderModes as mode (mode)}
                  <button type="button" class:on={draft.exportSubfolders === mode} on:click={() => setSubfolders(mode)}>
                    {$t(`mailboxes.export.subfolders.${mode}`)}
                  </button>
                {/each}
              </div>
            </span>
            <label class="field">
              <span>{$t('mailboxes.export.template')}</span>
              <input
                type="text"
                bind:value={draft.exportNameTemplate}
                on:input={refreshPreview}
                placeholder="{'{date}'}_{'{subject}'}"
              />
            </label>
            <p class="server-hint">{$t('mailboxes.export.placeholders')}</p>
            {#if namePreview}
              <p class="server-hint preview">{$t('mailboxes.export.preview')} <code>{namePreview}</code></p>
            {/if}
          {/if}
        </div>
      </section>

      <section class="group">
        <h4>{$t('mailboxes.section.encryption')}</h4>
        <div class="form">
          <span class="field">
            <span>{$t('mailboxes.pgpDefault')}</span>
            <div class="seg" role="radiogroup" aria-label={$t('mailboxes.pgpDefault')}>
              {#each pgpDefaults as mode (mode)}
                <button type="button" class:on={draft.pgpDefault === mode} on:click={() => setPGPDefault(mode)}>
                  {$t(`mailboxes.pgpDefault.${mode || 'off'}`)}
                </button>
              {/each}
            </div>
          </span>
          <p class="server-hint">{$t('mailboxes.pgpDefaultHint')}</p>
        </div>
      </section>

      <section class="group">
        <h4>{$t('mailboxes.section.network')}</h4>
        <div class="form">
          <AccountRouteFields bind:route={draft.proxy} oauth={oauthProvider !== ''} />
          <div class="cert-actions">
            <button type="button" class="ghost small" disabled={testingRoute} on:click={testRoute}>
              {testingRoute ? $t('wizard.testing') : $t('mailboxes.route.test')}
            </button>
          </div>
        </div>
      </section>

      <svelte:fragment slot="footer">
        <button type="button" class="primary" on:click={() => (showAdvanced = false)}>{$t('modal.close')}</button>
      </svelte:fragment>
    </Modal>
  {/if}
{/if}

{#if wizardOpen}
  {#await import('../wizard/AddMailboxWizard.svelte') then m}
    <svelte:component this={m.default} on:close={() => (wizardOpen = false)} on:added={onMailboxAdded} />
  {/await}
{/if}

<style>
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-3);
  }

  .add-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    flex-shrink: 0;
    padding: var(--space-2) var(--space-4);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
  }
  .add-btn:hover {
    filter: brightness(1.05);
  }

  h3 {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .hint {
    margin: 0 0 var(--space-4);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .empty {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    padding: var(--space-3) 0;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-1);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-size: var(--fz-label);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .addr {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .icon {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
    flex-shrink: 0;
  }
  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .icon.del:hover {
    color: var(--danger);
  }

  /* the mailbox cannot sync, so this one is coloured at rest rather than on
     hover like the edit and delete buttons next to it. */
  .warn-icon {
    color: var(--warning);
  }
  .warn-icon:hover {
    color: var(--warning);
  }

  .confirm {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .warn {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .muted {
    opacity: 0.6;
  }

  /* the way into the second modal, with the chevron so it reads as somewhere
     to go rather than another setting. */
  .more {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    align-self: flex-start;
    margin-top: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }
  .more:hover {
    background: var(--surface-hover);
  }

  /* the two halves of the extras modal, so archiving and encryption do not
     read as one long list of unrelated controls. */
  .group + .group {
    margin-top: var(--space-5);
    padding-top: var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .group h4 {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }


  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    width: 100%;
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .field span {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
  .field input {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-primary);
    font-size: var(--fz-list);
  }
  .field input:focus {
    border-color: var(--accent);
    outline: none;
  }

  .servers {
    display: flex;
    gap: var(--space-2);
  }
  .servers .field {
    flex: 1;
  }

  /* the security picker, matching the one in the add-mailbox wizard. */
  .seg {
    display: inline-flex;
    align-self: flex-start;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    overflow: hidden;
  }
  .seg button {
    border: none;
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: pointer;
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
  .servers .field.narrow {
    flex: 0 0 88px;
  }

  .server-hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .route-line {
    color: var(--text-secondary);
  }

  /* the export-on-archive block: a switch, the chosen folder, and a preview of
     the file name the template produces. */
  .toggle {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .folder-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    direction: rtl;
    text-align: start;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .path.unset {
    color: var(--text-tertiary);
    direction: ltr;
  }

  .preview code {
    font-family: var(--font-mono);
    color: var(--text-secondary);
    overflow-wrap: anywhere;
  }

  .primary,
  .ghost,
  .danger {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border-radius: var(--radius-control);
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    cursor: var(--cursor-action);
    border: var(--hairline) solid var(--border-default);
  }
  .primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }
  .primary:disabled {
    opacity: 0.6;
    cursor: default;
  }
  .ghost {
    background: transparent;
    color: var(--text-secondary);
  }
  .ghost.small {
    padding: var(--space-1) var(--space-3);
    font-size: var(--fz-meta);
  }
  .pins {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .pins li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .pins code {
    min-width: 0;
    overflow-wrap: anywhere;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }
  .cert-actions {
    display: flex;
    gap: var(--space-2);
  }
  .ghost:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .danger {
    background: var(--danger);
    color: var(--accent-fg);
    border-color: transparent;
  }
  .reauth {
    align-self: flex-start;
  }
</style>
