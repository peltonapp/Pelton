<script lang="ts">
  // the message info modal opened from the "i" button. it lays out everything we
  // know about the open message in copyable rows and offers a one-click "copy as
  // Markdown" of the whole thing. raw headers and parsed authentication results
  // (spf/dkim/dmarc) are a documented backend follow-up: they need an on-demand
  // header fetch, so we show an honest placeholder rather than faking a result.
  import { createEventDispatcher } from 'svelte'
  import { IconCopy } from '@tabler/icons-svelte'
  import Modal from '../common/Modal.svelte'
  import '../../theme/controls.css'
  import { formatFullDate, formatBytes, type TimeFormat } from '../../lib/format'
  import { prefs } from '../../stores/prefs'
  import { toastSuccess, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { MessageDetail } from '../../lib/types'

  export let detail: MessageDetail

  const dispatch = createEventDispatcher<{ close: void }>()

  $: pgpLabel = {
    none: $t('detail.infoModal.pgp.none'),
    signed: $t('detail.infoModal.pgp.signed'),
    encrypted: $t('detail.infoModal.pgp.encrypted'),
  } as Record<string, string>

  interface Field {
    label: string
    value: string
  }

  $: fields = buildFields(detail)
  function buildFields(d: MessageDetail): Field[] {
    const list: Field[] = [
      { label: $t('detail.infoModal.subject'), value: d.subject || $t('detail.noSubject') },
      { label: $t('detail.infoModal.from'), value: d.fromName ? `${d.fromName} <${d.fromAddress}>` : d.fromAddress },
      { label: $t('detail.infoModal.to'), value: d.toAddresses || '-' },
    ]
    if (d.ccAddresses) {
      list.push({ label: $t('detail.infoModal.cc'), value: d.ccAddresses })
    }
    list.push(
      { label: $t('detail.infoModal.date'), value: formatFullDate(d.date, $prefs.timeFormat as TimeFormat) || d.date },
      { label: $t('detail.infoModal.account'), value: d.accountEmail },
      { label: $t('detail.infoModal.folder'), value: d.folderName },
      { label: $t('detail.infoModal.format'), value: d.isHtml ? $t('detail.infoModal.formatHtml') : $t('detail.infoModal.formatPlain') },
      { label: $t('detail.infoModal.remoteContent'), value: d.hasRemoteContent ? $t('detail.infoModal.remoteContentPresent') : $t('detail.infoModal.remoteContentNone') },
      { label: $t('detail.infoModal.encryption'), value: pgpLabel[d.pgp] ?? d.pgp },
    )
    if (d.attachments.length > 0) {
      const names = d.attachments.map((a) => `${a.filename} (${formatBytes(a.sizeBytes)})`).join(', ')
      list.push({ label: $t('detail.infoModal.attachments'), value: names })
    }
    return list
  }

  function buildMarkdown(): string {
    const lines = [`## ${$t('detail.infoModal.markdownHeading')}`, '']
    for (const f of fields) {
      lines.push(`- **${f.label}:** ${f.value}`)
    }
    return lines.join('\n')
  }

  async function copyAll(): Promise<void> {
    try {
      await navigator.clipboard.writeText(buildMarkdown())
      toastSuccess($t('detail.infoModal.copiedMarkdown'))
    } catch {
      toastError($t('detail.infoModal.copyFailed'))
    }
  }

  async function copyOne(field: Field): Promise<void> {
    try {
      await navigator.clipboard.writeText(field.value)
      toastSuccess($t('detail.infoModal.copiedField').replace('{field}', field.label.toLowerCase()))
    } catch {
      toastError($t('detail.infoModal.copyFailed'))
    }
  }

</script>

<Modal title={$t('detail.infoModal.title')} size="medium" on:close={() => dispatch('close')}>
    <div class="fields selectable">
      {#each fields as field}
        <div class="field">
          <span class="key">{field.label}</span>
          <span class="val">{field.value}</span>
          <button type="button" class="copy" aria-label={$t('detail.infoModal.copyAriaLabel').replace('{field}', field.label)} on:click={() => copyOne(field)}>
            <IconCopy size={14} stroke={1.6} />
          </button>
        </div>
      {/each}

      <div class="field note">
        <span class="key">{$t('detail.infoModal.authentication')}</span>
        <span class="val muted">
          {$t('detail.infoModal.authenticationNote')}
        </span>
      </div>
    </div>

    <svelte:fragment slot="footer">
      <button type="button" class="cal-btn primary" on:click={copyAll}>
        {$t('detail.infoModal.copyAsMarkdown')}
      </button>
    </svelte:fragment>
</Modal>

<style>

  .field {
    display: grid;
    grid-template-columns: 120px 1fr auto;
    align-items: start;
    gap: var(--space-3);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .field:last-child {
    border-bottom: none;
  }

  .key {
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .val {
    font-size: var(--fz-label);
    color: var(--text-primary);
    word-break: break-word;
  }

  .val.muted {
    color: var(--text-tertiary);
    line-height: 1.5;
  }

  .copy {
    display: inline-flex;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: var(--space-1);
    border-radius: var(--radius-control);
  }

  .copy:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .note .val {
    grid-column: 2 / span 2;
  }
</style>
