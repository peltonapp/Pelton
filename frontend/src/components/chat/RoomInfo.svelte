<script lang="ts">
  // the panel beside a room: its name, topic and encryption, its members,
  // inviting someone, verifying the person of a direct chat, and leaving.
  import { createEventDispatcher } from 'svelte'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import lockIcon from '@phosphor-icons/core/assets/duotone/lock-simple-duotone.svg?raw'
  import shieldIcon from '@phosphor-icons/core/assets/duotone/shield-check-duotone.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ChatAvatar from './ChatAvatar.svelte'
  import { chatMembers, chatInvite, chatLeave, chatStartVerification, chatUserTrust } from '../../lib/api'
  import { chatError, closeRoom, refreshRooms, chatStatus } from '../../stores/chat'
  import { askConfirm } from '../../stores/confirm'
  import { toastError, toastInfo, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ChatMember, ChatRoom } from '../../lib/types'

  /** The room described. */
  export let room: ChatRoom
  /** True in a narrow window, where the panel fills the space. */
  export let narrow: boolean = false

  const dispatch = createEventDispatcher<{ close: void }>()

  let members: ChatMember[] = []
  let loading = false
  let error = ''
  let invitee = ''
  let inviting = false
  let trusted: boolean | null = null
  let loadedFor = ''

  $: if (room.id !== loadedFor) {
    loadedFor = room.id
    void load()
  }

  async function load(): Promise<void> {
    loading = true
    error = ''
    trusted = null
    try {
      members = await chatMembers(room.id)
      if (room.kind === 'direct' && room.directUser) {
        trusted = await chatUserTrust(room.directUser).catch(() => null)
      }
    } catch (err) {
      error = chatError(err)
    } finally {
      loading = false
    }
  }

  async function invite(): Promise<void> {
    const who = invitee.trim()
    if (!who) {
      return
    }
    inviting = true
    try {
      await chatInvite(room.id, who)
      toastSuccess($t('chat.info.invited').replace('{user}', who))
      invitee = ''
      await load()
    } catch (err) {
      toastError(chatError(err))
    } finally {
      inviting = false
    }
  }

  async function leave(): Promise<void> {
    const ok = await askConfirm({
      title: $t('chat.info.leaveTitle').replace('{room}', room.name),
      body: room.encrypted ? $t('chat.info.leaveBodyEncrypted') : $t('chat.info.leaveBody'),
      confirmLabel: $t('chat.info.leave'),
      danger: true,
    })
    if (!ok) {
      return
    }
    try {
      await chatLeave(room.id)
      closeRoom()
      dispatch('close')
      await refreshRooms()
    } catch (err) {
      toastError(chatError(err))
    }
  }

  async function verify(): Promise<void> {
    try {
      await chatStartVerification(room.directUser)
      toastInfo($t('chat.verify.requestSentUser'))
    } catch (err) {
      toastError(chatError(err))
    }
  }

  function role(level: number): string {
    if (level >= 100) {
      return $t('chat.info.admin')
    }
    if (level >= 50) {
      return $t('chat.info.moderator')
    }
    return ''
  }
</script>

<aside class="info" class:narrow aria-label={$t('chat.room.info')}>
  <header>
    <h3>{$t('chat.room.info')}</h3>
    <button type="button" class="icon" aria-label={$t('chat.info.close')} title={$t('chat.info.close')} on:click={() => dispatch('close')}>
      <Ph svg={xIcon} size={14} />
    </button>
  </header>

  <div class="scroll">
    <div class="summary">
      <ChatAvatar mxc={room.avatar} name={room.name} seed={room.directUser || room.id} size="calc(var(--chat-avatar) * 2)" />
      <h4>{room.name || room.alias || room.id}</h4>
      {#if room.alias}<p class="muted">{room.alias}</p>{/if}
      {#if room.topic}<p class="topic">{room.topic}</p>{/if}
      <p class="enc" class:on={room.encrypted}>
        <Ph svg={lockIcon} size={13} />
        {room.encrypted ? $t('chat.info.encrypted') : $t('chat.info.notEncrypted')}
      </p>
    </div>

    {#if room.kind === 'direct' && room.directUser}
      <section>
        <h5>{$t('chat.info.verification')}</h5>
        {#if trusted === true}
          <p class="trust ok"><Ph svg={shieldIcon} size={14} />{$t('chat.info.verified')}</p>
        {:else}
          <p class="muted">{$t('chat.info.notVerified')}</p>
          {#if $chatStatus.encryption.ready}
            <button type="button" class="btn" on:click={() => void verify()}>{$t('chat.info.verify')}</button>
          {/if}
        {/if}
      </section>
    {/if}

    <section>
      <h5>{$t('chat.info.members').replace('{n}', String(members.length))}</h5>
      {#if loading}
        <PieProgress label={$t('chat.space.loading')} />
      {:else if error}
        <p class="muted">{error}</p>
      {:else}
        <ul class="members">
          {#each members as m (m.userId)}
            <li>
              <ChatAvatar mxc={m.avatar} name={m.name} seed={m.userId} size="var(--chat-room-avatar)" />
              <span class="m-text">
                <span class="m-name">{m.name}</span>
                <span class="m-id">{m.userId}</span>
              </span>
              {#if m.membership === 'invite'}
                <span class="tag">{$t('chat.info.invitedTag')}</span>
              {:else if role(m.powerLevel)}
                <span class="tag">{role(m.powerLevel)}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
      {#if room.kind !== 'direct'}
        <form class="invite" on:submit|preventDefault={() => void invite()}>
          <input
            bind:value={invitee}
            placeholder="@name:example.org"
            aria-label={$t('chat.info.inviteLabel')}
            disabled={inviting}
          />
          <button type="submit" class="btn" disabled={inviting || !invitee.trim()}>{$t('chat.info.invite')}</button>
        </form>
      {/if}
    </section>

    <section>
      <button type="button" class="btn danger" on:click={() => void leave()}>{$t('chat.info.leave')}</button>
    </section>
  </div>
</aside>

<style>
  .info {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border-left: var(--hairline) solid var(--border-subtle);
    background: var(--surface-base);
  }
  .info.narrow {
    border-left: none;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: var(--space-3) var(--pane-pad);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }
  h3 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
  }

  .scroll {
    flex: 1;
    overflow-y: auto;
    padding: var(--pane-pad);
  }

  .summary {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-2);
    padding-bottom: var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
    text-align: center;
  }
  h4 {
    margin: var(--space-2) 0 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
    overflow-wrap: anywhere;
  }
  .topic {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    white-space: pre-wrap;
  }
  .muted {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .enc {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .enc.on {
    color: var(--success);
  }

  section {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-4) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }
  section:last-child {
    border-bottom: none;
  }
  h5 {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    letter-spacing: 0.04em;
    text-transform: uppercase;
  }

  .trust {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    margin: 0;
    font-size: var(--fz-meta);
  }
  .trust.ok {
    color: var(--success);
  }

  .members {
    width: 100%;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .members li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) 0;
  }
  .m-text {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }
  .m-name,
  .m-id {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .m-name {
    font-size: var(--fz-body);
  }
  .m-id {
    color: var(--text-tertiary);
    font-size: var(--fz-label);
  }
  .tag {
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .invite {
    display: flex;
    gap: var(--space-2);
    width: 100%;
  }
  .invite input {
    flex: 1;
    min-width: 0;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }
  .icon:hover {
    background: var(--surface-hover);
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
  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
  }
  .btn.danger {
    color: var(--danger);
  }
</style>
