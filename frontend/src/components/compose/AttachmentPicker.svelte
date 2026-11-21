<script lang="ts">
  // attach files to a compose session. files are read as base64 so they cross the
  // bindings boundary as strings; a progress bar shows while large files are read.
  // attached files render as cards with a type icon, size and a remove button.
  import { IconPaperclip, IconX } from '@tabler/icons-svelte'
  import { updateCompose, type ComposeSession } from '../../stores/compose'
  import { formatBytes } from '../../lib/format'
  import { fileIcon } from '../../lib/filetype'
  import { errorMessage, toastError } from '../../stores/toast'
  import type { ComposeAttachment } from '../../lib/types'
  import { t } from '../../lib/i18n'

  export let session: ComposeSession

  let input: HTMLInputElement

  // files currently being read, shown as progress cards until they land in the
  // session.
  interface Reading {
    id: number
    name: string
    type: string
    progress: number
  }
  let reading: Reading[] = []
  let nextId = 1

  // readAsBase64 returns the base64 payload (without the data-url prefix) and
  // reports progress to the matching reading card.
  function readAsBase64(file: File, id: number): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onprogress = (e) => {
        if (e.lengthComputable) {
          setProgress(id, Math.round((e.loaded / e.total) * 100))
        }
      }
      reader.onload = () => {
        const result = String(reader.result)
        const comma = result.indexOf(',')
        resolve(comma >= 0 ? result.slice(comma + 1) : result)
      }
      reader.onerror = () => reject(reader.error)
      reader.readAsDataURL(file)
    })
  }

  function setProgress(id: number, progress: number): void {
    reading = reading.map((r) => (r.id === id ? { ...r, progress } : r))
  }

  async function onPick(event: Event): Promise<void> {
    const files = (event.currentTarget as HTMLInputElement).files
    if (!files) {
      return
    }
    for (const file of Array.from(files)) {
      const id = nextId++
      const type = file.type || 'application/octet-stream'
      reading = [...reading, { id, name: file.name, type, progress: 0 }]
      try {
        const contentBase64 = await readAsBase64(file, id)
        const att: ComposeAttachment = { filename: file.name, contentType: type, contentBase64, inline: false, contentId: '' }
        updateCompose(session.id, { attachments: [...session.attachments, att] })
      } catch (err) {
        toastError(errorMessage(err))
      } finally {
        reading = reading.filter((r) => r.id !== id)
      }
    }
    input.value = ''
  }

  function remove(index: number): void {
    updateCompose(session.id, {
      attachments: session.attachments.filter((_, i) => i !== index),
    })
  }
</script>

<div class="picker">
  <button type="button" class="attach" on:click={() => input.click()}>
    <IconPaperclip size={15} stroke={1.6} />
    {$t('compose.attach.button')}
  </button>
  <input bind:this={input} type="file" multiple hidden on:change={onPick} />

  {#if session.attachments.length > 0 || reading.length > 0}
    <div class="cards">
      {#each session.attachments as att, index (index)}
        <div class="card">
          <span class="glyph"><svelte:component this={fileIcon(att.contentType, att.filename)} size={18} stroke={1.5} /></span>
          <span class="meta">
            <span class="name" title={att.filename}>{att.filename}</span>
            <span class="size">{formatBytes(att.contentBase64.length * 0.75)}</span>
          </span>
          <button type="button" class="rm" aria-label={`${$t('compose.attach.remove')} ${att.filename}`} on:click={() => remove(index)}>
            <IconX size={13} stroke={1.8} />
          </button>
        </div>
      {/each}

      {#each reading as r (r.id)}
        <div class="card reading">
          <span class="glyph"><svelte:component this={fileIcon(r.type, r.name)} size={18} stroke={1.5} /></span>
          <span class="meta">
            <span class="name" title={r.name}>{r.name}</span>
            <span class="bar"><span class="fill" style={`width:${r.progress}%`}></span></span>
          </span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .picker {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .attach {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .attach:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .cards {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .card {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 200px;
    max-width: 100%;
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
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
    gap: 3px;
  }

  .name {
    font-size: var(--fz-meta);
    color: var(--text-primary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .size {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .bar {
    height: 3px;
    border-radius: 999px;
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .fill {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.1s linear;
  }

  .rm {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
    flex-shrink: 0;
  }

  .rm:hover {
    color: var(--danger);
    background: var(--surface-hover);
  }
</style>
