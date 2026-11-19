<script lang="ts">
  // the composer of the new design. one session, laid out three ways: a
  // window of its own and a tab both put the actions in a toolbar on top, the
  // docked panel condenses them into its footer. under the actions come the
  // reply line, the header fields, the attachment strip, the formatting bar
  // and the editor, with a slim status line at the bottom.
  //
  // there is one editor and no mode switch: rich text, with Markdown typed or
  // pasted converted when the composeMarkdown pref is on. vim mode needs
  // CodeMirror, so with it on the body is plain text in that editor instead.
  // send, save and the close prompt share their logic with the classic pane
  // through lib/composeactions.ts.
  import { onMount, tick } from 'svelte'
  import { get } from 'svelte/store'
  import paperclipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import floppyIcon from '@phosphor-icons/core/assets/duotone/floppy-disk-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import windowIcon from '@phosphor-icons/core/assets/duotone/app-window-duotone.svg?raw'
  import minimizeIcon from '@phosphor-icons/core/assets/duotone/minus-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import AnchoredPopover from '../common/AnchoredPopover.svelte'
  import SendButton from './v2/SendButton.svelte'
  import ProtectionMenu from './v2/ProtectionMenu.svelte'
  import HeaderFields from './v2/HeaderFields.svelte'
  import AttachmentStrip from './v2/AttachmentStrip.svelte'
  import FormatBar from './v2/FormatBar.svelte'
  import ReplyContext from './v2/ReplyContext.svelte'
  import {
    closeCompose,
    closeRequest,
    getSession,
    switchReplyKind,
    typedPart,
    updateCompose,
    type ComposeSession,
    type ReplyKind,
  } from '../../stores/compose'
  import { draftState, hasContent, ProtectionWatch, recipientsOf, saveSession, sendSession, settled } from '../../lib/composeactions'
  import { openComposeWindow, showMessageInMainWindow, titleBarDoubleClick } from '../../lib/api'
  import { isDemoActive } from '../../lib/demo'
  import { convertEditorBody, htmlToText } from '../../lib/richtext'
  import { withSignatureBlock } from '../../lib/signatureblock'
  import { bodyFontStack } from '../../lib/fonts'
  import { comboMatches } from '../../lib/shortcuts'
  import { formatStatusClock, type TimeFormat } from '../../lib/format'
  import { tip } from '../../lib/tip'
  import { t } from '../../lib/i18n'
  import { prefs } from '../../stores/prefs'
  import { bindings, shortcutTitle } from '../../stores/shortcuts'
  import { signatures, signatureById, getAccountSignatures } from '../../stores/signatures'
  import { sidebar } from '../../stores/accounts'
  import { revealMessage } from '../../stores/selection'
  import { focusPane } from '../../stores/tabs'
  import { errorMessage, toastError } from '../../stores/toast'
  import type { ProtectionStatus, Signature } from '../../lib/types'
  import type CodeMirrorEditor from './CodeMirrorEditor.svelte'
  import type RichEditorView from './RichEditor.svelte'

  /** The session this composer edits. */
  export let session: ComposeSession
  /** How the composer is laid out: a window of its own, docked, or a tab. */
  export let layout: 'window' | 'docked' | 'tab' = 'docked'
  /** Set in the main window, where the original of a reply can be shown in place. */
  export let mainWindow = false

  type SendState = 'idle' | 'sending' | 'sent'

  let sendState: SendState = 'idle'
  let confirmClose = false
  let saving = false
  let savedAt: Date | null = session.saved ? new Date() : null
  // savedAs is the draft as last saved, so closing it unchanged asks nothing.
  let savedAs = session.saved ? draftState(session) : ''
  let protectionStatus: ProtectionStatus | null = null
  const protectionWatch = new ProtectionWatch(session.id, session.saved)

  let strip: AttachmentStrip
  let moreButton: HTMLButtonElement
  let moreOpen = false
  let sigChoice = ''

  $: rich = session.mode === 'wysiwyg'
  $: composeFont = bodyFontStack($prefs.bodyFont)
  $: canPopOut = layout !== 'window' && !isDemoActive()
  $: docked = layout === 'docked'

  // the editors are code-split and load the first time they are needed.
  let RichEditor: typeof RichEditorView | null = null
  let richEditor: RichEditorView | null = null
  let CMEditor: typeof CodeMirrorEditor | null = null
  let revision = 0
  $: if (rich && !RichEditor) {
    void import('./RichEditor.svelte').then((m) => (RichEditor = m.default))
  }
  $: if (!rich && !CMEditor) {
    void import('./CodeMirrorEditor.svelte').then((m) => (CMEditor = m.default))
  }

  // the quoted or forwarded original sits folded below what is being written,
  // behind a small toggle, until it is opened for editing.
  let folded = Boolean(session.fold) && typedPart(session) !== session.body
  $: typed = folded ? typedPart(session) : session.body

  // a reply opens with the caret in the body above the quote; anything else
  // starts in the first empty field.
  const startInBody = session.to.trim() !== '' && session.subject.trim() !== ''
  const richCaret: string | null = startInBody ? '' : null
  const textCaret: number | null = startInBody ? 0 : null

  $: words = countWords(rich ? htmlToText(typed) : typed)

  function countWords(text: string): number {
    return text.match(/\S+/g)?.length ?? 0
  }

  function setBody(next: string): void {
    const current = getSession(session.id) ?? session
    const tail = folded ? current.body.slice(typedPart(current).length) : ''
    updateCompose(session.id, { body: next + tail })
  }

  async function unfold(): Promise<void> {
    folded = false
    await tick()
    richEditor?.focus()
  }

  // withBlock puts a signature into the body: above the original when the
  // user asked for that or the original is folded away, below it otherwise.
  function withBlock(body: string, sig: Signature): string {
    const above = $prefs.signatureAboveQuote || folded
    return withSignatureBlock(body, sig, session.mode, above ? (session.fold ?? '') : '')
  }

  function insertSignature(event: CustomEvent<string>): void {
    const sig = signatureById(Number(event.detail))
    sigChoice = ''
    if (sig) {
      updateCompose(session.id, { body: withBlock(session.body, sig) })
    }
  }

  onMount(async () => {
    // one editor: rich text, or plain text in CodeMirror for vim mode. A draft
    // saved in another mode is converted once on open.
    const wanted = $prefs.composeVimMode ? 'plaintext' : 'wysiwyg'
    if (session.mode !== wanted) {
      folded = false
      updateCompose(session.id, { mode: wanted, body: convertEditorBody(session.body, session.mode, wanted), fold: undefined, quote: undefined })
    }

    if (session.to.trim() === '') {
      document.querySelector<HTMLInputElement>(`#to-${session.id}`)?.focus()
    } else if (session.subject.trim() === '') {
      document.querySelector<HTMLInputElement>(`#subject-${session.id}`)?.focus()
    }

    if (session.signaturesApplied) {
      return
    }
    try {
      const assigned = await getAccountSignatures(session.accountId)
      let body = getSession(session.id)?.body ?? session.body
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

  $: recipientList = recipientsOf(session)
  $: void refreshProtection(session.accountId, recipientList)

  async function refreshProtection(accountId: number, recipients: string[]): Promise<void> {
    if (await protectionWatch.refresh(accountId, recipients)) {
      protectionStatus = protectionWatch.status
    }
  }

  function wait(ms: number): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, ms))
  }

  // send morphs the button: a pie loader while the message is queued, then a
  // check that is left to draw before the composer closes.
  async function send(scheduledAt?: Date): Promise<void> {
    if (sendState !== 'idle') {
      return
    }
    sendState = 'sending'
    const sent = await sendSession(session.id, session, {
      scheduledAt,
      beforeClose: async () => {
        sendState = 'sent'
        await wait(get(prefs).reduceMotion ? 0 : 560)
      },
    })
    if (!sent) {
      sendState = 'idle'
    }
  }

  // onSendKey sends on the send shortcut from anywhere in the composer. It
  // listens on the way down, because both editors bind the same key.
  async function onSendKey(event: KeyboardEvent): Promise<void> {
    const combo = $bindings.send
    if (!combo || !comboMatches(event, combo) || sendState !== 'idle' || confirmClose || session.minimized) {
      return
    }
    event.preventDefault()
    if (!(event.target instanceof HTMLInputElement)) {
      event.stopPropagation()
    }
    await tick()
    await send()
  }

  async function save(after?: () => void): Promise<void> {
    saving = true
    try {
      await saveSession(session.id, session, (current) => {
        savedAs = draftState(current)
        savedAt = new Date()
        after?.()
      })
    } finally {
      saving = false
    }
  }

  $: dirty = draftState(session) !== savedAs
  $: statusText = saving
    ? $t('compose2.status.saving')
    : savedAt && !dirty
      ? $t('compose2.status.saved').replace('{time}', formatStatusClock(savedAt, $prefs.timeFormat as TimeFormat))
      : hasContent(session)
        ? $t('compose2.status.unsaved')
        : ''

  // an outside close (the close-window action, a tab's close button) lands
  // here rather than removing the session, so an unsaved draft still asks.
  $: if ($closeRequest === session.id) {
    closeRequest.set(null)
    requestClose()
  }

  function requestClose(): void {
    if (hasContent(session) && draftState(session) !== savedAs) {
      confirmClose = true
      return
    }
    closeCompose(session.id)
  }

  function saveAndClose(): Promise<void> {
    confirmClose = false
    return save(() => closeCompose(session.id))
  }

  // discardAndClose drops the edits since the last save; a save still running
  // finishes first, so its failed upload does not bring the composer back.
  async function discardAndClose(): Promise<void> {
    confirmClose = false
    moreOpen = false
    await settled(session.id, session)
    closeCompose(session.id)
  }

  async function popOut(): Promise<void> {
    moreOpen = false
    try {
      await openComposeWindow(getSession(session.id) ?? session)
      closeCompose(session.id)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function switchKind(event: CustomEvent<ReplyKind>): void {
    switchReplyKind(session.id, event.detail)
    folded = Boolean(getSession(session.id)?.fold)
  }

  // showOriginal opens the message being answered: in place in the main
  // window, or by bringing the main window forward from anywhere else.
  function showOriginal(): void {
    const origin = session.origin
    if (!origin) {
      return
    }
    if (mainWindow) {
      if (layout === 'tab') {
        focusPane()
      }
      revealMessage(origin.id, origin.accountId, origin.folderId, get(sidebar).data ?? null)
      return
    }
    void showMessageInMainWindow(origin.id).catch((err) => toastError(errorMessage(err)))
  }

  function onDrop(event: DragEvent): void {
    const files = event.dataTransfer?.files
    if (files && files.length > 0) {
      void strip.add(files)
    }
  }

  function onConfirmKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      confirmClose = false
    }
  }
</script>

<!-- the key and drop listeners only watch what the fields inside send up; the
     composer itself is not something to focus. -->
<!-- svelte-ignore a11y-no-noninteractive-element-interactions a11y-interactive-supports-focus -->
<div
  class="composer {layout}"
  class:minimized={docked && session.minimized}
  data-compose-id={session.id}
  role="dialog"
  aria-label={$t('compose.dialog.ariaLabel')}
  on:keydown|capture={onSendKey}
  on:dragover|preventDefault
  on:drop|preventDefault={onDrop}
>
  {#if docked}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <header class="dock-head" on:dblclick|self={() => updateCompose(session.id, { minimized: !session.minimized })}>
      <span class="title">{session.subject || $t('compose.title.untitled')}</span>
      <button
        type="button"
        class="c2-tool compact"
        aria-label={session.minimized ? $t('compose.window.restore') : $t('compose.window.minimize')}
        use:tip={{ text: session.minimized ? $t('compose.window.restore') : $t('compose.window.minimize'), side: 'top' }}
        on:click={() => updateCompose(session.id, { minimized: !session.minimized })}
      >
        <Ph svg={minimizeIcon} size={14} />
      </button>
      {#if canPopOut}
        <button type="button" class="c2-tool compact" aria-label={$t('messageList.menu.openInWindow')} use:tip={{ text: $t('messageList.menu.openInWindow'), side: 'top' }} on:click={popOut}>
          <Ph svg={windowIcon} size={14} />
        </button>
      {/if}
      <button type="button" class="c2-tool compact" aria-label={$t('compose.window.close')} use:tip={{ text: $shortcutTitle($t('compose.window.close'), 'close-window'), side: 'top' }} on:click={requestClose}>
        <Ph svg={xIcon} size={14} />
      </button>
    </header>
  {:else}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="toolbar"
      style={layout === 'window' ? '--wails-draggable:drag' : undefined}
      on:dblclick|self={() => layout === 'window' && titleBarDoubleClick()}
    >
      <SendButton id={`compose-${session.id}`} state={sendState} on:send={() => send()} on:schedule={(e) => send(e.detail)} />
      <span class="gap" aria-hidden="true"></span>
      <button type="button" class="c2-tool" on:click={() => strip.pick()}>
        <Ph svg={paperclipIcon} size={16} />
        <span>{$t('compose.attach.button')}</span>
      </button>
      <ProtectionMenu protection={session.protection} status={protectionStatus} on:change={(e) => updateCompose(session.id, { protection: e.detail })} />
      <button type="button" class="c2-tool" disabled={saving} on:click={() => save()}>
        <Ph svg={floppyIcon} size={16} />
        <span>{$t('action.saveDraft')}</span>
      </button>
      <span class="grow" aria-hidden="true"></span>
      {#if canPopOut}
        <button type="button" class="c2-tool compact" aria-label={$t('messageList.menu.openInWindow')} use:tip={{ text: $t('messageList.menu.openInWindow') }} on:click={popOut}>
          <Ph svg={windowIcon} size={16} />
        </button>
      {/if}
      <button
        type="button"
        class="c2-tool compact"
        bind:this={moreButton}
        aria-label={$t('compose2.more')}
        aria-haspopup="menu"
        aria-expanded={moreOpen}
        use:tip={{ text: $t('compose2.more') }}
        on:click={() => (moreOpen = !moreOpen)}
      >
        <Ph svg={moreIcon} size={16} />
      </button>
      <button type="button" class="c2-tool compact" aria-label={$t('compose.window.close')} use:tip={{ text: $shortcutTitle($t('compose.window.close'), 'close-window') }} on:click={requestClose}>
        <Ph svg={xIcon} size={16} />
      </button>
    </div>
  {/if}

  {#if !(docked && session.minimized)}
    {#if session.origin}
      <ReplyContext origin={session.origin} on:kind={switchKind} on:show={showOriginal} />
    {/if}

    <HeaderFields {session} />
    <AttachmentStrip bind:this={strip} {session} />

    {#if rich}
      <FormatBar editor={richEditor} {revision} />
    {/if}

    <div class="editor" class:folded style:--compose-font={composeFont}>
      {#if rich}
        {#if RichEditor}
          {#key $prefs.composeMarkdown}
            <svelte:component
              this={RichEditor}
              bind:this={richEditor}
              bare
              inputRules={$prefs.composeMarkdown}
              pasteMarkdown={$prefs.composeMarkdown}
              placeholder={$t('compose.editor.placeholderPlain')}
              content={typed}
              caretContent={richCaret}
              on:change={(e) => setBody(e.detail)}
              on:transaction={(e) => (revision = e.detail)}
            />
          {/key}
        {:else}
          <div class="loading">{$t('compose.editor.loading')}</div>
        {/if}
      {:else if CMEditor}
        <svelte:component
          this={CMEditor}
          content={typed}
          placeholder={$t('compose.editor.placeholderPlain')}
          vimEnabled={$prefs.composeVimMode}
          mono
          caret={textCaret}
          on:change={(e) => setBody(e.detail)}
        />
      {:else}
        <div class="loading">{$t('compose.editor.loading')}</div>
      {/if}
      {#if folded}
        <button type="button" class="fold" aria-expanded="false" aria-label={$t('compose2.quote.show')} use:tip={{ text: $t('compose2.quote.show') }} on:click={unfold}>
          <Ph svg={moreIcon} size={16} />
        </button>
        <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
        <div class="filler" on:click={() => richEditor?.focus()}></div>
      {/if}
    </div>

    <footer class="status" class:dock-foot={docked}>
      {#if docked}
        <SendButton id={`compose-${session.id}`} state={sendState} menuAbove on:send={() => send()} on:schedule={(e) => send(e.detail)} />
        <button type="button" class="c2-tool compact" aria-label={$t('compose.attach.button')} use:tip={{ text: $t('compose.attach.button'), side: 'top' }} on:click={() => strip.pick()}>
          <Ph svg={paperclipIcon} size={16} />
        </button>
        <ProtectionMenu compact menuAbove protection={session.protection} status={protectionStatus} on:change={(e) => updateCompose(session.id, { protection: e.detail })} />
        <button type="button" class="c2-tool compact" disabled={saving} aria-label={$t('action.saveDraft')} use:tip={{ text: $t('action.saveDraft'), side: 'top' }} on:click={() => save()}>
          <Ph svg={floppyIcon} size={16} />
        </button>
      {/if}
      <span class="state" aria-live="polite">{statusText}</span>
      <span class="grow" aria-hidden="true"></span>
      {#if $signatures.length > 0}
        <Select
          class="c2-sig"
          bind:value={sigChoice}
          ariaLabel={$t('compose.signature.ariaLabel')}
          items={[
            { value: '', label: $t('compose.signature.placeholder'), disabled: true },
            ...$signatures.map((sig) => ({ value: String(sig.id), label: sig.name })),
          ]}
          on:change={insertSignature}
        />
      {/if}
      <span class="words">{words === 1 ? $t('compose2.status.word') : $t('compose2.status.words').replace('{count}', String(words))}</span>
      {#if docked}
        <button
          type="button"
          class="c2-tool compact"
          bind:this={moreButton}
          aria-label={$t('compose2.more')}
          aria-haspopup="menu"
          aria-expanded={moreOpen}
          use:tip={{ text: $t('compose2.more'), side: 'top' }}
          on:click={() => (moreOpen = !moreOpen)}
        >
          <Ph svg={moreIcon} size={16} />
        </button>
      {/if}
    </footer>
  {/if}

  {#if confirmClose}
    <div class="confirm-backdrop">
      <div class="confirm" role="alertdialog" aria-modal="true" aria-label={$t('compose.confirmClose.ariaLabel')} tabindex="-1" on:keydown={onConfirmKeydown}>
        <p>{$t('compose.confirmClose.message')}</p>
        <div class="confirm-actions">
          <button type="button" class="c2-tool" on:click={() => (confirmClose = false)}>{$t('compose2.confirm.keepEditing')}</button>
          <span class="grow" aria-hidden="true"></span>
          <button type="button" class="c2-tool danger" on:click={discardAndClose}>
            <Ph svg={trashIcon} size={14} />
            <span>{$t('action.discard')}</span>
          </button>
          <button type="button" class="primary" on:click={saveAndClose}>
            <Ph svg={floppyIcon} size={14} />
            <span>{$t('compose.confirmClose.saveToDrafts')}</span>
          </button>
        </div>
      </div>
    </div>
  {/if}
</div>

{#if moreOpen && moreButton}
  <AnchoredPopover anchor={moreButton} side={docked ? 'above' : 'below'} align="end" role="menu" label={$t('compose2.more')} on:close={() => (moreOpen = false)}>
    <div class="menu">
      {#if canPopOut}
        <button type="button" class="item" role="menuitem" on:click={popOut}>
          <Ph svg={windowIcon} size={16} />
          <span>{$t('messageList.menu.openInWindow')}</span>
        </button>
      {/if}
      <button type="button" class="item danger" role="menuitem" on:click={discardAndClose}>
        <Ph svg={trashIcon} size={16} />
        <span>{$t('compose2.discard')}</span>
      </button>
    </div>
  </AnchoredPopover>
{/if}

<style>
  .composer {
    position: relative;
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }

  .composer.window {
    position: fixed;
    inset: 0;
  }

  .composer.tab {
    height: 100%;
  }

  .composer.docked {
    width: var(--compose-dock-width);
    max-width: 100%;
    height: var(--compose-dock-height);
    max-height: 100%;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    box-shadow: var(--shadow-overlay);
    pointer-events: auto;
    animation: dock-in var(--motion-slow) var(--ease-out);
  }

  .composer.docked.minimized {
    height: auto;
  }

  @keyframes dock-in {
    from {
      opacity: 0;
      transform: translateY(var(--space-5));
    }
  }

  /* the toolbar of a window or tab. in a window it is the title bar too: it
     drags the window and keeps clear of the macOS traffic lights. */
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-sunken);
    user-select: none;
  }

  /* in a window the bar is exactly as tall as the title bar the window buttons
     are centred in, so its own buttons share their centre line. */
  .composer.window .toolbar {
    padding-block: 0;
    padding-inline-start: max(var(--pane-pad), var(--titlebar-inset));
    min-height: max(calc(var(--control-height) + 2 * var(--space-2)), var(--titlebar-lights));
  }

  .gap {
    width: var(--space-3);
    flex-shrink: 0;
  }

  .grow {
    flex: 1;
  }

  .dock-head {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2) var(--space-1) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-sunken);
    user-select: none;
  }

  .composer.minimized .dock-head {
    border-bottom: none;
  }

  .dock-head .c2-tool {
    height: calc(var(--control-height) - var(--space-2));
    min-width: calc(var(--control-height) - var(--space-2));
  }

  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  /* the editor area scrolls as one: the text, the fold toggle under it, and
     the empty room below, which still puts the caret in the text. */
  .editor {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
  }

  .editor :global(.rich) {
    flex: 1 0 auto;
    min-height: 0;
  }

  .editor.folded :global(.rich) {
    flex: 0 0 auto;
  }

  .editor :global(.rich > .surface) {
    flex: 1 0 auto;
    display: flex;
    flex-direction: column;
    overflow: visible;
  }

  .composer .editor :global(.ProseMirror) {
    flex: 1 0 auto;
    padding: var(--pane-pad);
    padding-bottom: var(--space-3);
  }

  .fold {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    margin: 0 var(--pane-pad) var(--space-3);
    padding: 0 var(--space-2);
    height: calc(var(--fz-body) + var(--space-2));
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .fold:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .fold:active {
    transform: scale(var(--press-scale));
  }

  .filler {
    flex: 1 0 auto;
    cursor: text;
  }

  .loading {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }

  /* the status line: draft state, signature picker and word count. docked, it
     also carries the actions the toolbar holds elsewhere. */
  .status {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--control-height) - var(--space-2));
    padding: var(--space-1) var(--pane-pad);
    border-top: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .status.dock-foot {
    gap: var(--space-1);
    padding: var(--space-3) var(--pane-pad);
  }

  .dock-foot .state {
    margin-inline-start: var(--space-3);
  }

  .state,
  .words {
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .status :global(.c2-sig) {
    border-color: transparent;
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    padding: var(--space-1) var(--space-2);
  }

  .status :global(.c2-sig:hover) {
    border-color: var(--border-subtle);
  }

  .confirm-backdrop {
    position: absolute;
    inset: 0;
    z-index: 10;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-5);
    background: var(--scrim);
    animation: fade var(--motion-base) var(--ease-out);
  }

  @keyframes fade {
    from {
      opacity: 0;
    }
  }

  .confirm {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    width: 100%;
    max-width: calc(var(--popover-width) + var(--space-6) * 2);
    padding: var(--space-5);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    animation: rise var(--motion-base) var(--ease-out);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(var(--space-3)) scale(0.98);
    }
  }

  .confirm p {
    margin: 0;
    font-size: var(--fz-body);
  }

  .confirm-actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .danger:hover:not(:disabled) {
    background: var(--danger-bg);
    color: var(--danger);
  }

  .primary {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding: 0 var(--space-4);
    border: none;
    border-radius: var(--radius-control);
    background: var(--accent);
    color: var(--accent-fg);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    cursor: var(--cursor-action);
    transition:
      filter var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .primary:hover {
    filter: brightness(1.08);
  }

  .primary:active {
    transform: scale(var(--press-scale));
  }

  .menu {
    display: flex;
    flex-direction: column;
    min-width: calc(var(--popover-width) / 1.6);
    padding: var(--space-2);
  }

  .item {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .item :global(.ph) {
    color: var(--text-secondary);
  }

  .item:hover,
  .item:focus-visible {
    background: var(--surface-hover);
    outline: none;
  }

  .item.danger:hover {
    color: var(--danger);
  }
</style>
