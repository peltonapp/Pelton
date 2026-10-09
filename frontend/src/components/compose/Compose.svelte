<script lang="ts">
  // a single compose pane. it edits one session: addresses, subject, the body in
  // the chosen editor mode, and attachments. the floating pane can be minimized
  // to its title bar or expanded to fill the window. markdown mode gets a
  // formatting toolbar and a github-style live preview. send enqueues to the
  // outbox (with the undo-send window when enabled); save stores a local draft.
  import { formatWeekdayTime, type TimeFormat } from '../../lib/format'
  import { onMount, tick } from 'svelte'
  import { marked } from 'marked'
  import {
    IconX,
    IconSend,
    IconDeviceFloppy,
    IconMinus,
    IconArrowsDiagonal,
    IconArrowsDiagonalMinimize2,
    IconTrash,
    IconChevronDown,
    IconClock,
    IconSunset2,
    IconSun,
    IconCalendarWeek,
  } from '@tabler/icons-svelte'
  import AddressFields from './AddressFields.svelte'
  import EditorModeSwitch from './EditorModeSwitch.svelte'
  import Select from '../common/Select.svelte'
  import EditorToolbar from './EditorToolbar.svelte'
  import AttachmentPicker from './AttachmentPicker.svelte'
  import DateTimePicker from '../common/DateTimePicker.svelte'
  import { currentUIScale } from '../../theme/theme'
  import { updateCompose, closeCompose, closeRequest, setComposeFullscreenDefault, type ComposeSession } from '../../stores/compose'
  import { signatures, signatureById, getAccountSignatures } from '../../stores/signatures'
  import type { Signature } from '../../lib/types'
  import { sidebar } from '../../stores/accounts'
  import { sendMessage, saveDraft, deleteDraft } from '../../lib/api'
  import { loadOutbox } from '../../stores/outbox'
  import { scheduleUndo } from '../../stores/undosend'
  import { prefs } from '../../stores/prefs'
  import { shortcutTitle, bindings } from '../../stores/shortcuts'
  import { comboMatches } from '../../lib/shortcuts'
  import { bodyFontStack } from '../../lib/fonts'
  import { buildRequest, hasRecipients } from '../../lib/mailcompose'
  import ProtectionPicker from './ProtectionPicker.svelte'
  import { composeProtectionStatus } from '../../lib/api'
  import type { ProtectionStatus } from '../../lib/types'
  import { atTime, addDays, nextWeekday } from '../../lib/datepresets'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import type CodeMirrorEditor from './CodeMirrorEditor.svelte'

  // the mail body font setting flows into the editors as a css variable so
  // what you type looks like what recipients without their own fonts see
  // (#64). null (the default) leaves each editor's built-in font untouched.
  let composeFont: string | null = null
  $: composeFont = bodyFontStack($prefs.bodyFont)
  import type { EditorMode } from '../../lib/types'
  import { t } from '../../lib/i18n'

  export let session: ComposeSession

  let sending = false
  // what signing and encryption are possible for this account and the current
  // recipients. It is refreshed as the recipient list changes, because a
  // control that offers encryption to somebody with no key is a control that
  // fails at send time.
  let protectionStatus: ProtectionStatus | null = null
  // the recipient list the status was fetched for, so an unchanged list does
  // not refetch on every keystroke elsewhere in the form.
  let statusFor = ''
  // suggestedApplied keeps the account default from overriding a choice the
  // user made: it is a starting point, once, not a correction.
  let suggestedApplied = false
  let preview = false
  let confirmClose = false
  let editorGrabFocus = false

  // send-later dropdown state: caret toggles the menu, presets are built fresh
  // each time it opens so "tomorrow" is always relative to now. the menu is
  // fixed-positioned (anchored to the caret button) rather than absolute, since
  // the compose pane clips overflow.
  let sendMenuOpen = false
  let sendCaretEl: HTMLButtonElement
  let sendMenuEl: HTMLDivElement
  let sendMenuLeft = 0
  let sendMenuTop = 0
  let customSendValue = ''

  interface SendPreset {
    label: string
    when: Date
    icon: typeof IconClock
  }

  // buildSendPresets mirrors SnoozeDialog's buildPresets: a short list of
  // friendly relative times, filtering out ones already in the past and
  // deduplicating "tomorrow" and "next Monday" when they land on the same day.
  function buildSendPresets(now: Date, tr: (key: string) => string): SendPreset[] {
    const out: SendPreset[] = []
    const laterToday = new Date(now.getTime() + 3 * 60 * 60 * 1000)
    out.push({ label: tr('compose.sendLater.laterToday'), when: laterToday, icon: IconClock })

    const evening = atTime(now, 18, 0)
    if (evening.getTime() > now.getTime() + 60 * 1000) {
      out.push({ label: tr('compose.sendLater.thisEvening'), when: evening, icon: IconSunset2 })
    }

    const tomorrow = atTime(addDays(now, 1), 8, 0)
    out.push({ label: tr('compose.sendLater.tomorrowMorning'), when: tomorrow, icon: IconSun })

    const monday = atTime(nextWeekday(now, 1), 8, 0)
    if (monday.toDateString() !== tomorrow.toDateString()) {
      out.push({ label: tr('compose.sendLater.mondayMorning'), when: monday, icon: IconCalendarWeek })
    }
    return out
  }

  $: sendPresets = sendMenuOpen ? buildSendPresets(new Date(), $t) : []
  $: formattedSendPresets = sendPresets.map((p) => ({ ...p, sub: formatSendWhen(p.when) }))

  function formatSendWhen(d: Date): string {
    return formatWeekdayTime(d, $prefs.timeFormat as TimeFormat)
  }

  // toggleSendMenu opens the menu anchored above the caret button (fixed
  // positioning, since the compose pane itself clips overflow) or closes it.
  // position is computed after render, once the menu's own size is known, and
  // clamped to the viewport so it never runs off-window regardless of where
  // the compose pane (and its caret) happens to sit on screen.
  async function toggleSendMenu(): Promise<void> {
    if (sending) {
      return
    }
    if (sendMenuOpen) {
      closeSendMenu()
      return
    }
    sendMenuOpen = true
    await tick()
    if (sendCaretEl && sendMenuEl) {
      // the app applies an interface zoom via css `zoom` on <html> (see
      // ContextMenu.svelte): getBoundingClientRect position stays in unscaled
      // screen pixels while a `position: fixed` element is placed in the
      // zoomed layout space, so raw rect values must be divided by the scale
      // (a no-op at 100%) before use as fixed left/top. offsetWidth/Height are
      // already layout-space.
      const scale = currentUIScale()
      const margin = 8
      const caretRect = sendCaretEl.getBoundingClientRect()
      const caretRight = caretRect.right / scale
      const caretTop = caretRect.top / scale
      const menuW = sendMenuEl.offsetWidth
      const menuH = sendMenuEl.offsetHeight
      const vw = window.innerWidth / scale
      const vh = window.innerHeight / scale
      const maxLeft = vw - menuW - margin
      const maxTop = vh - menuH - margin
      sendMenuLeft = Math.min(Math.max(caretRight - menuW, margin), Math.max(margin, maxLeft))
      sendMenuTop = Math.min(Math.max(caretTop - 8 - menuH, margin), Math.max(margin, maxTop))
    }
  }

  function closeSendMenu(): void {
    sendMenuOpen = false
    customSendValue = ''
  }

  function onSendMenuKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      closeSendMenu()
    }
  }

  // sendScheduled validates the chosen time is still in the future (the menu
  // may have been left open a while) and sends with it as the scheduled time.
  async function sendScheduled(when: Date): Promise<void> {
    if (when.getTime() <= Date.now()) {
      toastError($t('compose.sendLater.pickFutureTime'))
      return
    }
    closeSendMenu()
    await send(when)
  }

  function confirmCustomSend(): void {
    if (!customSendValue) {
      return
    }
    void sendScheduled(new Date(customSendValue))
  }
  // the CodeMirror editor instance, used by the markdown toolbar to format the
  // selection. one editor is mounted per non-wysiwyg mode.
  let editor: CodeMirrorEditor

  // CodeMirror is code-split (it is sizeable) and loaded the first time a
  // non-wysiwyg compose is shown, keeping it out of the startup bundle.
  let CMEditor: typeof CodeMirrorEditor | null = null
  $: if (session.mode !== 'wysiwyg' && !CMEditor) {
    void import('./CodeMirrorEditor.svelte').then((m) => (CMEditor = m.default))
  }

  // the rich editor (tiptap) is code-split: it only loads when the session is in
  // wysiwyg mode, keeping prosemirror out of the main bundle.
  let RichEditor: typeof import('./RichEditor.svelte').default | null = null
  $: if (session.mode === 'wysiwyg' && !RichEditor) {
    void import('./RichEditor.svelte').then((m) => (RichEditor = m.default))
  }

  $: accounts = $sidebar.data?.accounts ?? []
  $: previewHtml = marked.parse(session.body || '', { async: false }) as string

  // withBlock inserts a signature block into a body: headers go to the top,
  // footers to the bottom after the standard "-- " delimiter.
  function withBlock(body: string, sig: Signature): string {
    if (sig.kind === 'header') {
      return `${sig.content}\n\n${body}`
    }
    return `${body.replace(/\s*$/, '')}\n\n-- \n${sig.content}`
  }

  // on first open, insert the account's default header/footer once. drafts and
  // reopened messages have signaturesApplied already set, so they are skipped.
  onMount(async () => {
    if (session.to.trim().length == 0) {
      const input = document.querySelector<HTMLInputElement>(`#to-${session.id}`)
      input?.focus()
    } else if (session.subject.trim().length == 0) {
      const input = document.querySelector<HTMLInputElement>(`#subject-${session.id}`)
      input?.focus()
    } else {
      editorGrabFocus = true
    }

    if (session.signaturesApplied) {
      return
    }
    try {
      const assigned = await getAccountSignatures(session.accountId)
      let body = session.body
      const header = signatureById(assigned.headerId)
      const footer = signatureById(assigned.footerId)
      if (header) {
        body = withBlock(body, header)
      }
      if (footer) {
        body = withBlock(body, footer)
      }
      updateCompose(session.id, { body, signaturesApplied: true })
    } catch {
      updateCompose(session.id, { signaturesApplied: true })
    }
  })

  // insertSignature lets the user drop a different block into the body from the
  // compose footer menu (the "change in compose" path).
  // the signature picker runs an action rather than holding a setting, so it
  // snaps back to its placeholder after each use instead of showing whichever
  // block was inserted last.
  let sigChoice = ''

  function insertSignature(event: CustomEvent<string>): void {
    const sig = signatureById(Number(event.detail))
    sigChoice = ''
    if (!sig) {
      return
    }
    updateCompose(session.id, { body: withBlock(session.body, sig) })
  }

  function setMode(event: CustomEvent<EditorMode>): void {
    if (event.detail !== 'markdown') {
      preview = false
    }
    updateCompose(session.id, { mode: event.detail })
  }

  function toggleFullscreen(): void {
    const fullscreen = !session.fullscreen
    updateCompose(session.id, { fullscreen, minimized: false })
    // remember the size so the next compose opens the same way.
    setComposeFullscreenDefault(fullscreen)
  }

  function toggleMinimize(): void {
    updateCompose(session.id, { minimized: !session.minimized })
  }

  // applyFormat applies the requested markdown formatting to the CodeMirror
  // selection via the editor's exposed methods (which dispatch the change and
  // keep focus/caret correct).
  function applyFormat(action: string): void {
    if (!editor) {
      return
    }
    switch (action) {
      case 'bold':
        editor.wrapSelection('**', 'bold text')
        break
      case 'italic':
        editor.wrapSelection('*', 'italic text')
        break
      case 'code':
        editor.wrapSelection('`', 'code')
        break
      case 'link':
        editor.insertLink()
        break
      case 'heading':
        editor.linePrefix('## ')
        break
      case 'list':
        editor.linePrefix('- ')
        break
      case 'quote':
        editor.linePrefix('> ')
        break
    }
  }

  // send enqueues the message. with no scheduledAt it goes through the normal
  // undo-send delay (if enabled); with scheduledAt it is held until that exact
  // time instead, and skips the undo-send window entirely (see
  // resolveNotBefore in internal/desktop/bind_compose.go for the backend side
  // of this precedence).
  async function send(scheduledAt?: Date): Promise<void> {
    if (!hasRecipients(session)) {
      toastError($t('compose.error.noRecipients'))
      return
    }
    sending = true
    try {
      const snapshot: ComposeSession = { ...session }
      const req = buildRequest(session)
      if (scheduledAt) {
        req.sendAt = scheduledAt.toISOString()
      }
      const id = await sendMessage(req)
      if (session.draftId) {
        await deleteDraft(session.draftId)
      }
      await loadOutbox()
      closeCompose(session.id)

      if (scheduledAt) {
        toastSuccess($t('compose.sendLater.scheduledToast').replace('{when}', formatSendWhen(scheduledAt)))
      } else {
        const delay = $prefs.sendDelaySeconds
        if (delay > 0) {
          scheduleUndo(id, snapshot, delay)
        } else {
          toastSuccess($t('compose.toast.queued'))
        }
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      sending = false
    }
  }

  // onSendKey sends on the send shortcut (#480) from anywhere in the pane. It
  // listens while the event is still on its way down, because both editors
  // bind the same key themselves (a blank line, a hard break) and must not
  // see it. A plain input still gets it, so an address typed into a recipient
  // field becomes a chip before the message goes, and the tick lets that
  // change reach the session first.
  async function onSendKey(event: KeyboardEvent): Promise<void> {
    const combo = $bindings.send
    if (!combo || !comboMatches(event, combo) || sending || confirmClose || session.minimized) {
      return
    }
    event.preventDefault()
    if (!(event.target instanceof HTMLInputElement)) {
      event.stopPropagation()
    }
    await tick()
    await send()
  }

  async function save(): Promise<void> {
    try {
      const id = await saveDraft(session.draftId, buildRequest(session))
      updateCompose(session.id, { draftId: id })
      toastSuccess($t('compose.toast.draftSaved'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // hasContent decides whether closing needs a save/discard prompt: any
  // recipient, subject or body text counts, but a session that was only ever
  // opened and never touched should close silently.
  // recipientList is every address currently typed in, which is what the
  // status is about.
  $: recipientList = [session.to, session.cc, session.bcc]
    .join(',')
    .split(/[,;]/)
    .map((part) => {
      const match = part.match(/<([^>]+)>/)
      return (match ? match[1] : part).trim().toLowerCase()
    })
    .filter((email) => email.includes('@'))

  $: void refreshProtection(session.accountId, recipientList.join(','))

  // refreshProtection asks the backend what is possible. A failure leaves the
  // controls as they were rather than pretending nothing can be protected.
  async function refreshProtection(accountId: number, key: string): Promise<void> {
    if (key === statusFor && protectionStatus !== null) {
      return
    }
    statusFor = key
    try {
      const status = await composeProtectionStatus(accountId, recipientList)
      protectionStatus = status as unknown as ProtectionStatus
      if (!suggestedApplied) {
        suggestedApplied = true
        if (status.suggested !== 'none') {
          updateCompose(session.id, { protection: status.suggested })
        }
      }
      // the recipient list changed and encryption is no longer possible: drop
      // it rather than leaving a control on that would fail the send.
      if (!protectionStatus.canEncrypt && (session.protection === 'encrypt' || session.protection === 'signencrypt')) {
        updateCompose(session.id, { protection: session.protection === 'signencrypt' ? 'sign' : 'none' })
      }
    } catch {
      // leave whatever was last known; the send still refuses on its own if
      // the keys are not there.
    }
  }

  function hasContent(): boolean {
    return (
      session.to.trim().length > 0 ||
      session.cc.trim().length > 0 ||
      session.bcc.trim().length > 0 ||
      session.subject.trim().length > 0 ||
      session.body.trim().length > 0
    )
  }

  // an outside close (the close-window action) lands here rather than removing
  // the session, so an unsaved draft still gets its prompt.
  $: if ($closeRequest === session.id) {
    closeRequest.set(null)
    requestClose()
  }

  function requestClose(): void {
    if (hasContent()) {
      confirmClose = true
      return
    }
    closeCompose(session.id)
  }

  async function saveAndClose(): Promise<void> {
    try {
      await saveDraft(session.draftId, buildRequest(session))
      toastSuccess($t('compose.toast.draftSaved'))
      closeCompose(session.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function discardAndClose(): Promise<void> {
    confirmClose = false
    try {
      if (session.draftId) {
        await deleteDraft(session.draftId)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
    closeCompose(session.id)
  }
</script>

<!-- the key listener only watches what the fields inside send up; the pane
     itself is not something to focus. -->
<!-- svelte-ignore a11y-interactive-supports-focus -->
<div
  class="compose"
  class:fullscreen={session.fullscreen}
  class:minimized={session.minimized}
  data-compose-id={session.id}
  role="dialog"
  aria-label={$t('compose.dialog.ariaLabel')}
  on:keydown|capture={onSendKey}
>
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <header class="head" on:dblclick={toggleMinimize}>
    <span class="title">{session.subject || $t('compose.title.untitled')}</span>
    <div class="win">
      <button type="button" class="win-btn" aria-label={$t('compose.window.minimize')} title={$t('compose.window.minimize')} on:click={toggleMinimize}>
        <IconMinus size={15} stroke={1.8} />
      </button>
      <button type="button" class="win-btn" aria-label={session.fullscreen ? $t('compose.window.restore') : $t('compose.window.fullscreen')} title={session.fullscreen ? $t('compose.window.restore') : $t('compose.window.fullscreen')} on:click={toggleFullscreen}>
        {#if session.fullscreen}
          <IconArrowsDiagonalMinimize2 size={15} stroke={1.8} />
        {:else}
          <IconArrowsDiagonal size={15} stroke={1.8} />
        {/if}
      </button>
      <button
        type="button"
        class="win-btn"
        aria-label={$t('compose.window.close')}
        title={$shortcutTitle($t('compose.window.close'), 'close-window')}
        on:click={requestClose}
      >
        <IconX size={16} stroke={1.8} />
      </button>
    </div>
  </header>

  {#if confirmClose}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="confirm-backdrop" on:click={() => (confirmClose = false)}>
      <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions a11y-no-noninteractive-element-interactions -->
      <div class="confirm-box" role="dialog" aria-modal="true" aria-label={$t('compose.confirmClose.ariaLabel')} tabindex="-1" on:click|stopPropagation>
        <p>{$t('compose.confirmClose.message')}</p>
        <div class="confirm-actions">
          <button type="button" class="confirm-discard" on:click={discardAndClose}>
            <IconTrash size={14} stroke={1.7} />
            {$t('action.discard')}
          </button>
          <button type="button" class="confirm-save" on:click={saveAndClose}>
            <IconDeviceFloppy size={14} stroke={1.7} />
            {$t('compose.confirmClose.saveToDrafts')}
          </button>
        </div>
      </div>
    </div>
  {/if}

  {#if !session.minimized}
    {#if accounts.length > 1}
      <div class="from">
        <label for={`from-${session.id}`}>{$t('compose.field.from')}</label>
        <Select
          id={`from-${session.id}`}
          value={String(session.accountId)}
          items={accounts.map((acc) => ({ value: String(acc.id), label: acc.email }))}
          on:change={(e) => updateCompose(session.id, { accountId: Number(e.detail) })}
        />
      </div>
    {/if}

    <AddressFields {session} />

    <div class="editor" style:--compose-font={composeFont}>
      {#if session.mode === 'wysiwyg'}
        <!-- the rich editor loads lazily; show a hint while the chunk arrives. -->
        {#if RichEditor}
          <svelte:component
            this={RichEditor}
            content={session.body}
            grabFocus={editorGrabFocus}
            on:change={(e) => updateCompose(session.id, { body: e.detail })}
          />
        {:else}
          <div class="editor-loading">{$t('compose.editor.loading')}</div>
        {/if}
      {:else if session.mode === 'markdown'}
        <EditorToolbar {preview} on:format={(e) => applyFormat(e.detail)} on:togglePreview={() => (preview = !preview)} />
        {#if preview}
          <div class="preview selectable">{@html previewHtml}</div>
        {:else if CMEditor}
          <svelte:component
            this={CMEditor}
            bind:this={editor}
            content={session.body}
            placeholder={$t('compose.editor.placeholderMarkdown')}
            vimEnabled={$prefs.composeVimMode}
            grabFocus={editorGrabFocus}
            on:change={(e) => updateCompose(session.id, { body: e.detail })}
          />
        {:else}
          <div class="editor-loading">{$t('compose.editor.loading')}</div>
        {/if}
      {:else if CMEditor}
        <svelte:component
          this={CMEditor}
          bind:this={editor}
          content={session.body}
          placeholder={$t('compose.editor.placeholderPlain')}
          vimEnabled={$prefs.composeVimMode}
          mono
          grabFocus={editorGrabFocus}
          on:change={(e) => updateCompose(session.id, { body: e.detail })}
        />
      {:else}
        <div class="editor-loading">Loading editor…</div>
      {/if}
    </div>

    <div class="attach-row">
      <AttachmentPicker {session} />
    </div>

    <footer class="foot">
      <div class="send-split">
        <button type="button" class="send" disabled={sending} title={$shortcutTitle($t('action.send'), 'send')} on:click={() => send()}>
          <IconSend size={15} stroke={1.7} />
          {sending ? $t('compose.action.sending') : $t('action.send')}
        </button>
        <button
          type="button"
          class="send-caret"
          bind:this={sendCaretEl}
          disabled={sending}
          aria-label={$t('compose.sendLater.ariaLabel')}
          aria-haspopup="true"
          aria-expanded={sendMenuOpen}
          on:click={toggleSendMenu}
        >
          <IconChevronDown size={14} stroke={1.8} />
        </button>
      </div>
      <ProtectionPicker
        protection={session.protection}
        status={protectionStatus}
        on:change={(e) => updateCompose(session.id, { protection: e.detail })}
      />
      <button type="button" class="save" on:click={save}>
        <IconDeviceFloppy size={15} stroke={1.6} />
        {$t('action.saveDraft')}
      </button>
      <span class="spacer"></span>
      {#if $signatures.length > 0}
        <Select
          class="sig-select"
          bind:value={sigChoice}
          ariaLabel={$t('compose.signature.ariaLabel')}
          items={[
            { value: '', label: $t('compose.signature.placeholder'), disabled: true },
            ...$signatures.map((sig) => ({ value: String(sig.id), label: sig.name })),
          ]}
          on:change={insertSignature}
        />
      {/if}
      <EditorModeSwitch mode={session.mode} on:change={setMode} />
    </footer>
  {/if}
</div>

<svelte:window on:keydown={sendMenuOpen ? onSendMenuKeydown : undefined} />

{#if sendMenuOpen}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="send-menu-scrim" on:click={closeSendMenu}></div>
  <div
    class="send-menu"
    role="menu"
    aria-label={$t('compose.sendLater.ariaLabel')}
    bind:this={sendMenuEl}
    style={`left:${sendMenuLeft}px; top:${sendMenuTop}px`}
  >
    {#each formattedSendPresets as p}
      <button type="button" class="send-preset" role="menuitem" on:click={() => sendScheduled(p.when)}>
        <span class="sp-icon"><svelte:component this={p.icon} size={16} stroke={1.6} /></span>
        <span class="sp-label">{p.label}</span>
        <span class="sp-sub">{p.sub}</span>
      </button>
    {/each}
    <div class="send-custom">
      <label for={`send-custom-${session.id}`}>{$t('compose.sendLater.customLabel')}</label>
      <DateTimePicker
        id={`send-custom-${session.id}`}
        mode="datetime"
        bind:value={customSendValue}
        confirmLabel={$t('compose.sendLater.schedule')}
        on:confirm={confirmCustomSend}
      />
    </div>
  </div>
{/if}

<style>
  .compose {
    position: relative;
    display: flex;
    flex-direction: column;
    width: 460px;
    height: 540px;
    max-height: calc(100vh - 40px);
    background: var(--surface-overlay);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  /* fullscreen breaks the pane out of the corner stack to fill the window. */
  .compose.fullscreen {
    position: fixed;
    inset: 24px;
    /* clears the macOS traffic lights, which the 24px inset alone runs into;
       zero on every other platform. */
    top: calc(24px + var(--titlebar-lights));
    width: auto;
    height: auto;
    max-height: none;
    z-index: 130;
  }

  /* minimized collapses to just the title bar. */
  .compose.minimized {
    height: auto;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-4);
    background: var(--surface-sunken);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .compose.minimized .head {
    border-bottom: none;
  }

  .title {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
  }

  .win {
    display: inline-flex;
    gap: 2px;
    flex-shrink: 0;
  }

  .win-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    border-radius: var(--radius-control);
  }

  .win-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .from,
  .attach-row {
    padding: var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .from {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .from label {
    width: 52px;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .from :global(.select) {
    flex: 1;
    padding: var(--space-1) var(--space-2);
  }

  :global(.compose .fields) {
    padding: 0 var(--space-4);
  }

  .editor {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .preview {
    flex: 1;
    width: 100%;
    min-height: 0;
    background: transparent;
    color: var(--text-primary);
    font-family: var(--compose-font, inherit);
    font-size: var(--fz-body);
    line-height: 1.55;
    overflow-y: auto;
    padding: var(--space-3) var(--space-4);
  }

  /* shown briefly while the rich editor chunk loads. */
  .editor-loading {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  /* a light github-style rendered preview. */
  .preview :global(h1),
  .preview :global(h2),
  .preview :global(h3) {
    margin: var(--space-3) 0 var(--space-2);
  }

  .preview :global(a) {
    color: var(--link);
  }

  .preview :global(pre),
  .preview :global(code) {
    font-family: var(--font-mono);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
  }

  .preview :global(pre) {
    padding: var(--space-3);
    overflow-x: auto;
  }

  .preview :global(blockquote) {
    margin: 0 0 var(--space-2);
    padding-inline-start: var(--space-3);
    border-inline-start: 2px solid var(--border-strong);
    color: var(--text-secondary);
  }

  .foot {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
  }

  .spacer {
    flex: 1;
  }

  /* the class reaches a child component, so it needs :global to land. */
  footer :global(.sig-select) {
    color: var(--text-secondary);
    font-size: var(--fz-label);
    padding: var(--space-1) var(--space-2);
  }

  .send,
  .save {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    font-size: var(--fz-label);
  }

  .send:hover:not(:disabled),
  .save:hover {
    background: var(--surface-hover);
  }

  .send:disabled {
    opacity: 0.6;
    cursor: default;
  }

  /* the send button and its caret read as one attached control. */
  .send-split {
    position: relative;
    display: inline-flex;
  }

  .send-split .send {
    border-radius: var(--radius-control) 0 0 var(--radius-control);
    border-inline-end: none;
  }

  .send-caret {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    border: var(--hairline) solid var(--border-default);
    border-radius: 0 var(--radius-control) var(--radius-control) 0;
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .send-caret:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .send-caret:disabled {
    opacity: 0.6;
    cursor: default;
  }

  /* the send-later menu is fixed-positioned and rendered outside .compose so
     the pane's own overflow:hidden never clips it. left/top are computed in
     toggleSendMenu from the menu's own measured size, clamped to the
     viewport, so it stays fully on-screen regardless of where the compose
     pane (and its caret) sits. */
  .send-menu-scrim {
    position: fixed;
    inset: 0;
    z-index: 140;
    /* the enclosing .compose-layer sets pointer-events: none and only restores
       it on .compose; these siblings must opt back in or clicks fall through to
       the compose controls behind them. */
    pointer-events: auto;
  }

  .send-menu {
    position: fixed;
    z-index: 141;
    pointer-events: auto;
    width: 260px;
    max-height: min(360px, calc(100vh - 2 * var(--space-5)));
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .send-preset {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    cursor: var(--cursor-action);
    text-align: start;
    font-size: var(--fz-label);
  }

  .send-preset:hover {
    background: var(--surface-hover);
  }

  .sp-icon {
    display: inline-flex;
    color: var(--accent);
    flex-shrink: 0;
  }

  .sp-label {
    flex: 1;
  }

  .sp-sub {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .send-custom {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin-top: var(--space-1);
    padding: var(--space-2) var(--space-3) var(--space-1);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .send-custom label {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .confirm-backdrop {
    position: absolute;
    inset: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--scrim, rgba(0, 0, 0, 0.4));
    backdrop-filter: blur(1px);
  }

  .confirm-box {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    width: min(320px, calc(100% - var(--space-5)));
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .confirm-box p {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .confirm-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }

  .confirm-discard,
  .confirm-save {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    font-size: var(--fz-label);
  }

  .confirm-discard:hover {
    background: var(--danger-bg, var(--surface-hover));
    color: var(--danger, var(--text-primary));
    border-color: var(--danger, var(--border-default));
  }

  .confirm-save:hover {
    background: var(--surface-hover);
  }
</style>
