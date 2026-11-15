<script lang="ts">
  // the attachments browser's preview column: the file in focus, drawn when it
  // is an image, a pdf or text, its details, and what can be done with it. A
  // file not stored here says so and is fetched from the server only when
  // asked. The bytes are read a moment after the focus settles, so running
  // the arrow keys down the list does not read every file on the way.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import cloudIcon from '@phosphor-icons/core/assets/duotone/cloud-arrow-down-duotone.svg?raw'
  import openIcon from '@phosphor-icons/core/assets/duotone/arrow-square-out-duotone.svg?raw'
  import saveIcon from '@phosphor-icons/core/assets/duotone/download-simple-duotone.svg?raw'
  import messageIcon from '@phosphor-icons/core/assets/duotone/envelope-open-duotone.svg?raw'
  import copyIcon from '@phosphor-icons/core/assets/duotone/copy-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Spinner from '../common/Spinner.svelte'
  import PdfView from '../detail/PdfView.svelte'
  import { kindIcon, kindLabelKey } from './kinds'
  import { fetchAttachment, openAttachmentFile, readAttachment, saveAttachment } from '../../lib/api'
  import { base64ToBytes } from '../../lib/thumbnails'
  import { previewKind } from '../../lib/filetype'
  import { formatBytes, formatFullDate, accountLabel, type TimeFormat } from '../../lib/format'
  import { revealAttachment } from '../../stores/attachments'
  import { sidebar } from '../../stores/accounts'
  import { online } from '../../stores/network'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { AttachmentEntry } from '../../lib/types'

  /** The file in focus. */
  export let entry: AttachmentEntry
  /** How many files are marked, for the save-all action. */
  export let selectedCount = 0

  const dispatch = createEventDispatcher<{ changed: AttachmentEntry; saveSelected: void }>()

  const settleMs = 160

  $: kind = previewKind(entry.contentType, entry.filename)
  $: account = ($sidebar.data?.accounts ?? []).find((a) => a.id === entry.accountId)

  let loadingPreview = false
  let previewError = ''
  let tooLarge = false
  let imageUrl = ''
  let pdfBytes: Uint8Array | null = null
  let text = ''
  let shownKey = ''
  let timer: ReturnType<typeof setTimeout> | undefined

  $: requestPreview(`${entry.id}:${entry.downloaded}`)

  function requestPreview(key: string): void {
    if (key === shownKey) {
      return
    }
    shownKey = key
    clearPreview()
    clearTimeout(timer)
    if (!entry.downloaded || kind === 'none') {
      return
    }
    loadingPreview = true
    timer = setTimeout(() => void load(key), settleMs)
  }

  function clearPreview(): void {
    if (imageUrl) {
      URL.revokeObjectURL(imageUrl)
    }
    imageUrl = ''
    pdfBytes = null
    text = ''
    tooLarge = false
    previewError = ''
    loadingPreview = false
  }

  async function load(key: string): Promise<void> {
    const target = entry
    try {
      const content = await readAttachment(target.messageId, target.id)
      if (key !== shownKey) {
        return
      }
      if (content.tooLarge || !content.data) {
        tooLarge = content.tooLarge
        return
      }
      const bytes = base64ToBytes(content.data)
      if (kind === 'image') {
        imageUrl = URL.createObjectURL(new Blob([bytes], { type: content.contentType || 'application/octet-stream' }))
      } else if (kind === 'pdf') {
        pdfBytes = bytes
      } else {
        text = new TextDecoder('utf-8').decode(bytes)
      }
    } catch (err) {
      if (key === shownKey) {
        previewError = errorMessage(err)
      }
    } finally {
      if (key === shownKey) {
        loadingPreview = false
      }
    }
  }

  let fetching = false
  let fetchError = ''

  async function download(): Promise<void> {
    if (fetching) {
      return
    }
    fetching = true
    fetchError = ''
    const id = entry.id
    try {
      const next = await fetchAttachment(id)
      dispatch('changed', next)
    } catch (err) {
      if (entry.id === id) {
        fetchError = errorMessage(err)
      }
    } finally {
      fetching = false
    }
  }

  $: if (entry.id) {
    fetchError = ''
  }

  async function open(): Promise<void> {
    try {
      await openAttachmentFile(entry.messageId, entry.id)
      if (!entry.downloaded) {
        dispatch('changed', { ...entry, downloaded: true })
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function save(): Promise<void> {
    try {
      const path = await saveAttachment(entry.messageId, entry.id)
      if (path) {
        toastSuccess($t('detail.attachments.saved').replace('{name}', entry.filename))
        if (!entry.downloaded) {
          dispatch('changed', { ...entry, downloaded: true })
        }
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  async function copyName(): Promise<void> {
    try {
      await navigator.clipboard.writeText(entry.filename)
      toastSuccess($t('attachments.nameCopied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  onDestroy(() => {
    clearTimeout(timer)
    clearPreview()
  })
</script>

<div class="inspector">
  <div class="preview" class:fill={!!pdfBytes}>
    {#if !entry.downloaded}
      <div class="state">
        <Ph svg={cloudIcon} size={32} />
        <strong>{$t('attachments.notDownloaded')}</strong>
        <p>{$online ? $t('attachments.notDownloadedHint') : $t('attachments.offlineHint')}</p>
        <button type="button" class="btn" disabled={fetching} on:click={download}>
          {#if fetching}
            <Spinner inline label={$t('attachments.fetching')} />
          {:else}
            {$t('attachments.fetch')}
          {/if}
        </button>
        {#if fetchError}
          <p class="error" role="alert">{fetchError}</p>
        {/if}
      </div>
    {:else if loadingPreview}
      <div class="state"><Spinner label={$t('detail.attachments.loadingPreview')} /></div>
    {:else if previewError}
      <div class="state"><p class="error">{previewError}</p></div>
    {:else if imageUrl}
      <img src={imageUrl} alt={entry.filename} />
    {:else if pdfBytes}
      {#key pdfBytes}
        <PdfView data={pdfBytes} />
      {/key}
    {:else if text}
      <pre class="text">{text}</pre>
    {:else}
      <div class="state">
        <Ph svg={kindIcon[entry.kind] ?? kindIcon.other} size={40} />
        <p>
          {tooLarge
            ? $t('detail.attachments.tooLarge').replace('{size}', formatBytes(entry.sizeBytes))
            : $t('attachments.noPreview')}
        </p>
      </div>
    {/if}
  </div>

  <div class="about">
    <h2 dir="auto" title={entry.filename}>{entry.filename}</h2>
    <dl>
      <dt>{$t('attachments.detail.type')}</dt>
      <dd>{$t(kindLabelKey[entry.kind] ?? kindLabelKey.other)}, {formatBytes(entry.sizeBytes)}</dd>
      <dt>{$t('attachments.detail.from')}</dt>
      <dd dir="auto" title={entry.fromAddress}>{entry.fromName || entry.fromAddress}</dd>
      <dt>{$t('attachments.detail.date')}</dt>
      <dd>{formatFullDate(entry.date, $prefs.timeFormat as TimeFormat)}</dd>
      <dt>{$t('attachments.detail.subject')}</dt>
      <dd dir="auto">{entry.subject || $t('detail.noSubject')}</dd>
      {#if account}
        <dt>{$t('attachments.detail.mailbox')}</dt>
        <dd>{accountLabel(account)}</dd>
      {/if}
    </dl>

    <div class="actions">
      <button type="button" class="btn primary" on:click={open}>
        <Ph svg={openIcon} size={14} />
        {$t('attachments.action.open')}
      </button>
      <button type="button" class="btn" on:click={save}>
        <Ph svg={saveIcon} size={14} />
        {$t('attachments.action.save')}
      </button>
      {#if selectedCount > 1}
        <button type="button" class="btn" on:click={() => dispatch('saveSelected')}>
          <Ph svg={saveIcon} size={14} />
          {$t('attachments.saveSelectedCount').replace('{count}', String(selectedCount))}
        </button>
      {/if}
      <button type="button" class="btn" on:click={() => revealAttachment(entry)}>
        <Ph svg={messageIcon} size={14} />
        {$t('attachments.action.showMessage')}
      </button>
      <button type="button" class="btn" on:click={copyName}>
        <Ph svg={copyIcon} size={14} />
        {$t('attachments.action.copyName')}
      </button>
    </div>
  </div>
</div>

<style>
  .inspector {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto;
    height: 100%;
    min-height: 0;
  }

  .preview {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 0;
    overflow: auto;
    padding: var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
  }

  .preview.fill {
    align-items: stretch;
    padding: 0;
  }

  .preview.fill > :global(*) {
    flex: 1;
    min-width: 0;
  }

  img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }

  .text {
    align-self: stretch;
    flex: 1;
    margin: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-label);
    color: var(--text-primary);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    max-width: 100%;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    text-align: center;
  }

  .state strong {
    color: var(--text-primary);
    font-weight: var(--fw-semibold);
  }

  .state p {
    margin: 0;
  }

  .error {
    color: var(--danger);
  }

  .about {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4) var(--pane-pad);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  dl {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: var(--space-1) var(--space-4);
    margin: 0;
    font-size: var(--fz-label);
  }

  dt {
    color: var(--text-tertiary);
  }

  dd {
    margin: 0;
    color: var(--text-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: var(--control-height);
    padding-inline: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .btn:disabled {
    cursor: default;
    opacity: 0.6;
  }

  .btn.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover {
    background: var(--accent);
    color: var(--accent-fg);
    filter: brightness(1.05);
  }
</style>
