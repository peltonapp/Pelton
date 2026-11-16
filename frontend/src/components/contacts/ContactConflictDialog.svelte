<script lang="ts">
  // the conflict dialog (#168): the contact changed on the server since the
  // copy being saved was fetched, so the write was refused and nothing has
  // changed anywhere. Both versions are shown and the user picks which one
  // survives. Nothing is decided for them: either choice loses something.
  import Modal from '../common/Modal.svelte'
  import { dismissConflict, saveContact, loadContacts } from '../../stores/contacts'
  import { t } from '../../lib/i18n'
  import type { Contact, ContactConflict } from '../../lib/types'

  /** The refused write, with both versions in it. */
  export let conflict: ContactConflict

  let busy = false

  // keeping the server's version means throwing away what was typed here, so
  // the local copy is refreshed from the server and the dialog closes.
  async function keepServer(): Promise<void> {
    busy = true
    await loadContacts()
    busy = false
    dismissConflict()
  }

  // keeping mine writes it again with no precondition, over whatever is there.
  async function keepMine(): Promise<void> {
    busy = true
    const saved = await saveContact({
      id: conflict.mine.id,
      bookId: conflict.mine.bookId,
      fullName: conflict.mine.fullName,
      organization: conflict.mine.organization,
      title: conflict.mine.title,
      note: conflict.mine.note,
      emails: conflict.mine.emails,
      phones: conflict.mine.phones,
      matrixId: conflict.mine.matrixId,
      force: true,
    })
    busy = false
    if (saved) {
      dismissConflict()
    }
  }

  function lines(contact: Contact): string[] {
    const out = [contact.fullName]
    if (contact.organization || contact.title) {
      out.push([contact.title, contact.organization].filter(Boolean).join(' · '))
    }
    for (const email of contact.emails) {
      out.push(email.value)
    }
    for (const phone of contact.phones) {
      out.push(phone.value)
    }
    if (contact.note) {
      out.push(contact.note)
    }
    return out.filter((line) => line !== '')
  }
</script>

<Modal title={$t('contacts.conflict.title')} hint={$t('contacts.conflict.hint')} size="large" on:close={dismissConflict}>
  <div class="sides">
    <section>
      <h4>{$t('contacts.conflict.server')}</h4>
      <ul>
        {#each lines(conflict.server) as line (line)}
          <li>{line}</li>
        {/each}
      </ul>
      <button type="button" class="ghost" disabled={busy} on:click={keepServer}>
        {$t('contacts.conflict.keepServer')}
      </button>
    </section>
    <section>
      <h4>{$t('contacts.conflict.mine')}</h4>
      <ul>
        {#each lines(conflict.mine) as line (line)}
          <li>{line}</li>
        {/each}
      </ul>
      <button type="button" class="primary" disabled={busy} on:click={keepMine}>
        {$t('contacts.conflict.keepMine')}
      </button>
    </section>
  </div>
</Modal>

<style>
  .sides {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-4);
    width: 100%;
  }

  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
  }

  h4 {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-secondary);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-body);
  }

  li {
    overflow-wrap: anywhere;
  }

  button {
    margin-top: auto;
    height: var(--control-height);
    padding: 0 var(--space-3);
    white-space: nowrap;
    border-radius: var(--radius-control);
    border: var(--hairline) solid var(--border-default);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  button.primary {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }

  button:disabled {
    opacity: 0.5;
    cursor: default;
  }
</style>
