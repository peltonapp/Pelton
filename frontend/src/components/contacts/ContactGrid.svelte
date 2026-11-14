<script lang="ts">
  // the card grid: the alternative to the list and detail panes. One card per
  // contact under the same headers as the list; a card opens the contact in the
  // drawer the space slides in.
  import { createEventDispatcher } from 'svelte'
  import Spinner from '../common/Spinner.svelte'
  import ContactAvatar from './ContactAvatar.svelte'
  import ContactFilterBar from './ContactFilterBar.svelte'
  import { contacts, contactsLoading } from '../../stores/contacts'
  import { contactQuery, contactSort, selectedContactId, visibleContacts } from '../../stores/contactsview'
  import { displayName, sectionContacts } from '../../lib/contactlist'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ select: number }>()

  $: sections = sectionContacts($visibleContacts, $contactSort, $t('contacts.sort.noOrganization'))
</script>

<div class="grid-pane">
  <ContactFilterBar />
  <div class="scroller">
    {#if $contactsLoading && $contacts.length === 0}
      <div class="note"><Spinner /></div>
    {:else if $visibleContacts.length === 0}
      <p class="note">{$contactQuery.trim() ? $t('contacts.noMatches') : $t('contacts.emptyBook')}</p>
    {:else}
      <div class="sections">
        {#each sections as section (section.key)}
          <section>
            {#if section.label}
              <h3 class="section">{section.label}</h3>
            {/if}
            <ul class="grid">
              {#each section.contacts as contact (contact.id)}
                <li>
                  <button
                    type="button"
                    class="card"
                    aria-current={contact.id === $selectedContactId ? 'true' : undefined}
                    on:click={() => dispatch('select', contact.id)}
                  >
                    <ContactAvatar {contact} size={40} />
                    <span class="name">{displayName(contact)}</span>
                    {#if contact.title || contact.organization}
                      <span class="sub">{[contact.title, contact.organization].filter(Boolean).join(' · ')}</span>
                    {/if}
                    {#if contact.emails[0] && contact.emails[0].value !== displayName(contact)}
                      <span class="line">{contact.emails[0].value}</span>
                    {/if}
                    {#if contact.phones[0]}
                      <span class="line">{contact.phones[0].value}</span>
                    {/if}
                  </button>
                </li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .grid-pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--surface-raised);
  }

  .scroller {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .sections {
    padding: var(--space-3);
    animation: settle var(--motion-base) var(--ease-out);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .section {
    margin: var(--space-3) 0 var(--space-2);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
  }

  section:first-child .section {
    margin-top: 0;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: var(--space-3);
  }

  .card {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
    width: 100%;
    height: 100%;
    padding: var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      background-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .card:hover {
    border-color: var(--border-strong);
    background: var(--surface-hover);
  }

  .card:active {
    transform: scale(0.985);
  }

  .card[aria-current='true'] {
    border-color: var(--accent);
    background: var(--selection-bg);
  }

  .card:focus-visible {
    outline: var(--hairline) solid var(--accent);
    outline-offset: var(--hairline);
  }

  .card :global(.avatar) {
    margin-bottom: var(--space-1);
  }

  .name {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  .sub,
  .line {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  .line {
    color: var(--text-secondary);
  }

  .note {
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 0;
    padding: var(--space-5);
    color: var(--text-tertiary);
    font-size: var(--fz-body);
    text-align: center;
  }

  @keyframes settle {
    from {
      opacity: 0;
      transform: translateY(var(--space-1));
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .sections {
      animation: none;
    }

    .card {
      transition: none;
    }
  }
</style>
