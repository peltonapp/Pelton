<script lang="ts">
  // the bottom of the app window: a few stand-in rows above the status bar,
  // which carries the unread count on the left and the sync state, low power
  // mark and clock on the right. each part appears only when its setting is on.
  import syncIcon from '@phosphor-icons/core/assets/duotone/arrows-clockwise-duotone.svg?raw'
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-duotone.svg?raw'
  import powerIcon from '@phosphor-icons/core/assets/duotone/lightning-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'
  import Ph from '../../common/Ph.svelte'
  import { prefs } from '../../../stores/prefs'
  import { formatStatusClock, type TimeFormat } from '../../../lib/format'
  import { sampleMessages } from './samples'
  import { t } from '../../../lib/i18n'

  /** Shows the date and time. */
  export let clock: boolean = true
  /** Shows the unread count. */
  export let unread: boolean = true
  /** Shows the progress bar of a running sync. */
  export let progress: boolean = false
  /** Names the mailbox, account and server being synced. */
  export let verbose: boolean = false
  /** Shows the low power mark. */
  export let lowPower: boolean = false
  /** Draws a sync in progress instead of a finished one. */
  export let syncing: boolean = false

  const mailbox = 'INBOX'
  const server = 'imap.compiler.example'
  const account = sampleMessages[1].email

  $: label = verbose
    ? $t('common.statusBar.syncingMailboxFull')
        .replace('{mailbox}', mailbox)
        .replace('{account}', account)
        .replace('{server}', server)
    : $t('common.statusBar.syncing')
  $: time = formatStatusClock(new Date(), $prefs.timeFormat as TimeFormat)
</script>

<Frame sunken>
  <div class="window">
    <div class="body"><MiniWindow /></div>
    <div class="status">
      <div class="left">
        {#if unread}
          <span class="unread">{$t('common.statusBar.unread').replace('{n}', '3')}</span>
        {/if}
      </div>
      <div class="right">
        {#if lowPower}
          <span class="power"><Ph svg={powerIcon} size={12} />{$t('common.statusBar.lowPower')}</span>
        {/if}
        {#if syncing}
          <span class="sync">
            <Ph svg={syncIcon} size={12} />
            <span class="text">{label}</span>
            {#if progress}
              <span class="meter"><span class="fill"></span></span>
              <span class="counts">120 / 480</span>
            {/if}
          </span>
        {:else}
          <span class="sync"><Ph svg={checkIcon} size={12} />{$t('common.statusBar.synced')}</span>
        {/if}
        {#if clock}
          <span class="clock">{time}</span>
        {/if}
      </div>
    </div>
  </div>
</Frame>

<style>
  /* the frame is padded; this picture is a window, so it takes the room back. */
  .window {
    margin: calc(-1 * var(--space-4));
    border-radius: inherit;
    overflow: hidden;
  }

  .body {
    height: 60px;
  }

  .status {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-4);
    padding: var(--space-2) var(--space-3);
    border-top: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .left,
  .right {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    min-height: calc(var(--fz-meta) + var(--space-3));
  }

  .right {
    flex: 1;
    justify-content: flex-end;
    min-width: 0;
  }

  .unread {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .sync,
  .power {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
  }

  .text {
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .meter {
    width: 36px;
    height: 3px;
    border-radius: 2px;
    background: var(--surface-sunken);
    overflow: hidden;
  }

  .fill {
    display: block;
    width: 25%;
    height: 100%;
    background: var(--accent);
    animation: fill calc(var(--motion-slow) * 10) var(--ease-in-out) infinite alternate;
  }

  .counts,
  .clock {
    font-variant-numeric: tabular-nums;
    color: var(--text-tertiary);
  }

  @keyframes fill {
    to {
      width: 100%;
    }
  }
</style>
