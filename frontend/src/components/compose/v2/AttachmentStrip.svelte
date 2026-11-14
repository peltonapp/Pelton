<script lang="ts">
  // the attachments of the new composer as a strip of chips under the header:
  // file type glyph, name, size and a remove button. a file being read shows
  // as a chip with a pie loader until it lands in the session. the composer's
  // Attach button and dropped files come in through pick() and add().
  import { flip } from 'svelte/animate'
  import { fly, scale } from 'svelte/transition'
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
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../../common/Ph.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import { getSession, updateCompose, type ComposeSession } from '../../../stores/compose'
  import { attachmentSize, readAttachment } from '../../../lib/attachments'
  import { fileKind, type FileKind } from '../../../lib/filetype'
  import { formatBytes } from '../../../lib/format'
  import { prefs } from '../../../stores/prefs'
  import { errorMessage, toastError } from '../../../stores/toast'
  import { t } from '../../../lib/i18n'

  /** The session whose attachments the strip shows. */
  export let session: ComposeSession

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

  interface Reading {
    id: number
    name: string
    type: string
    progress: number
  }

  let input: HTMLInputElement
  let reading: Reading[] = []
  let nextId = 1

  $: motion = $prefs.reduceMotion ? 0 : 170
  $: total = session.attachments.reduce((sum, att) => sum + attachmentSize(att), 0)

  /** pick opens the system file picker. */
  export function pick(): void {
    input.click()
  }

  /** add reads files into the session, one chip each. */
  export async function add(files: FileList | File[]): Promise<void> {
    for (const file of Array.from(files)) {
      const id = nextId++
      reading = [...reading, { id, name: file.name, type: file.type, progress: 0 }]
      try {
        const att = await readAttachment(file, (progress) => {
          reading = reading.map((r) => (r.id === id ? { ...r, progress } : r))
        })
        const current = getSession(session.id) ?? session
        updateCompose(session.id, { attachments: [...current.attachments, att] })
      } catch (err) {
        toastError(errorMessage(err))
      } finally {
        reading = reading.filter((r) => r.id !== id)
      }
    }
  }

  async function onPick(): Promise<void> {
    if (input.files) {
      await add(input.files)
    }
    input.value = ''
  }

  function remove(index: number): void {
    const current = getSession(session.id) ?? session
    updateCompose(session.id, { attachments: current.attachments.filter((_, i) => i !== index) })
  }
</script>

<input bind:this={input} type="file" multiple hidden on:change={onPick} />

{#if session.attachments.length > 0 || reading.length > 0}
  <div class="strip" role="list" aria-label={$t('detail.infoModal.attachments')} transition:fly={{ y: -4, duration: motion }}>
    {#each session.attachments as att, index (att.filename + index)}
      <div class="chip" role="listitem" animate:flip={{ duration: motion }} in:fly={{ x: -8, duration: motion }} out:scale={{ start: 0.9, duration: motion }}>
        <span class="glyph"><Ph svg={icons[fileKind(att.contentType, att.filename)]} size={16} /></span>
        <span class="name" title={att.filename}>{att.filename}</span>
        <span class="size">{formatBytes(attachmentSize(att))}</span>
        <button type="button" class="rm" aria-label={`${$t('compose.attach.remove')} ${att.filename}`} on:click={() => remove(index)}>
          <Ph svg={xIcon} size={12} />
        </button>
      </div>
    {/each}
    {#each reading as r (r.id)}
      <div class="chip reading" role="listitem" in:fly={{ x: -8, duration: motion }}>
        <span class="glyph"><PieProgress value={r.progress / 100} size={14} label={r.name} /></span>
        <span class="name" title={r.name}>{r.name}</span>
      </div>
    {/each}
    {#if session.attachments.length > 1}
      <span class="total">{formatBytes(total)}</span>
    {/if}
  </div>
{/if}

<style>
  .strip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-width: 100%;
    height: calc(var(--control-height) - var(--space-2));
    padding-inline: var(--space-2) var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-meta);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .chip:hover {
    border-color: var(--border-strong);
  }

  .chip.reading {
    padding-inline-end: var(--space-3);
    color: var(--text-tertiary);
  }

  .glyph {
    display: inline-flex;
    color: var(--accent);
  }

  .name {
    max-width: 22ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-primary);
  }

  .size,
  .total {
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .total {
    margin-inline-start: auto;
    font-size: var(--fz-meta);
  }

  .rm {
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

  .rm:hover {
    background: var(--danger-bg);
    color: var(--danger);
  }
</style>
