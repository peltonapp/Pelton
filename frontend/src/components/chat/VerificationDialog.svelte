<script lang="ts">
  // device verification, mounted once while chat is on. It follows the
  // verifications in flight: a request from another device to accept or
  // decline, waiting for the other side, the seven emoji both sides compare,
  // and how it ended. Comparing emoji is the only method offered; QR codes
  // would need a library to draw and a camera to read them.
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import { verifications, forgetVerification, chatError, refreshStatus } from '../../stores/chat'
  import {
    chatAcceptVerification,
    chatStartSAS,
    chatConfirmSAS,
    chatCancelVerification,
    chatDismissVerification,
  } from '../../lib/api'
  import { t } from '../../lib/i18n'
  import type { ChatVerification } from '../../lib/types'

  let busy = false
  let error = ''

  // the newest verification that still needs the user's eyes.
  $: current = [...$verifications].reverse().find((v) => v.state !== 'done' && v.state !== 'cancelled') ?? [...$verifications].reverse()[0] ?? null

  $: title = !current
    ? ''
    : current.self
      ? $t('chat.verify.titleSelf')
      : $t('chat.verify.titleUser').replace('{name}', current.userName || current.userId)

  async function act(run: () => Promise<void>): Promise<void> {
    busy = true
    error = ''
    try {
      await run()
    } catch (err) {
      error = chatError(err)
    } finally {
      busy = false
    }
  }

  function close(v: ChatVerification): void {
    if (v.state !== 'done' && v.state !== 'cancelled') {
      void chatCancelVerification(v.id, false).catch(() => {})
    }
    void chatDismissVerification(v.id).catch(() => {})
    forgetVerification(v.id)
    if (v.state === 'done') {
      void refreshStatus()
    }
    error = ''
  }
</script>

{#if current}
  {@const v = current}
  <Modal {title} size="small" on:close={() => close(v)}>
    {#if v.state === 'requested' && v.incoming}
      <p>{v.self ? $t('chat.verify.incomingSelf').replace('{device}', v.deviceId) : $t('chat.verify.incomingUser').replace('{name}', v.userName || v.userId)}</p>
      <p class="note">{$t('chat.verify.incomingNote')}</p>
    {:else if v.state === 'requested'}
      <div class="wait">
        <PieProgress label={$t('chat.verify.waiting')} />
        <p>{v.self ? $t('chat.verify.waitingSelf') : $t('chat.verify.waitingUser').replace('{name}', v.userName || v.userId)}</p>
      </div>
    {:else if v.state === 'ready'}
      <div class="wait">
        <PieProgress label={$t('chat.verify.starting')} />
        <p>{$t('chat.verify.ready')}</p>
      </div>
    {:else if v.state === 'sas'}
      <p>{$t('chat.verify.compare')}</p>
      <ol class="emojis">
        {#each v.emojis as e, i (i)}
          <li>
            <span class="emoji">{e.emoji}</span>
            <span class="desc">{e.description}</span>
          </li>
        {/each}
      </ol>
      {#if v.emojis.length === 0 && v.decimals.length > 0}
        <p class="decimals">{v.decimals.join('  ')}</p>
      {/if}
    {:else if v.state === 'confirmed'}
      <div class="wait">
        <PieProgress label={$t('chat.verify.waiting')} />
        <p>{$t('chat.verify.waitingConfirm')}</p>
      </div>
    {:else if v.state === 'done'}
      <p class="ok">{v.self ? $t('chat.verify.doneSelf') : $t('chat.verify.doneUser').replace('{name}', v.userName || v.userId)}</p>
    {:else}
      <p class="bad">{$t('chat.verify.cancelled')}</p>
      {#if v.reason}<p class="note">{v.reason}</p>{/if}
    {/if}
    {#if error}<p class="bad" role="alert">{error}</p>{/if}

    <svelte:fragment slot="footer">
      <div class="actions">
        {#if v.state === 'requested' && v.incoming}
          <button type="button" class="btn" disabled={busy} on:click={() => close(v)}>{$t('chat.verify.decline')}</button>
          <button type="button" class="btn primary" disabled={busy} on:click={() => void act(() => chatAcceptVerification(v.id))}>{$t('chat.verify.accept')}</button>
        {:else if v.state === 'ready' && v.incoming}
          <button type="button" class="btn" disabled={busy} on:click={() => close(v)}>{$t('chat.verify.cancel')}</button>
          <button type="button" class="btn primary" disabled={busy} on:click={() => void act(() => chatStartSAS(v.id))}>{$t('chat.verify.startEmoji')}</button>
        {:else if v.state === 'sas'}
          <button type="button" class="btn danger" disabled={busy} on:click={() => void act(() => chatCancelVerification(v.id, true))}>{$t('chat.verify.noMatch')}</button>
          <button type="button" class="btn primary" disabled={busy} on:click={() => void act(() => chatConfirmSAS(v.id))}>{$t('chat.verify.match')}</button>
        {:else if v.state === 'done' || v.state === 'cancelled'}
          <button type="button" class="btn primary" on:click={() => close(v)}>{$t('chat.verify.close')}</button>
        {:else}
          <button type="button" class="btn" on:click={() => close(v)}>{$t('chat.verify.cancel')}</button>
        {/if}
      </div>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  p {
    margin: 0 0 var(--space-3);
    font-size: var(--fz-body);
    color: var(--text-primary);
  }
  .note {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .ok {
    color: var(--success);
  }
  .bad {
    color: var(--danger);
  }

  .wait {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .wait p {
    margin: 0;
  }

  .emojis {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(calc(var(--chat-avatar) * 2.4), 1fr));
    gap: var(--space-3);
    margin: 0 0 var(--space-3);
    padding: 0;
    list-style: none;
  }
  .emojis li {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-2);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }
  .emoji {
    font-size: calc(var(--fz-title) * 1.6);
    line-height: 1.2;
  }
  .desc {
    color: var(--text-secondary);
    font-size: var(--fz-label);
    text-align: center;
  }
  .decimals {
    font-family: var(--font-mono);
    font-size: var(--fz-heading);
    letter-spacing: 0.1em;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  .btn {
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    cursor: var(--cursor-action);
  }
  .btn.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }
  .btn.danger {
    color: var(--danger);
  }
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
</style>
