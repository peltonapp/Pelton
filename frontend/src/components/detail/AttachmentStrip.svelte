<script lang="ts">
  // the attachments of a message in the new reader, as a strip of chips under
  // the body: type glyph, name, size, and buttons to open and save. inline
  // parts already shown in the body are left out.
  import imageIcon from '@phosphor-icons/core/assets/duotone/image-duotone.svg?raw'
  import audioIcon from '@phosphor-icons/core/assets/duotone/music-note-duotone.svg?raw'
  import videoIcon from '@phosphor-icons/core/assets/duotone/film-strip-duotone.svg?raw'
  import pdfIcon from '@phosphor-icons/core/assets/duotone/file-pdf-duotone.svg?raw'
  import docIcon from '@phosphor-icons/core/assets/duotone/file-doc-duotone.svg?raw'
  import sheetIcon from '@phosphor-icons/core/assets/duotone/file-xls-duotone.svg?raw'
  import slidesIcon from '@phosphor-icons/core/assets/duotone/file-ppt-duotone.svg?raw'
  import archiveIcon from '@phosphor-icons/core/assets/duotone/file-zip-duotone.svg?raw'
  import textIcon from '@phosphor-icons/core/assets/duotone/file-text-duotone.svg?raw'
  import fileIcon from '@phosphor-icons/core/assets/duotone/file-duotone.svg?raw'
  import eyeIcon from '@phosphor-icons/core/assets/duotone/eye-duotone.svg?raw'
  import saveIcon from '@phosphor-icons/core/assets/duotone/download-simple-duotone.svg?raw'
  import clipIcon from '@phosphor-icons/core/assets/duotone/paperclip-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/regular/check.svg?raw'
  import { onDestroy } from 'svelte'
  import { onAttachmentProgress } from '../../lib/events'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import VerdictBadge from '../common/VerdictBadge.svelte'
  import { saveAttachment, saveAllAttachments } from '../../lib/api'
  import { fileKind, previewKind, type FileKind } from '../../lib/filetype'
  import { formatBytes } from '../../lib/format'
  import { openPreview } from '../../stores/preview'
  import { attachmentVerdicts } from '../../stores/virustotal'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { Attachment } from '../../lib/types'

  /** The message the attachments belong to. */
  export let messageId: number
  /** Every part of the message; inline ones are skipped. */
  export let attachments: Attachment[]

  const icons: Record<FileKind, string> = {
    image: imageIcon,
    audio: audioIcon,
    video: videoIcon,
    pdf: pdfIcon,
    doc: docIcon,
    sheet: sheetIcon,
    slides: slidesIcon,
    archive: archiveIcon,
    text: textIcon,
    other: fileIcon,
  }

  $: visible = attachments.filter((a) => !a.inline)
  $: totalBytes = visible.reduce((sum, a) => sum + a.sizeBytes, 0)

  const doneShownMs = 1200
  const fillMs = 260

  // the attachments being saved, by id.
  let saving = new Set<number>()
  let savingAll = false
  // how far each save has got, 0 to 1. absent until the first bytes are written,
  // which is while the save dialog is still open.
  let progress = new Map<number, number>()
  // the attachments whose save just finished, shown with a check for a moment.
  let done = new Set<number>()

  const stopProgress = onAttachmentProgress((e) => {
    if (!e.running || !saving.has(e.attachmentId)) {
      return
    }
    const share = e.bytesTotal > 0 ? e.bytesDone / e.bytesTotal : 1
    progress = new Map(progress).set(e.attachmentId, share)
  })
  onDestroy(stopProgress)

  function showDone(id: number): void {
    done = new Set(done).add(id)
    setTimeout(() => {
      const next = new Set(done)
      next.delete(id)
      done = next
    }, doneShownMs)
  }

  function preview(att: Attachment): void {
    openPreview(messageId, att)
  }

  async function save(att: Attachment): Promise<void> {
    if (saving.has(att.id)) {
      return
    }
    saving = new Set(saving).add(att.id)
    try {
      const path = await saveAttachment(messageId, att.id)
      if (path) {
        // the file is written from the local store, often faster than the eye:
        // let the pie run to full before it turns into the check.
        progress = new Map(progress).set(att.id, 1)
        await new Promise((resolve) => setTimeout(resolve, fillMs))
        showDone(att.id)
        toastSuccess($t('detail.attachments.saved').replace('{name}', att.filename))
      }
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      const next = new Set(saving)
      next.delete(att.id)
      saving = next
      const rest = new Map(progress)
      rest.delete(att.id)
      progress = rest
    }
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
  <div class="strip">
    <div class="head">
      <Ph svg={clipIcon} size={14} />
      <span>
        {visible.length}
        {visible.length === 1 ? $t('detail.attachments.one') : $t('detail.attachments.many')}
        <span class="total">{formatBytes(totalBytes)}</span>
      </span>
      {#if visible.length > 1}
        <button type="button" class="save-all" disabled={savingAll} on:click={saveAll}>
          <Ph svg={saveIcon} size={14} />
          {$t('detail.attachments.saveAll')}
        </button>
      {/if}
    </div>
    <ul class="chips" aria-label={$t('detail.infoModal.attachments')}>
      {#each visible as att (att.id)}
        <li class="chip">
          <span class="glyph">
            <Ph svg={icons[fileKind(att.contentType, att.filename)]} size={16} />
          </span>
          <span class="name" title={att.filename}>{att.filename}</span>
          <span class="size">{formatBytes(att.sizeBytes)}</span>
          {#if $attachmentVerdicts.has(att.id)}
            <VerdictBadge verdict={$attachmentVerdicts.get(att.id)!} size={12} />
          {/if}
          {#if previewKind(att.contentType, att.filename) !== 'none'}
            <button
              type="button"
              class="act"
              aria-label={$t('detail.attachments.preview').replace('{name}', att.filename)}
              title={$t('detail.attachments.preview').replace('{name}', att.filename)}
              on:click={() => preview(att)}
            >
              <Ph svg={eyeIcon} size={14} />
            </button>
          {/if}
          <button
            type="button"
            class="act"
            aria-label={$t('detail.attachments.download').replace('{name}', att.filename)}
            title={$t('detail.attachments.download').replace('{name}', att.filename)}
            class:busy={saving.has(att.id)}
            class:done={done.has(att.id)}
            disabled={saving.has(att.id)}
            on:click={() => save(att)}
          >
            {#if saving.has(att.id)}
              <PieProgress size={14} value={progress.get(att.id) ?? null} label={att.filename} />
            {:else if done.has(att.id)}
              <span class="pop"><Ph svg={checkIcon} size={14} /></span>
            {:else}
              <Ph svg={saveIcon} size={14} />
            {/if}
          </button>
        </li>
      {/each}
    </ul>
  </div>
{/if}

<style>
  .strip {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding-top: var(--space-4);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .total {
    margin-inline-start: var(--space-2);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .save-all {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin-inline-start: auto;
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .save-all:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-width: 100%;
    height: calc(var(--control-height) - var(--space-1));
    padding-inline: var(--space-3) var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-meta);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .chip:hover {
    border-color: var(--border-strong);
  }

  .glyph {
    display: inline-flex;
    color: var(--accent);
  }

  .name {
    min-width: 0;
    max-width: 26ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .size {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .act:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .act:disabled {
    cursor: default;
  }

  .act.busy,
  .act.done {
    color: var(--accent);
  }

  .pop {
    display: inline-flex;
    animation: pop var(--motion-base) var(--ease-out);
  }

  @keyframes pop {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
</style>
