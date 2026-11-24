<script lang="ts">
  // an app window with one notification toast pinned to the chosen corner or
  // edge, where the real toasts stack.
  import checkIcon from '@phosphor-icons/core/assets/duotone/check-circle-duotone.svg?raw'
  import Frame from './Frame.svelte'
  import MiniWindow from './MiniWindow.svelte'
  import Ph from '../../common/Ph.svelte'
  import { t } from '../../../lib/i18n'

  /** Where the toast sits, as top or bottom then left, center or right. */
  export let position: string = 'bottom-right'
</script>

<Frame sunken>
  <div class="window">
    <MiniWindow />
    <div class="toast {position}">
      <Ph svg={checkIcon} size={14} />
      <span>{$t('settingsPanel.notificationPreview')}</span>
    </div>
  </div>
</Frame>

<style>
  .window {
    position: relative;
    height: 110px;
    border: var(--hairline) solid var(--border-default);
    border-radius: calc(var(--radius-card) - 2px);
    overflow: hidden;
  }

  .toast {
    position: absolute;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    font-size: var(--fz-meta);
    color: var(--text-primary);
    white-space: nowrap;
  }

  .toast :global(.ph) {
    color: var(--success);
  }

  .top-left,
  .top-center,
  .top-right {
    top: 0;
  }

  .bottom-left,
  .bottom-center,
  .bottom-right {
    bottom: 0;
  }

  .top-left,
  .bottom-left {
    left: 0;
  }

  .top-right,
  .bottom-right {
    right: 0;
  }

  .top-center,
  .bottom-center {
    left: 50%;
    margin-inline: 0;
    transform: translateX(-50%);
  }
</style>
