<script lang="ts">
  // the Notifications page: where in-app toasts appear, whether new mail raises
  // a system notification, the unread badge on the dock or tray icon, and the
  // senders that always notify.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import ToastPositionPicker from '../../settings/ToastPositionPicker.svelte'
  import { prefs, setToastPosition, setNotifyNewMail, setUnreadBadgeEnabled } from '../../../stores/prefs'
  import { isMac, t } from '../../../lib/i18n'

  // the unread badge is a count on the dock tile on macOS and a dot on the tray
  // icon on Windows and Linux, so its row is labelled for the one at hand.
  const badgeToggleKey = isMac ? 'settingsPanel.toggle.dockBadge' : 'settingsPanel.toggle.trayBadge'
  const badgeHintKey = isMac ? 'settingsPanel.hint.dockBadge' : 'settingsPanel.hint.trayBadge'

  let vipOpen = false
</script>

<Card title={$t('settingsPanel.category.notifications')}>
  <Row id="toast-position" label={$t('settings.toastPosition')}>
    <ToastPositionPicker value={$prefs.toastPosition} on:change={(e) => setToastPosition(e.detail)} />
  </Row>
  <Row id="notify-new-mail" label={$t('vip.notifyNewMail')} hint={$t('vip.notifyNewMailHint')}>
    <ToggleSwitch checked={$prefs.notifyNewMail} label={$t('vip.notifyNewMail')} on:change={(e) => setNotifyNewMail(e.detail)} />
  </Row>
  <Row id="unread-badge" label={$t(badgeToggleKey)} hint={$t(badgeHintKey)}>
    <span slot="preview" class="badge-pic">
      <span class="tile">
        {#if $prefs.unreadBadge}
          <span class="badge" class:dot={!isMac}>{isMac ? '3' : ''}</span>
        {/if}
      </span>
    </span>
    <ToggleSwitch checked={$prefs.unreadBadge} label={$t(badgeToggleKey)} on:change={(e) => setUnreadBadgeEnabled(e.detail)} />
  </Row>
  <Row id="vip-senders" label={$t('vip.manageLabel')} hint={$t('vip.manageHint')}>
    <button type="button" class="action" on:click={() => (vipOpen = true)}>
      {$t('vip.manage')}
    </button>
  </Row>
</Card>

{#if vipOpen}
  {#await import('../../settings/VIPSendersModal.svelte') then m}
    <svelte:component this={m.default} on:close={() => (vipOpen = false)} />
  {/await}
{/if}

<style>
  .action {
    padding: var(--space-2) var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-size: var(--fz-label);
    color: var(--text-primary);
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .action:hover {
    background: var(--surface-hover);
  }

  .badge-pic {
    display: flex;
    justify-content: center;
    padding: var(--space-4);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
  }

  .tile {
    position: relative;
    width: 56px;
    height: 56px;
    border-radius: var(--radius-card);
    background: var(--accent);
  }

  .badge {
    position: absolute;
    top: -6px;
    inset-inline-end: -6px;
    min-width: 20px;
    height: 20px;
    padding: 0 var(--space-1);
    border-radius: 10px;
    background: var(--danger);
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    line-height: 20px;
    text-align: center;
    color: var(--text-inverse);
  }

  .badge.dot {
    top: 0;
    inset-inline-end: 0;
    min-width: 0;
    width: 14px;
    height: 14px;
    padding: 0;
    border-radius: 50%;
  }
</style>
