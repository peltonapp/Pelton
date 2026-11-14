<script lang="ts">
  // the message action toolbar: reply, reply-all, forward, archive, delete, flag.
  // it only dispatches intents; the detail container performs them. icon-only
  // buttons carry aria-labels via IconButton.
  import { createEventDispatcher } from 'svelte'
  import {
    IconArrowBackUp,
    IconArrowBackUpDouble,
    IconArrowForwardUp,
    IconArchive,
    IconTrash,
    IconFlag,
    IconFlagFilled,
    IconPrinter,
    IconFileExport,
    IconInfoCircle,
    IconCode,
    IconShieldSearch,
  } from '@tabler/icons-svelte'
  import IconButton from '../common/IconButton.svelte'
  import { t } from '../../lib/i18n'
  import { online } from '../../stores/network'
  import { shortcutTitle } from '../../stores/shortcuts'

  export let flagged: boolean = false
  /** whether the VirusTotal scan action is offered. */
  export let canScan: boolean = false
  /** whether a scan of this message is currently running. */
  export let scanning: boolean = false

  const dispatch = createEventDispatcher<{
    reply: void
    replyAll: void
    forward: void
    archive: void
    delete: void
    toggleFlag: void
    print: void
    exportEml: void
    info: void
    source: void
    scan: void
  }>()

  $: flagLabel = flagged ? $t('detail.toolbar.unflag') : $t('detail.toolbar.flag')
</script>

<div class="toolbar" role="toolbar" aria-label={$t('detail.toolbar.ariaLabel')}>
  <IconButton label={$t('action.reply')} title={$shortcutTitle($t('action.reply'), 'reply')} on:click={() => dispatch('reply')}>
    <IconArrowBackUp size={18} stroke={1.6} />
  </IconButton>
  <IconButton
    label={$t('detail.toolbar.replyAll')}
    title={$shortcutTitle($t('detail.toolbar.replyAll'), 'reply-all')}
    on:click={() => dispatch('replyAll')}
  >
    <IconArrowBackUpDouble size={18} stroke={1.6} />
  </IconButton>
  <IconButton label={$t('action.forward')} title={$shortcutTitle($t('action.forward'), 'forward')} on:click={() => dispatch('forward')}>
    <IconArrowForwardUp size={18} stroke={1.6} />
  </IconButton>

  <span class="divider" aria-hidden="true"></span>

  <IconButton label={$t('action.archive')} title={$shortcutTitle($t('action.archive'), 'archive')} on:click={() => dispatch('archive')}>
    <IconArchive size={18} stroke={1.6} />
  </IconButton>
  <IconButton label={flagLabel} title={$shortcutTitle(flagLabel, 'flag')} active={flagged} toggle on:click={() => dispatch('toggleFlag')}>
    {#if flagged}
      <IconFlagFilled size={18} />
    {:else}
      <IconFlag size={18} stroke={1.6} />
    {/if}
  </IconButton>
  <IconButton
    label={$t('action.delete')}
    title={$shortcutTitle($t('action.delete'), 'delete-message')}
    danger
    on:click={() => dispatch('delete')}
  >
    <IconTrash size={18} stroke={1.6} />
  </IconButton>

  {#if canScan}
    <IconButton
      label={$t('virustotal.scanMessage')}
      title={$online ? $t('virustotal.scanMessage') : $t('common.network.offlineTitle')}
      disabled={scanning || !$online}
      on:click={() => dispatch('scan')}
    >
      <IconShieldSearch size={18} stroke={1.6} />
    </IconButton>
  {/if}

  <span class="divider" aria-hidden="true"></span>

  <IconButton
    label={$t('detail.toolbar.print')}
    title={$shortcutTitle($t('detail.toolbar.print'), 'export-pdf')}
    on:click={() => dispatch('print')}
  >
    <IconPrinter size={18} stroke={1.6} />
  </IconButton>
  <IconButton label={$t('menu.exportEml')} on:click={() => dispatch('exportEml')}>
    <IconFileExport size={18} stroke={1.6} />
  </IconButton>
  <IconButton label={$t('detail.toolbar.messageInfo')} on:click={() => dispatch('info')}>
    <IconInfoCircle size={18} stroke={1.6} />
  </IconButton>
  <IconButton
    label={$t('detail.toolbar.viewSource')}
    title={$online ? $t('detail.toolbar.viewSource') : $t('common.network.offlineTitle')}
    disabled={!$online}
    on:click={() => dispatch('source')}
  >
    <IconCode size={18} stroke={1.6} />
  </IconButton>
</div>

<style>
  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }

  .divider {
    width: var(--hairline);
    height: 18px;
    margin: 0 var(--space-2);
    background: var(--border-default);
  }
</style>
