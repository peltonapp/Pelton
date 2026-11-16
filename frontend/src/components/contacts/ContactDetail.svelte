<script lang="ts">
  // the contact in the right pane (or the drawer of the grid): who it is, how to
  // reach them, what is written on the card, and the latest mail with them.
  import { createEventDispatcher } from 'svelte'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import Ph from '../common/Ph.svelte'
  import ContactAvatar from './ContactAvatar.svelte'
  import envelopeIcon from '@phosphor-icons/core/assets/duotone/envelope-simple-duotone.svg?raw'
  import phoneIcon from '@phosphor-icons/core/assets/duotone/phone-duotone.svg?raw'
  import noteIcon from '@phosphor-icons/core/assets/duotone/note-duotone.svg?raw'
  import pencilIcon from '@phosphor-icons/core/assets/duotone/pencil-simple-duotone.svg?raw'
  import trashIcon from '@phosphor-icons/core/assets/duotone/trash-duotone.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/regular/arrow-left.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { removeContact } from '../../stores/contacts'
  import { prefs } from '../../stores/prefs'
  import { displayName } from '../../lib/contactlist'
  import { recentMailWith } from '../../lib/contactmail'
  import { formatListDate, formatRelative, type TimeFormat } from '../../lib/format'
  import { dur, motion } from '../../lib/motion'
  import ContactTasks from '../tasks/ContactTasks.svelte'
  import NoteMentions from '../notes/NoteMentions.svelte'
  import { t } from '../../lib/i18n'
  import type { Contact, MessageSummary } from '../../lib/types'

  /** The contact to show. */
  export let contact: Contact
  /** Adds a way out above the contact: a back arrow, a close button, or nothing. */
  export let dismiss: 'back' | 'close' | null = null

  const dispatch = createEventDispatcher<{ compose: string; edit: Contact; dismiss: void; openMail: MessageSummary }>()

  let confirming = false
  let recent: MessageSummary[] = []
  let recentLoaded = false
  let lastId = -1

  $: if (contact.id !== lastId) {
    lastId = contact.id
    confirming = false
    recent = []
    recentLoaded = false
    void loadRecent(contact)
  }

  async function loadRecent(c: Contact): Promise<void> {
    const found = await recentMailWith(
      c.emails.map((e) => e.value),
      5,
    )
    if (c.id === lastId) {
      recent = found
      recentLoaded = true
    }
  }

  async function confirmDelete(): Promise<void> {
    confirming = false
    await removeContact(contact.id)
  }

  $: firstEmail = contact.emails.find((e) => e.value.trim() !== '')?.value ?? ''
  $: subtitle = [contact.title, contact.organization].filter(Boolean).join(' · ')
</script>

<article class="detail" aria-label={displayName(contact)}>
  {#if dismiss}
    <div class="top">
      <button
        type="button"
        class="icon"
        aria-label={dismiss === 'back' ? $t('contacts.back') : $t('action.close')}
        on:click={() => dispatch('dismiss')}
      >
        <Ph svg={dismiss === 'back' ? backIcon : closeIcon} size={16} />
      </button>
    </div>
  {/if}

  {#key contact.id}
    <div class="body" in:fly={{ y: 6, duration: dur(motion.base), easing: cubicOut }}>
      <header class="head">
        <ContactAvatar {contact} size={56} />
        <div class="who">
          <h2>{displayName(contact)}</h2>
          {#if subtitle}
            <p class="sub">{subtitle}</p>
          {/if}
          <p class="sub">{contact.bookName || $t('contacts.book.untitled')}</p>
        </div>
      </header>

      <div class="actions">
        {#if firstEmail}
          <button type="button" class="btn primary" on:click={() => dispatch('compose', firstEmail)}>
            <Ph svg={envelopeIcon} size={16} />
            {$t('contacts.action.write')}
          </button>
        {/if}
        {#if !contact.readOnly}
          <button type="button" class="btn" on:click={() => dispatch('edit', contact)}>
            <Ph svg={pencilIcon} size={16} />
            {$t('action.edit')}
          </button>
          <button type="button" class="btn danger" aria-expanded={confirming} on:click={() => (confirming = !confirming)}>
            <Ph svg={trashIcon} size={16} />
            {$t('action.delete')}
          </button>
        {/if}
      </div>

      {#if confirming}
        <div class="confirm" role="alert" in:fly={{ y: -4, duration: dur(motion.fast), easing: cubicOut }}>
          <span>{$t('contacts.deleteConfirm')}</span>
          <span class="confirm-actions">
            <button type="button" class="btn" on:click={() => (confirming = false)}>{$t('mailboxes.cancel')}</button>
            <button type="button" class="btn solid-danger" on:click={confirmDelete}>{$t('action.delete')}</button>
          </span>
        </div>
      {/if}

      {#if contact.emails.length > 0}
        <section>
          <h3><Ph svg={envelopeIcon} size={14} />{$t('contacts.field.emails')}</h3>
          <ul class="values">
            {#each contact.emails as email, i (i)}
              <li>
                <button type="button" class="link" on:click={() => dispatch('compose', email.value)}>{email.value}</button>
                {#if email.label}<span class="label">{email.label}</span>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if contact.phones.length > 0}
        <section>
          <h3><Ph svg={phoneIcon} size={14} />{$t('contacts.field.phones')}</h3>
          <ul class="values">
            {#each contact.phones as phone, i (i)}
              <li>
                <span>{phone.value}</span>
                {#if phone.label}<span class="label">{phone.label}</span>{/if}
              </li>
            {/each}
          </ul>
        </section>
      {/if}

      {#if contact.note}
        <section>
          <h3><Ph svg={noteIcon} size={14} />{$t('contacts.field.note')}</h3>
          <p class="note">{contact.note}</p>
        </section>
      {/if}

      {#if contact.uid || contact.emails.length > 0}
        <section class="vault-notes">
          <NoteMentions about={{ kind: 'contact', id: contact.uid || contact.emails[0].value, title: contact.fullName || contact.emails[0]?.value || '' }} />
        </section>
      {/if}

      {#if contact.emails.length > 0}
        <ContactTasks name={contact.fullName} emails={contact.emails.map((e) => e.value)} />
      {/if}

      {#if firstEmail}
        <section>
          <h3><Ph svg={envelopeIcon} size={14} />{$t('contacts.recent')}</h3>
          {#if recent.length > 0}
            <ul class="mail">
              {#each recent as message (message.id)}
                <li>
                  <button type="button" class="mail-row" on:click={() => dispatch('openMail', message)}>
                    <span class="subject">{message.subject || $t('contacts.recent.noSubject')}</span>
                    <span class="when">{formatListDate(message.date, $prefs.timeFormat as TimeFormat)}</span>
                  </button>
                </li>
              {/each}
            </ul>
          {:else if recentLoaded}
            <p class="sub">{$t('contacts.recent.none')}</p>
          {/if}
        </section>
      {/if}

      <footer class="meta">
        {#if contact.extra.length > 0}
          <p>{$t('contacts.extraFields').replace('{fields}', contact.extra.join(', '))}</p>
        {/if}
        {#if contact.updated}
          <p>{$t('contacts.updated').replace('{when}', formatRelative(Date.parse(contact.updated), $t))}</p>
        {/if}
        {#if contact.readOnly}
          <p>{$t('contacts.readOnly')}</p>
        {/if}
      </footer>
    </div>
  {/key}
</article>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    background: var(--surface-raised);
    color: var(--text-primary);
  }

  .top {
    display: flex;
    padding: var(--space-2) var(--space-3) 0;
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: var(--space-4);
  }

  .head {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }

  .who {
    min-width: 0;
  }

  h2 {
    margin: 0;
    overflow-wrap: anywhere;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
  }

  .sub {
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
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
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .btn:hover {
    background: var(--surface-hover);
  }

  .btn:active {
    transform: scale(0.98);
  }

  .btn.primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover {
    background: var(--accent);
    filter: brightness(0.94);
  }

  .btn.danger {
    color: var(--danger);
  }

  .btn.solid-danger {
    border-color: transparent;
    background: var(--danger);
    color: var(--text-inverse);
  }

  .btn.solid-danger:hover {
    background: var(--danger);
    filter: brightness(0.94);
  }

  .confirm {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--danger);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
    font-size: var(--fz-label);
  }

  .confirm-actions {
    display: flex;
    gap: var(--space-2);
  }

  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-top: var(--space-3);
    border-top: var(--hairline) solid var(--border-subtle);
  }

  h3 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .values {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .values li {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    font-size: var(--fz-body);
    overflow-wrap: anywhere;
  }

  .label {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .link {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .note {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-body);
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }

  .mail {
    display: flex;
    flex-direction: column;
  }

  .mail-row {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    width: 100%;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-body);
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .mail-row:hover {
    background: var(--surface-hover);
  }

  .subject {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .when {
    flex-shrink: 0;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .meta {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .meta p {
    margin: 0;
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
    color: var(--text-primary);
  }

  @media (prefers-reduced-motion: reduce) {
    .btn,
    .mail-row {
      transition: none;
    }
  }
</style>
