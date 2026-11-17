<script lang="ts">
  // the people a project is with: contacts from the address book or plain
  // addresses. mail from any of them is what the reader offers to file under
  // the project. a person opens in Contacts, or can be written to with the
  // project already picked in the composer.
  import { createEventDispatcher, onMount } from 'svelte'
  import { flip } from 'svelte/animate'
  import { fade, fly } from 'svelte/transition'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import userPlusIcon from '@phosphor-icons/core/assets/duotone/user-plus-duotone.svg?raw'
  import usersIcon from '@phosphor-icons/core/assets/duotone/users-three-duotone.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import Avatar from '../common/Avatar.svelte'
  import EmptyState from '../common/EmptyState.svelte'
  import { searchAddresses } from '../../lib/api'
  import { showPerson } from '../../lib/projectlinks'
  import { addPerson, removePerson } from '../../stores/projects'
  import { contacts, loadContacts } from '../../stores/contacts'
  import { prefs } from '../../stores/prefs'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import type { AddressBookEntry, Contact, Project } from '../../lib/types'

  /** The project whose people these are. */
  export let project: Project

  const dispatch = createEventDispatcher<{ compose: string }>()

  let query = ''
  let suggestions: AddressBookEntry[] = []
  let highlight = -1
  let debounce: ReturnType<typeof setTimeout> | undefined
  let seq = 0

  onMount(() => {
    if ($contacts.length === 0) {
      void loadContacts()
    }
  })

  $: byAddress = new Map<string, Contact>(
    $contacts.flatMap((c) => c.emails.map((e) => [e.value.toLowerCase(), c] as [string, Contact])),
  )
  $: taken = new Set(project.people.map((p) => p.email.toLowerCase()))

  function onInput(): void {
    clearTimeout(debounce)
    const q = query.trim()
    if (!q) {
      suggestions = []
      return
    }
    debounce = setTimeout(async () => {
      const mine = ++seq
      try {
        const found = await searchAddresses(q, 8)
        if (mine === seq && query.trim() === q) {
          suggestions = found.filter((e) => !taken.has(e.email.toLowerCase()))
          highlight = suggestions.length ? 0 : -1
        }
      } catch {
        suggestions = []
      }
    }, 140)
  }

  function typed(): { name: string; email: string } {
    const text = query.trim()
    const m = text.match(/^(.*)<([^>]+)>$/)
    if (m) {
      return { name: m[1].trim().replace(/^"|"$/g, ''), email: m[2].trim() }
    }
    return { name: '', email: text }
  }

  async function add(person: { name: string; email: string }): Promise<void> {
    if (!person.email.includes('@')) {
      return
    }
    query = ''
    suggestions = []
    seq++
    const contact = byAddress.get(person.email.toLowerCase())
    await addPerson(project.id, person.email, person.name || contact?.fullName || '')
  }

  function onKey(event: KeyboardEvent): void {
    if (suggestions.length && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      highlight = (highlight + (event.key === 'ArrowDown' ? 1 : -1) + suggestions.length) % suggestions.length
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const pick = suggestions[highlight]
      void add(pick ? { name: pick.name, email: pick.email } : typed())
    } else if (event.key === 'Escape' && (suggestions.length || query)) {
      event.preventDefault()
      event.stopPropagation()
      suggestions = []
      query = ''
    }
  }
</script>

<section class="people" aria-label={$t('projects.tab.people')}>
  <div class="add">
    <span class="icon" aria-hidden="true"><Ph svg={userPlusIcon} size={16} /></span>
    <div class="field">
      <input
        class="cal-field"
        bind:value={query}
        placeholder={$t('projects.people.addPlaceholder')}
        aria-label={$t('projects.people.add')}
        aria-autocomplete="list"
        on:input={onInput}
        on:keydown={onKey}
        on:blur={() => setTimeout(() => (suggestions = []), 150)}
      />
      {#if suggestions.length > 0}
        <ul class="suggest" role="listbox" aria-label={$t('projects.people.add')} transition:fly={{ y: -4, duration: dur(motion.fast) }}>
          {#each suggestions as s, i (s.email)}
            <li role="option" aria-selected={i === highlight}>
              <button type="button" class:on={i === highlight} on:mousedown|preventDefault={() => void add({ name: s.name, email: s.email })}>
                <Avatar name={s.name} email={s.email} size={20} />
                <span class="s-name">{s.name || s.email}</span>
                {#if s.name}<span class="s-mail">{s.email}</span>{/if}
              </button>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
  <p class="lead">{$t('projects.people.lead')}</p>

  {#if project.people.length === 0}
    <EmptyState icon={usersIcon} title={$t('projects.people.emptyTitle')} detail={$t('projects.people.emptyDetail')} />
  {:else}
    <ul class="list" aria-label={$t('projects.tab.people')}>
      {#each project.people as person (person.email)}
        {@const contact = byAddress.get(person.email.toLowerCase())}
        <li class="person" animate:flip={{ duration: dur(motion.base) }} in:fade={{ duration: dur(motion.fast) }}>
          <Avatar name={contact?.fullName || person.name} email={person.email} size={$prefs.density === 'compact' ? 28 : 32} />
          <button type="button" class="who" on:click={() => void showPerson(person.email, contact?.fullName || person.name)}>
            <span class="name">{contact?.fullName || person.name || person.email}</span>
            <span class="detail">
              {person.email}{#if contact?.organization}<span class="org"> &middot; {contact.organization}</span>{/if}
            </span>
          </button>
          {#if !contact}
            <span class="tag" title={$t('projects.people.notContactHint')}>{$t('projects.people.notContact')}</span>
          {/if}
          <button type="button" class="cal-btn" on:click={() => dispatch('compose', person.email)}>{$t('projects.people.write')}</button>
          <button type="button" class="cal-icon" aria-label={$t('projects.people.remove').replace('{name}', person.name || person.email)} on:click={() => void removePerson(project.id, person.email)}>
            <Ph svg={closeIcon} size={12} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .people {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    min-width: 0;
    padding: var(--space-4) var(--space-5);
    overflow-y: auto;
  }

  .add {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    max-width: calc(var(--cal-editor-width) * 0.9);
  }

  .icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .field {
    position: relative;
    flex: 1;
    min-width: 0;
  }

  .field .cal-field {
    width: 100%;
  }

  .suggest {
    position: absolute;
    inset-inline: 0;
    top: calc(100% + var(--space-1));
    z-index: 30;
    margin: 0;
    padding: var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    list-style: none;
  }

  .suggest button {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .suggest button.on,
  .suggest button:hover {
    background: var(--surface-hover);
  }

  .s-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .s-mail {
    margin-inline-start: auto;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .lead {
    margin: 0;
    max-width: calc(var(--cal-editor-width) * 0.9);
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .list {
    display: flex;
    flex-direction: column;
    max-width: calc(var(--cal-editor-width) * 1.2);
    margin: 0;
    padding: 0;
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .person {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .person:last-child {
    border-bottom: none;
  }

  .who {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    flex: 1;
    min-width: 0;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    text-align: start;
    cursor: var(--cursor-action);
  }

  .who:hover .name {
    text-decoration: underline;
  }

  .name {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .detail {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tag {
    flex-shrink: 0;
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .person .cal-btn {
    height: calc(var(--control-height) - 6px);
  }
</style>
