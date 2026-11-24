<script lang="ts">
  // the downloadable attachment list for the open message, shown as outlook-style
  // cards at the bottom of the reading pane. inline parts (cid images already
  // shown in the body, and only those actually referenced) are hidden. each card
  // shows a file-type icon, the name and size, and downloads via a native save
  // dialog on click.
  import { IconPaperclip, IconDownload, IconEye, IconDownloadOff, IconShieldSearch } from '@tabler/icons-svelte'
  import { onDestroy } from 'svelte'
  import { saveAttachment, saveAllAttachments, scanAttachment } from '../../lib/api'
  import { onAttachmentProgress } from '../../lib/events'
  import { formatBytes } from '../../lib/format'
  import { fileIcon, previewKind } from '../../lib/filetype'
  import { openPreview } from '../../stores/preview'
  import { openContextMenu } from '../../stores/contextmenu'
  import { virusTotal, attachmentVerdicts, putAttachmentVerdict, scanEnabled } from '../../stores/virustotal'
  import VerdictBadge from '../common/VerdictBadge.svelte'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Attachment } from '../../lib/types'

  export let messageId: number
  export let attachments: Attachment[]

  let busy = new Set<number>()
  let savingAll = false
  let scanningIds = new Set<number>()

  // the attachment being written to disk and how far it is, from 0 to 1. the
  // backend writes one file at a time, and its final event ends the run.
  let saving = new Map<number, number>()
  const stopProgress = onAttachmentProgress((e) => {
    if (!e.running) {
      saving = new Map()
    } else if (e.attachmentId && e.bytesTotal > 0) {
      saving = new Map([[e.attachmentId, Math.min(1, e.bytesDone / e.bytesTotal)]])
    }
  })
  onDestroy(stopProgress)

  $: canScan = scanEnabled($virusTotal)

  $: visible = attachments.filter((a) => !a.inline)
  $: attachmentsLabel = `${visible.length} ${visible.length === 1 ? $t('detail.attachments.one') : $t('detail.attachments.many')}`

  // whether the card click should preview (previewable types) or download.
  function canPreview(att: Attachment): boolean {
    return previewKind(att.contentType, att.filename) !== 'none'
  }

  function activate(att: Attachment): void {
    if (canPreview(att)) {
      openPreview(messageId, att)
    } else {
      void download(att)
    }
  }

  async function download(att: Attachment): Promise<void> {
    if (busy.has(att.id)) {
      return
    }
    busy = new Set(busy).add(att.id)
    try {
      const path = await saveAttachment(messageId, att.id)
      if (path) {
        toastSuccess($t('detail.attachments.saved').replace('{name}', att.filename))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      const next = new Set(busy)
      next.delete(att.id)
      busy = next
    }
  }

  // scan looks the attachment up by the hash of its bytes. the file is never
  // uploaded, so a file VirusTotal has not seen comes back unknown.
  async function scan(att: Attachment): Promise<void> {
    if (scanningIds.has(att.id)) {
      return
    }
    scanningIds = new Set(scanningIds).add(att.id)
    try {
      putAttachmentVerdict(messageId, att.id, await scanAttachment(messageId, att.id))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      const next = new Set(scanningIds)
      next.delete(att.id)
      scanningIds = next
    }
  }

  function onContext(event: MouseEvent, att: Attachment): void {
    if (!canScan) {
      return
    }
    event.preventDefault()
    openContextMenu(event.clientX, event.clientY, [
      { label: $t('virustotal.scanAttachment'), icon: IconShieldSearch, action: () => void scan(att) },
    ])
  }

  async function saveAll(): Promise<void> {
    if (savingAll) {
      return
    }
    savingAll = true
    try {
      const dir = await saveAllAttachments(messageId)
      if (dir) {
        toastSuccess($t('detail.attachments.savedAll').replace('{count}', String(visible.length)))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      savingAll = false
    }
  }
</script>

{#if visible.length > 0}
  <div class="attachments">
    <div class="head">
      <IconPaperclip size={13} stroke={1.6} />
      <span>{attachmentsLabel}</span>
      {#if visible.length > 1}
        <button type="button" class="save-all" class:busy={savingAll} on:click={saveAll}>
          <IconDownload size={13} stroke={1.7} />
          {$t('detail.attachments.saveAll')}
        </button>
      {/if}
    </div>
    <div class="cards">
      {#each visible as att (att.id)}
        <div class="card" class:busy={busy.has(att.id) || scanningIds.has(att.id)} on:contextmenu={(e) => onContext(e, att)} role="presentation">
          <button
            type="button"
            class="card-main"
            on:click={() => activate(att)}
            title={canPreview(att) ? $t('detail.attachments.preview').replace('{name}', att.filename) : $t('detail.attachments.download').replace('{name}', att.filename)}
          >
            <span class="glyph">
              <svelte:component this={fileIcon(att.contentType, att.filename)} size={22} stroke={1.5} />
            </span>
            <span class="meta">
              <span class="name">{att.filename}</span>
              <span class="size">
                {formatBytes(att.sizeBytes)}
                {#if $attachmentVerdicts.has(att.id)}
                  <VerdictBadge verdict={$attachmentVerdicts.get(att.id)!} size={12} />
                {/if}
              </span>
            </span>
            {#if canPreview(att)}
              <span class="dl" aria-hidden="true"><IconEye size={15} stroke={1.7} /></span>
            {/if}
          </button>
          <button type="button" class="dl-btn" title={$t('detail.attachments.download').replace('{name}', att.filename)} aria-label={$t('detail.attachments.download').replace('{name}', att.filename)} on:click={() => download(att)}>
            {#if busy.has(att.id)}
              <IconDownloadOff size={15} stroke={1.7} />
            {:else}
              <IconDownload size={15} stroke={1.7} />
            {/if}
          </button>
          {#if saving.has(att.id)}
            <span class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round((saving.get(att.id) ?? 0) * 100)}>
              <span class="fill" style={`width:${(saving.get(att.id) ?? 0) * 100}%`}></span>
            </span>
          {/if}
        </div>
      {/each}
    </div>
  </div>
{/if}

<style>
  .attachments {
    margin-top: var(--space-4);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    margin-bottom: var(--space-3);
  }

  .cards {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .save-all {
    margin-inline-start: auto;
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 2px var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    font-size: var(--fz-meta);
  }
  .save-all:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
  .save-all.busy {
    opacity: 0.6;
    cursor: default;
  }

  .card {
    position: relative;
    display: flex;
    align-items: stretch;
    width: 248px;
    max-width: 100%;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    overflow: hidden;
  }

  .card:hover {
    border-color: var(--border-strong, var(--border-default));
  }

  .card.busy {
    opacity: 0.6;
  }

  .progress {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    height: 3px;
    background: var(--surface-sunken);
  }

  .progress .fill {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.1s linear;
  }

  .card-main {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex: 1;
    min-width: 0;
    padding: var(--space-3);
    border: none;
    background: transparent;
    text-align: start;
    cursor: var(--cursor-action);
  }
  .card-main:hover {
    background: var(--surface-hover);
  }

  .dl-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: 40px;
    border: none;
    border-inline-start: var(--hairline) solid var(--border-subtle);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
  }
  .dl-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--accent);
  }

  .meta {
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

  .size {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .dl {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    color: var(--text-tertiary);
  }

  .card:hover .dl {
    color: var(--text-primary);
  }
</style>
