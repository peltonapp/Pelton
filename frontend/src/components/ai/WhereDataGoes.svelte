<script lang="ts">
  // a small mark saying where an AI provider sends what a script hands it:
  // off this device, or to this device or the local network.
  import { t } from '../../lib/i18n'

  /** The provider's address names this device or a private network. */
  export let local: boolean
</script>

<span class="mark" class:cloud={!local} title={local ? $t('ai.local.hint') : $t('ai.cloud.hint')}>
  {local ? $t('ai.local') : $t('ai.cloud')}
</span>

<style>
  .mark {
    display: inline-flex;
    align-items: center;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--success-bg);
    color: var(--success);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    white-space: nowrap;
  }

  .mark.cloud {
    background: var(--warning-bg);
    color: var(--warning);
  }
</style>
