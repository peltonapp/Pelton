<script lang="ts">
  // the middle pane of the contacts space: the contacts under alphabetical (or
  // organization) headers, with a selection band that slides to the open one.
  import { createEventDispatcher, tick } from 'svelte'
  import { openScriptMenu } from '../../lib/scriptmenu'
  import ContactListEmpty from './ContactListEmpty.svelte'
  import ContactAvatar from './ContactAvatar.svelte'
  import ContactFilterBar from './ContactFilterBar.svelte'
  import { contactSort, selectedContactId, visibleContacts } from '../../stores/contactsview'
  import { displayName, sectionContacts } from '../../lib/contactlist'
  import { t } from '../../lib/i18n'

  const dispatch = createEventDispatcher<{ select: number; create: void }>()

  let scroller: HTMLElement | undefined
  // snap places the band without sliding it, for the first selection after none.
  let band = { top: 0, height: 0, shown: false, snap: false }

  $: sections = sectionContacts($visibleContacts, $contactSort, $t('contacts.sort.noOrganization'))
  $: tabbable = $visibleContacts.some((c) => c.id === $selectedContactId) ? $selectedContactId : ($visibleContacts[0]?.id ?? null)
  $: void placeBand(sections, $selectedContactId)

  async function placeBand(_sections: unknown, id: number | null): Promise<void> {
    await tick()
    const row = id === null ? null : scroller?.querySelector<HTMLElement>(`[data-contact="${id}"]`)
    band = row
      ? { top: row.offsetTop, height: row.offsetHeight, shown: true, snap: !band.shown }
      : { ...band, shown: false, snap: false }
  }

  function onKeydown(event: KeyboardEvent): void {
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (step === 0 || $visibleContacts.length === 0) {
      return
    }
    event.preventDefault()
    const from = $visibleContacts.findIndex((c) => c.id === tabbable)
    const next = $visibleContacts[Math.min(Math.max(from + step, 0), $visibleContacts.length - 1)]
    dispatch('select', next.id)
    void tick().then(() => scroller?.querySelector<HTMLElement>(`[data-contact="${next.id}"] button`)?.focus())
  }

  function subline(contact: { organization: string; emails: { value: string }[] }): string {
    return contact.organization.trim() || contact.emails[0]?.value || ''
  }
</script>

<div class="list">
  <ContactFilterBar />
  <div class="scroller" bind:this={scroller}>
    {#if $visibleContacts.length === 0}
      <ContactListEmpty on:create={() => dispatch('create')} />
    {:else}
      <div class="rows" on:keydown={onKeydown} role="presentation">
        <div
          class="band"
          class:shown={band.shown}
          class:snap={band.snap}
          style={`transform: translateY(${band.top}px); height: ${band.height}px`}
          aria-hidden="true"
        ></div>
        {#each sections as section (section.key)}
          <section>
            {#if section.label}
              <h3 class="section">{section.label}</h3>
            {/if}
            <ul>
              {#each section.contacts as contact (contact.id)}
                <li data-contact={contact.id}>
                  <button
                    type="button"
                    class="row"
                    tabindex={contact.id === tabbable ? 0 : -1}
                    aria-current={contact.id === $selectedContactId ? 'true' : undefined}
                    on:click={() => dispatch('select', contact.id)}
                    on:contextmenu={(e) => openScriptMenu(e, 'contacts.contact', { id: String(contact.id) })}
                  >
                    <ContactAvatar {contact} size={28} />
                    <span class="text">
                      <span class="name">{displayName(contact)}</span>
                      {#if subline(contact) && subline(contact) !== displayName(contact)}
                        <span class="sub">{subline(contact)}</span>
                      {/if}
                    </span>
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
  .list {
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

  .rows {
    position: relative;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .section {
    position: sticky;
    top: 0;
    z-index: 2;
    margin: 0;
    padding: var(--space-1) var(--row-pad-x);
    border-bottom: var(--hairline) solid var(--border-subtle);
    background: var(--surface-sunken);
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    width: 100%;
    padding: var(--row-pad-y) var(--row-pad-x);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
    transition: background-color var(--motion-fast) var(--ease-out);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row:focus-visible {
    outline: var(--hairline) solid var(--accent);
    outline-offset: calc(var(--hairline) * -1);
  }

  .text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fz-list);
  }

  .sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
  }

  /* lies over the rows as a tint, so it can travel between them. */
  .band {
    position: absolute;
    top: 0;
    inset-inline: 0;
    z-index: 1;
    background: var(--selection-bg);
    opacity: 0;
    pointer-events: none;
    transition:
      transform var(--motion-base) var(--ease-out),
      height var(--motion-fast) var(--ease-out),
      opacity var(--motion-fast) var(--ease-out);
  }

  .band.shown {
    opacity: 1;
  }

  .band.snap {
    transition: opacity var(--motion-fast) var(--ease-out);
  }

  @media (prefers-reduced-motion: reduce) {
    .band,
    .row {
      transition: none;
    }
  }
</style>
