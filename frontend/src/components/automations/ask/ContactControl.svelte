<script lang="ts">
  // a person from the address book, found by name or address; the answer is
  // the contact's id.
  import { createEventDispatcher, onMount } from 'svelte'
  import { t } from '../../../lib/i18n'
  import type { ScriptAsk } from '../../../lib/scripting'
  import { contacts, loadContacts } from '../../../stores/contacts'
  import '../../../theme/controls.css'

  /** The question. */
  export let ask: ScriptAsk
  /** The picked contact's id. */
  export let value: unknown = undefined

  const dispatch = createEventDispatcher<{ answer: unknown }>()
  const shown = 50
  let query = ''
  let input: HTMLInputElement

  onMount(() => {
    input?.focus()
    if ($contacts.length === 0) {
      void loadContacts()
    }
  })

  $: q = query.trim().toLowerCase()
  $: matches = $contacts
    .filter((c) => q === '' || c.fullName.toLowerCase().includes(q) || c.emails.some((e) => e.value.toLowerCase().includes(q)))
    .slice(0, shown)
</script>

<div class="contact">
  <input bind:this={input} class="cal-field" type="search" placeholder={$t('scripts.ask.contactSearch')} aria-label={ask.prompt || $t('scripts.ask.answer')} bind:value={query} />
  <ul class="list" role="listbox" aria-label={$t('scripts.ask.answer')}>
    {#each matches as c (c.id)}
      <li role="option" aria-selected={value === c.id}>
        <button type="button" class="row" class:on={value === c.id} on:click={() => (value = c.id)} on:dblclick={() => dispatch('answer', c.id)}>
          <span class="name">{c.fullName || c.emails[0]?.value || ''}</span>
          {#if c.emails[0]}<span class="email">{c.emails[0].value}</span>{/if}
        </button>
      </li>
    {:else}
      <li class="none">{$t('scripts.ask.noContacts')}</li>
    {/each}
  </ul>
</div>

<style>
  .contact {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
    max-height: calc(var(--control-height) * 8);
    overflow-y: auto;
  }

  .row {
    display: flex;
    width: 100%;
    gap: var(--space-2);
    align-items: baseline;
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    text-align: start;
    cursor: var(--cursor-action);
  }

  .row:hover {
    background: var(--surface-hover);
  }

  .row.on {
    background: var(--selection-bg);
  }

  .email,
  .none {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .none {
    padding: var(--space-2);
  }
</style>
