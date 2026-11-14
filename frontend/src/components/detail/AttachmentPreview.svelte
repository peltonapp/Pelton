<script lang="ts">
  // the in-app attachment previewer. it fetches the attachment bytes and renders
  // pdf (with the bundled pdf.js), images, or text/code/markdown source. anything
  // else, or a file over the preview cap, offers "save instead". text is shown as
  // source (never rendered as html) so an untrusted attachment cannot run script.
  import { onDestroy } from 'svelte'
  import { fade, scale } from 'svelte/transition'
  import { IconX, IconDownload } from '@tabler/icons-svelte'
  import { previewTarget, closePreview } from '../../stores/preview'
  import { readAttachment, saveAttachment } from '../../lib/api'
  import { previewKind } from '../../lib/filetype'
  import { formatBytes } from '../../lib/format'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { AttachmentContent } from '../../lib/types'
  import PdfView from './PdfView.svelte'

  let loading = false
  let content: AttachmentContent | null = null
  let errorText = ''
  let lastKey = ''
  // an image renders from a Blob object url, revoked whenever it changes. a pdf
  // goes to PdfView as raw bytes.
  let objectUrl = ''
  let pdfBytes: Uint8Array | null = null

  // (re)load whenever the target changes.
  $: if ($previewTarget) {
    const key = `${$previewTarget.messageId}:${$previewTarget.attachment.id}`
    if (key !== lastKey) {
      lastKey = key
      void load()
    }
  } else {
    lastKey = ''
    content = null
    pdfBytes = null
    errorText = ''
    releaseUrl()
  }

  function releaseUrl(): void {
    if (objectUrl) {
      URL.revokeObjectURL(objectUrl)
      objectUrl = ''
    }
  }

  function base64ToBytes(b64: string): Uint8Array<ArrayBuffer> {
    const bin = atob(b64)
    const bytes = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) {
      bytes[i] = bin.charCodeAt(i)
    }
    return bytes
  }

  $: kind = $previewTarget
    ? previewKind($previewTarget.attachment.contentType, $previewTarget.attachment.filename)
    : 'none'

  async function load(): Promise<void> {
    const target = $previewTarget
    if (!target) {
      return
    }
    loading = true
    content = null
    pdfBytes = null
    errorText = ''
    releaseUrl()
    try {
      const c = await readAttachment(target.messageId, target.attachment.id)
      content = c
      const k = previewKind(target.attachment.contentType, target.attachment.filename)
      if (c.data && k === 'pdf') {
        pdfBytes = base64ToBytes(c.data)
      } else if (c.data && k === 'image') {
        objectUrl = URL.createObjectURL(new Blob([base64ToBytes(c.data)], { type: c.contentType || 'application/octet-stream' }))
      }
    } catch (err) {
      errorText = errorMessage(err)
    } finally {
      loading = false
    }
  }

  // textBody decodes the base64 payload to utf-8 text for the source view.
  $: textBody = kind === 'text' && content && content.data ? decodeText(content.data) : ''

  function decodeText(b64: string): string {
    try {
      const bin = atob(b64)
      const bytes = new Uint8Array(bin.length)
      for (let i = 0; i < bin.length; i++) {
        bytes[i] = bin.charCodeAt(i)
      }
      return new TextDecoder('utf-8').decode(bytes)
    } catch {
      return ''
    }
  }

  async function saveInstead(): Promise<void> {
    const target = $previewTarget
    if (!target) {
      return
    }
    try {
      const path = await saveAttachment(target.messageId, target.attachment.id)
      if (path) {
        toastSuccess($t('detail.attachments.saved').replace('{name}', target.attachment.filename))
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      closePreview()
    }
  }

  onDestroy(releaseUrl)
</script>

<svelte:window on:keydown={$previewTarget ? onKeydown : undefined} />

{#if $previewTarget}
  <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
  <div class="backdrop" transition:fade={{ duration: 120 }} on:click={closePreview}></div>
  <div class="viewer" role="dialog" aria-modal="true" aria-label={$t('detail.attachments.previewDialogLabel')} transition:scale={{ duration: 150, start: 0.96 }}>
    <header>
      <span class="name" title={$previewTarget.attachment.filename}>{$previewTarget.attachment.filename}</span>
      <span class="spacer"></span>
      <button type="button" class="hbtn" title={$t('detail.attachments.save')} on:click={saveInstead}>
        <IconDownload size={16} stroke={1.7} />
      </button>
      <button type="button" class="hbtn" aria-label={$t('detail.attachments.close')} on:click={closePreview}>
        <IconX size={16} stroke={1.8} />
      </button>
    </header>

    <div class="body">
      {#if loading}
        <div class="state">{$t('detail.attachments.loadingPreview')}</div>
      {:else if errorText}
        <div class="state">{errorText}</div>
      {:else if content && content.tooLarge}
        <div class="state">
          {$t('detail.attachments.tooLarge').replace('{size}', formatBytes(content.sizeBytes))}
          <button type="button" class="link" on:click={saveInstead}>{$t('detail.attachments.saveInstead')}</button>
        </div>
      {:else if kind === 'none'}
        <div class="state">
          {$t('detail.attachments.noPreview')}
          <button type="button" class="link" on:click={saveInstead}>{$t('detail.attachments.saveInstead')}</button>
        </div>
      {:else if kind === 'pdf' && pdfBytes}
        {#key pdfBytes}
          <PdfView data={pdfBytes} />
        {/key}
      {:else if kind === 'image' && objectUrl}
        <div class="image-wrap">
          <img src={objectUrl} alt={$previewTarget.attachment.filename} />
        </div>
      {:else if kind === 'text'}
        <pre class="text">{textBody}</pre>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 300;
    background: var(--scrim, rgba(0, 0, 0, 0.5));
    backdrop-filter: blur(2px);
  }

  .viewer {
    position: fixed;
    z-index: 301;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    width: min(920px, calc(100vw - 2 * var(--space-5)));
    height: min(80vh, calc(100vh - 2 * var(--space-5)));
    display: flex;
    flex-direction: column;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
    flex-shrink: 0;
  }

  .name {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .spacer {
    flex: 1;
  }

  .hbtn {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }
  .hbtn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: var(--surface-sunken);
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    height: 100%;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    text-align: center;
    padding: var(--space-5);
  }

  .link {
    border: none;
    background: transparent;
    color: var(--accent);
    cursor: var(--cursor-action);
    font: inherit;
    text-decoration: underline;
  }

  .image-wrap {
    display: flex;
    align-items: center;
    justify-content: center;
    min-height: 100%;
    padding: var(--space-4);
  }
  .image-wrap img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
  }

  .text {
    margin: 0;
    padding: var(--space-4);
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: var(--fz-label);
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-word;
  }
</style>
