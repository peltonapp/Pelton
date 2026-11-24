<script lang="ts">
  // starting something new in chat: a direct chat with a person by Matrix ID
  // (looked up first, so it is clear who it is), a new room, or joining a
  // room by its address or a matrix.to link. A new direct chat and, unless
  // turned off, a new room are end to end encrypted.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ChatAvatar from './ChatAvatar.svelte'
  import { chatCreateRoom, chatJoin, chatLookupProfile, chatOpenDirect } from '../../lib/api'
  import { chatError, refreshRooms } from '../../stores/chat'
  import { t } from '../../lib/i18n'
  import type { ChatProfile } from '../../lib/types'

  const dispatch = createEventDispatcher<{ close: void; opened: string }>()

  type Mode = 'direct' | 'room' | 'join'
  let mode: Mode = 'direct'
  let busy = false
  let error = ''

  let person = ''
  let profile: ChatProfile | null = null
  let lookedUp = ''

  let name = ''
  let topic = ''
  let invites = ''
  let encrypted = true
  let isPublic = false

  let address = ''

  $: validId = /^@[^:\s]+:\S+$/.test(person.trim())
  $: dirty = !!(person || name || topic || invites || address)

  async function lookup(): Promise<void> {
    const id = person.trim()
    if (!validId || id === lookedUp) {
      return
    }
    lookedUp = id
    profile = null
    try {
      profile = await chatLookupProfile(id)
    } catch {
      profile = null
    }
  }

  async function submit(): Promise<void> {
    busy = true
    error = ''
    try {
      let roomId = ''
      if (mode === 'direct') {
        roomId = await chatOpenDirect(person.trim())
      } else if (mode === 'room') {
        roomId = await chatCreateRoom({
          name: name.trim(),
          topic: topic.trim(),
          invite: invites
            .split(/[\s,]+/)
            .map((s) => s.trim())
            .filter(Boolean),
          encrypted: encrypted && !isPublic,
          public: isPublic,
        })
      } else {
        roomId = await chatJoin(address.trim())
      }
      await refreshRooms()
      dispatch('opened', roomId)
      dispatch('close')
    } catch (err) {
      error = chatError(err)
    } finally {
      busy = false
    }
  }

  $: ready = mode === 'direct' ? validId : mode === 'room' ? !!name.trim() : !!address.trim()
</script>

<Modal title={$t('chat.new.title')} size="small" {dirty} on:close={() => dispatch('close')}>
  <div class="tabs" role="tablist" aria-label={$t('chat.new.title')}>
    <button type="button" role="tab" aria-selected={mode === 'direct'} class:active={mode === 'direct'} on:click={() => (mode = 'direct')}>{$t('chat.new.direct')}</button>
    <button type="button" role="tab" aria-selected={mode === 'room'} class:active={mode === 'room'} on:click={() => (mode = 'room')}>{$t('chat.new.room')}</button>
    <button type="button" role="tab" aria-selected={mode === 'join'} class:active={mode === 'join'} on:click={() => (mode = 'join')}>{$t('chat.new.join')}</button>
  </div>

  <form id="new-chat" class="form" on:submit|preventDefault={() => void submit()}>
    {#if mode === 'direct'}
      <label>
        <span>{$t('chat.new.person')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input bind:value={person} placeholder="@name:example.org" autofocus on:blur={() => void lookup()} />
      </label>
      {#if profile}
        <div class="profile">
          <ChatAvatar mxc={profile.avatar} name={profile.name || profile.userId} seed={profile.userId} />
          <span>{profile.name || profile.userId}</span>
        </div>
      {/if}
      <p class="note">{$t('chat.new.directNote')}</p>
    {:else if mode === 'room'}
      <label>
        <span>{$t('chat.new.name')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input bind:value={name} autofocus />
      </label>
      <label>
        <span>{$t('chat.new.topic')}</span>
        <input bind:value={topic} />
      </label>
      <label>
        <span>{$t('chat.new.invite')}</span>
        <input bind:value={invites} placeholder="@name:example.org" />
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={isPublic} />
        <span>{$t('chat.new.public')}</span>
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={encrypted} disabled={isPublic} />
        <span>{$t('chat.new.encrypted')}</span>
      </label>
      <p class="note">{isPublic ? $t('chat.new.publicNote') : $t('chat.new.encryptedNote')}</p>
    {:else}
      <label>
        <span>{$t('chat.new.address')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input bind:value={address} placeholder="#room:example.org" autofocus />
      </label>
      <p class="note">{$t('chat.new.joinNote')}</p>
    {/if}
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
  </form>

  <svelte:fragment slot="footer">
    <div class="actions">
      <button type="button" class="btn" on:click={() => dispatch('close')}>{$t('chat.new.cancel')}</button>
      <button type="submit" form="new-chat" class="btn primary" disabled={!ready || busy}>
        {#if busy}<PieProgress label={$t('chat.new.working')} />{/if}
        {mode === 'direct' ? $t('chat.new.start') : mode === 'room' ? $t('chat.new.create') : $t('chat.new.joinAction')}
      </button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .tabs {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--space-1);
    margin-bottom: var(--space-4);
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
  }
  .tabs button {
    height: calc(var(--control-height) - var(--space-2));
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-body);
    cursor: var(--cursor-action);
  }
  .tabs button.active {
    background: var(--surface-raised);
    color: var(--text-primary);
    box-shadow: inset 0 0 0 var(--hairline) var(--border-default);
  }

  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }
  label.check {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-primary);
    font-size: var(--fz-body);
  }
  input:not([type='checkbox']) {
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
  }
  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .profile {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-body);
  }
  .note {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }
  .error {
    margin: 0;
    color: var(--danger);
    font-size: var(--fz-meta);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
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
  .btn:disabled {
    opacity: 0.55;
    cursor: default;
  }
</style>
