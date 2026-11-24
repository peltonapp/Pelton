<script lang="ts">
  // the friction a strong sender warning adds: before a reply, who it will
  // really go to; before a link, where it really opens, with the choice of
  // skipping a tracking redirect. Either answer is for this one action; going
  // ahead also stops asking for the rest of this message.
  import Modal from '../common/Modal.svelte'
  import { t } from '../../lib/i18n'
  import { fill } from '../../lib/passport'
  import { openURL } from '../../lib/browser'
  import { openReplyWith } from '../../stores/compose'
  import { acknowledge, linkCheck, replyCheck } from '../../stores/trust'

  function replyAnyway(): void {
    const check = $replyCheck
    replyCheck.set(null)
    if (check) {
      acknowledge(check.detail.id)
      openReplyWith(check.detail, check.mode, check.kind, check.typed)
    }
  }

  function open(direct: boolean): void {
    const check = $linkCheck
    linkCheck.set(null)
    if (check) {
      acknowledge(check.messageId)
      openURL(direct && check.item?.destination ? check.item.destination : check.url)
    }
  }
</script>

{#if $replyCheck}
  <Modal title={$t('trust.reply.title')} size="small" on:close={() => replyCheck.set(null)}>
    <p class="body">{fill($t('trust.reply.body'), { address: $replyCheck.to })}</p>
    <div class="row">
      <button type="button" class="cal-btn" on:click={() => replyCheck.set(null)}>{$t('settingsPanel.button.cancel')}</button>
      <button type="button" class="cal-btn primary" on:click={replyAnyway}>{$t('trust.reply.anyway')}</button>
    </div>
  </Modal>
{:else if $linkCheck}
  <Modal title={$t('trust.link.title')} size="small" on:close={() => linkCheck.set(null)}>
    <p class="body url" dir="ltr">{fill($t('trust.link.body'), { url: $linkCheck.item?.destination || $linkCheck.url })}</p>
    {#if $linkCheck.item?.destination}
      <p class="body">{fill($t('trust.link.via'), { host: $linkCheck.item.host })}</p>
    {/if}
    <div class="row">
      <button type="button" class="cal-btn" on:click={() => linkCheck.set(null)}>{$t('settingsPanel.button.cancel')}</button>
      {#if $linkCheck.item?.destination}
        <button type="button" class="cal-btn" on:click={() => open(false)}>{$t('trust.link.open')}</button>
        <button type="button" class="cal-btn primary" on:click={() => open(true)}>{$t('trust.link.direct')}</button>
      {:else}
        <button type="button" class="cal-btn primary" on:click={() => open(false)}>{$t('trust.link.open')}</button>
      {/if}
    </div>
  </Modal>
{/if}

<style>
  .body {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-body);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .url {
    font-family: var(--font-mono);
    font-size: var(--fz-label);
  }

  .row {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
</style>
