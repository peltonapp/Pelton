<script lang="ts">
  // the sender panel beside the reader in the new design: who the open
  // message is from, all from what is stored here. A contact shows its
  // address book details and can be edited; anyone else can be added. Below
  // that, recent conversations and files from them, and quick actions. A
  // mailing list or a machine sender says so instead of passing for a person.
  import { createEventDispatcher } from 'svelte'
  import { slide } from 'svelte/transition'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import writeIcon from '@phosphor-icons/core/assets/duotone/paper-plane-tilt-duotone.svg?raw'
  import searchIcon from '@phosphor-icons/core/assets/duotone/magnifying-glass-duotone.svg?raw'
  import viewIcon from '@phosphor-icons/core/assets/duotone/bookmark-simple-duotone.svg?raw'
  import addIcon from '@phosphor-icons/core/assets/duotone/user-plus-duotone.svg?raw'
  import editIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import listIcon from '@phosphor-icons/core/assets/duotone/megaphone-duotone.svg?raw'
  import robotIcon from '@phosphor-icons/core/assets/duotone/robot-duotone.svg?raw'
  import orgIcon from '@phosphor-icons/core/assets/duotone/buildings-duotone.svg?raw'
  import atIcon from '@phosphor-icons/core/assets/duotone/at-duotone.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import ContactEditor from '../contacts/ContactEditor.svelte'
  import ContactConflictDialog from '../contacts/ContactConflictDialog.svelte'
  import { kindIcon, asAttachment } from '../attachments/kinds'
  import { unsubscribeMessage } from '../../lib/api'
  import { openURL } from '../../lib/browser'
  import { senderSections } from '../../lib/sendersections'
  import { formatBytes, formatListDate, type TimeFormat } from '../../lib/format'
  import { dur, motion } from '../../lib/motion'
  import { allContacts, contactFor, loadAllContacts, overviewFor } from '../../stores/sender'
  import { contactSeed, contactConflict, loadContacts } from '../../stores/contacts'
  import { vipSenders, bareAddress, addVIP, removeVIP } from '../../stores/vip'
  import { openSpace } from '../../stores/space'
  import { openMessage } from '../../stores/selection'
  import { openPreview } from '../../stores/preview'
  import { openAttachments, closeAttachments } from '../../stores/attachments'
  import { openComposeWith } from '../../stores/compose'
  import { openViewEditor } from '../../stores/views'
  import { setDraft, submitSearch } from '../../stores/search'
  import { prefs } from '../../stores/prefs'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { ContactDraft, EditorMode, MessageDetail, SenderOverview } from '../../lib/types'

  /** The open message, whose sender the panel is about. */
  export let detail: MessageDetail

  const dispatch = createEventDispatcher<{ close: void }>()

  $: address = bareAddress(detail.fromAddress)
  $: contact = contactFor(address, $allContacts)
  $: name = contact?.fullName || detail.fromName || address
  $: otherAddresses = contact ? contact.emails.map((e) => e.value).filter((v) => v.trim().toLowerCase() !== address) : []
  $: vip = $vipSenders.has(address)
  $: isList = detail.unsubscribe !== null

  void loadAllContacts()

  let overview: SenderOverview | null = null
  let overviewError = ''
  let loadedFor = ''

  $: if (address !== loadedFor) {
    loadedFor = address
    void load(address)
  }

  async function load(addr: string): Promise<void> {
    overview = null
    overviewError = ''
    if (!addr) {
      return
    }
    try {
      const o = await overviewFor(addr)
      if (addr === loadedFor) {
        overview = o
      }
    } catch (err) {
      if (addr === loadedFor) {
        overviewError = errorMessage(err)
      }
    }
  }

  $: automated = !isList && (overview?.automated ?? false)
  $: since = overview?.first ? new Date(overview.first).toLocaleDateString(undefined, { year: 'numeric', month: 'short' }) : ''

  async function toggleVIP(): Promise<void> {
    try {
      if (vip) {
        await removeVIP(address)
      } else {
        await addVIP(address)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function addContact(): void {
    contactSeed.set({ name: detail.fromName, email: address })
    openSpace('contacts')
  }

  let editing: ContactDraft | null = null

  async function editContact(): Promise<void> {
    if (!contact) {
      return
    }
    // the editor picks the address book from this list.
    await loadContacts()
    editing = {
      id: contact.id,
      bookId: contact.bookId,
      fullName: contact.fullName,
      organization: contact.organization,
      title: contact.title,
      note: contact.note,
      emails: contact.emails.map((e) => ({ ...e })),
      phones: contact.phones.map((p) => ({ ...p })),
      force: false,
    }
  }

  function editorClosed(): void {
    editing = null
    void loadAllContacts(true)
  }

  function writeTo(to: string): void {
    openComposeWith(detail.accountId, $prefs.defaultEditorMode as EditorMode, { to, cc: '', bcc: '', subject: '', body: '' })
  }

  function searchFrom(): void {
    closeAttachments()
    setDraft({ text: '', chips: [{ field: 'from', value: address }] })
    submitSearch()
  }

  function saveView(): void {
    openViewEditor({ name, queryFrom: [address] })
  }

  let unsubscribing = false
  let unsubscribed = false

  $: if (address) {
    unsubscribed = false
  }

  async function unsubscribe(): Promise<void> {
    const unsub = detail.unsubscribe
    if (!unsub || unsubscribing) {
      return
    }
    if (unsub.kind === 'link') {
      openURL(unsub.target)
      return
    }
    unsubscribing = true
    try {
      await unsubscribeMessage(detail.id)
      unsubscribed = true
      toastSuccess($t('detail.unsubscribe.done'))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      unsubscribing = false
    }
  }

  $: listName = detail.fromName || address.split('@')[1] || address
</script>

<aside class="panel" aria-label={$t('senderPanel.title')} transition:slide={{ axis: 'x', duration: dur(motion.base) }}>
  <header class="top">
    <span class="eyebrow">{$t('senderPanel.title')}</span>
    <button type="button" class="icon-btn" aria-label={$t('senderPanel.hide')} title={$t('senderPanel.hide')} on:click={() => dispatch('close')}>
      <Ph svg={closeIcon} size={14} />
    </button>
  </header>

  <section class="who">
    {#if $prefs.avatarPlacement !== 'hidden' && !isList && !automated}
      <Avatar name={name} email={address} size={48} />
    {:else if isList || automated}
      <span class="kind-glyph" aria-hidden="true"><Ph svg={isList ? listIcon : robotIcon} size={24} /></span>
    {/if}
    <div class="names">
      <h2 dir="auto">{isList ? listName : name}</h2>
      <span class="addr" dir="ltr">{address}</span>
      {#if isList}
        <span class="tag">{$t('senderPanel.mailingList')}</span>
      {:else if automated}
        <span class="tag">{$t('senderPanel.automated')}</span>
      {/if}
    </div>
    <button
      type="button"
      class="icon-btn star"
      class:on={vip}
      aria-pressed={vip}
      aria-label={vip ? $t('senderPanel.vipRemove') : $t('senderPanel.vipAdd')}
      title={vip ? $t('senderPanel.vipRemove') : $t('senderPanel.vipAdd')}
      on:click={toggleVIP}
    >
      <Ph svg={starIcon} size={16} />
    </button>
  </section>

  {#if contact && (contact.organization || contact.title)}
    <p class="line"><Ph svg={orgIcon} size={14} /><span dir="auto">{[contact.title, contact.organization].filter(Boolean).join(', ')}</span></p>
  {/if}

  {#if overview && overview.count > 0}
    <p class="stats">
      {$t('senderPanel.stats').replace('{count}', overview.count.toLocaleString()).replace('{since}', since)}
    </p>
  {/if}

  {#if isList && detail.unsubscribe}
    <section class="block">
      <p class="note">{$t('senderPanel.listNote')}</p>
      {#if detail.unsubscribe.done || unsubscribed}
        <p class="note">{$t('senderPanel.unsubscribed')}</p>
      {:else}
        <button type="button" class="btn" disabled={unsubscribing} on:click={unsubscribe}>
          {$t('senderPanel.unsubscribe')}
        </button>
      {/if}
    </section>
  {/if}

  <section class="actions" aria-label={$t('senderPanel.actions')}>
    {#if !isList && !automated}
      <button type="button" class="btn primary" on:click={() => writeTo(address)}>
        <Ph svg={writeIcon} size={14} />{$t('senderPanel.write')}
      </button>
    {/if}
    <button type="button" class="btn" on:click={searchFrom}>
      <Ph svg={searchIcon} size={14} />{$t('senderPanel.searchFrom')}
    </button>
    <button type="button" class="btn" on:click={saveView}>
      <Ph svg={viewIcon} size={14} />{$t('senderPanel.saveView')}
    </button>
    {#if !isList && !automated}
      {#if contact}
        {#if !contact.readOnly}
          <button type="button" class="btn" on:click={editContact}>
            <Ph svg={editIcon} size={14} />{$t('senderPanel.editContact')}
          </button>
        {/if}
      {:else}
        <button type="button" class="btn" on:click={addContact}>
          <Ph svg={addIcon} size={14} />{$t('senderPanel.addContact')}
        </button>
      {/if}
    {/if}
  </section>

  {#if otherAddresses.length > 0}
    <section class="block">
      <h3>{$t('senderPanel.otherAddresses')}</h3>
      <ul class="plain">
        {#each otherAddresses as other (other)}
          <li>
            <button type="button" class="link-row" title={$t('senderPanel.writeTo').replace('{address}', other)} on:click={() => writeTo(other)}>
              <Ph svg={atIcon} size={13} /><span dir="ltr">{other}</span>
            </button>
          </li>
        {/each}
      </ul>
    </section>
  {/if}

  {#if overviewError}
    <p class="note" role="alert">{overviewError}</p>
  {:else if overview}
    {#if overview.recent.length > 0}
      <section class="block">
        <h3>{$t('senderPanel.recent')}</h3>
        <ul class="plain">
          {#each overview.recent as m (m.id)}
            <li>
              <button type="button" class="conv" class:current={m.id === detail.id} on:click={() => openMessage(m.id)}>
                <span class="conv-subject" dir="auto">{m.subject || $t('detail.noSubject')}</span>
                <span class="conv-date">{formatListDate(m.date, $prefs.timeFormat as TimeFormat)}</span>
              </button>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if overview.files.length > 0}
      <section class="block">
        <h3>{$t('senderPanel.files')}</h3>
        <ul class="plain">
          {#each overview.files as f (f.id)}
            <li>
              <button type="button" class="file" on:click={() => openPreview(f.messageId, asAttachment(f))}>
                <Ph svg={kindIcon[f.kind] ?? kindIcon.other} size={15} />
                <span class="file-name" dir="auto" title={f.filename}>{f.filename}</span>
                <span class="file-size">{formatBytes(f.sizeBytes)}</span>
              </button>
            </li>
          {/each}
        </ul>
        {#if overview.filesTotal > overview.files.length}
          <button type="button" class="more" on:click={() => openAttachments({ sender: address })}>
            {$t('senderPanel.allFiles').replace('{count}', overview.filesTotal.toLocaleString())}
          </button>
        {/if}
      </section>
    {/if}
  {/if}

  <!-- extension point: sections other parts of the app add, such as the
       calendar's upcoming events shared with this sender. Nothing renders
       here until one registers through lib/sendersections.ts. -->
  {#each $senderSections as section (section.id)}
    <svelte:component this={section.component} {address} name={contact?.fullName || detail.fromName} />
  {/each}
</aside>

{#if editing}
  <ContactEditor draft={editing} on:close={editorClosed} />
  {#if $contactConflict}
    <ContactConflictDialog conflict={$contactConflict} />
  {/if}
{/if}

<style>
  .panel {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    width: var(--sender-panel-width);
    flex-shrink: 0;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-4) var(--pane-pad);
    border-inline-start: var(--hairline) solid var(--border-subtle);
    background: var(--surface-raised);
    font-size: var(--fz-label);
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .eyebrow {
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .who {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: var(--space-3);
  }

  .kind-glyph {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--sender-avatar);
    height: var(--sender-avatar);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
  }

  .names {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  h2 {
    margin: 0;
    font-size: var(--fz-body);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .addr {
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-align: start;
  }

  .tag {
    align-self: flex-start;
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .icon-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .icon-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .star.on {
    color: var(--warning);
  }

  .line {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    color: var(--text-secondary);
  }

  .stats,
  .note {
    margin: 0;
    color: var(--text-tertiary);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    height: calc(var(--control-height) - var(--space-1));
    padding-inline: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    font: inherit;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .btn:hover:not(:disabled) {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .btn:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .btn.primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
  }

  .block {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  h3 {
    margin: 0;
    font-size: var(--fz-meta);
    font-weight: var(--fw-semibold);
    color: var(--text-tertiary);
  }

  .plain {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .conv,
  .file,
  .link-row {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .conv:hover,
  .file:hover,
  .link-row:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .conv.current {
    background: var(--selection-bg);
  }

  .conv-subject,
  .file-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .conv-date,
  .file-size {
    flex-shrink: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    font-variant-numeric: tabular-nums;
  }

  .file :global(.ph) {
    color: var(--accent);
  }

  .more {
    align-self: flex-start;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font: inherit;
    cursor: var(--cursor-action);
  }

  .more:hover {
    text-decoration: underline;
  }
</style>
