<script lang="ts">
  // a recipient field that renders addresses as removable chips. it keeps the
  // store contract simple by emitting a comma-separated string (which the send
  // path already parses); chips are just the display layer. typing a comma,
  // semicolon, Enter or Tab commits the current token; Backspace on an empty
  // input removes the last chip.
  import { createEventDispatcher } from 'svelte'
  import { IconX } from '@tabler/icons-svelte'
  import { searchAddresses } from '../../lib/api'
  import type { AddressBookEntry } from '../../lib/types'
  import { t } from '../../lib/i18n'
  import { splitAddressList } from '../../lib/mailcompose'

  export let value = ''
  export let label: string
  export let id: string
  export let chipsEnabled = true
  export let autocompleteEnabled = true

  const dispatch = createEventDispatcher<{ change: string }>()

  let draft = ''

  // autocomplete against the harvested address book. queries are debounced and a
  // highlighted suggestion commits on Enter; the list also commits on click.
  let suggestions: AddressBookEntry[] = []
  let highlight = -1
  let debounce: ReturnType<typeof setTimeout> | undefined

  $: chips = splitAddressList(value)

  function emit(next: string[]): void {
    dispatch('change', next.join(', '))
  }

  function onInput(): void {
    const q = draft.trim()
    if (debounce) {
      clearTimeout(debounce)
    }
    if (!autocompleteEnabled || q.length < 1) {
      suggestions = []
      highlight = -1
      return
    }
    debounce = setTimeout(async () => {
      try {
        const found = await searchAddresses(q, 6)
        // drop any already added as a chip.
        suggestions = found.filter((e) => !chips.includes(e.email))
        highlight = suggestions.length > 0 ? 0 : -1
      } catch {
        suggestions = []
      }
    }, 140)
  }

  function closeSuggest(): void {
    suggestions = []
    highlight = -1
  }

  function commit(): void {
    const token = draft.trim().replace(/[,;]+$/, '').trim()
    if (token) {
      emit([...chips, token])
    }
    draft = ''
    closeSuggest()
  }

  function commitSuggestion(entry: AddressBookEntry): void {
    emit([...chips, entry.email])
    draft = ''
    closeSuggest()
  }

  function removeAt(index: number): void {
    emit(chips.filter((_, i) => i !== index))
  }

  function onKeydown(event: KeyboardEvent): void {
    if (suggestions.length > 0 && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault()
      const delta = event.key === 'ArrowDown' ? 1 : -1
      highlight = (highlight + delta + suggestions.length) % suggestions.length
      return
    }
    if (event.key === 'Escape' && suggestions.length > 0) {
      event.preventDefault()
      closeSuggest()
      return
    }
    if (event.key === 'Enter' || event.key === ',' || event.key === ';' || event.key === 'Tab') {
      if (suggestions.length > 0 && highlight >= 0 && event.key !== ',' && event.key !== ';') {
        event.preventDefault()
        commitSuggestion(suggestions[highlight])
        return
      }
      if (draft.trim()) {
        event.preventDefault()
        commit()
      }
    } else if (event.key === 'Backspace' && draft === '' && chips.length > 0) {
      event.preventDefault()
      removeAt(chips.length - 1)
    }
  }

  // commit on blur, but let a suggestion click land first.
  function onBlur(): void {
    setTimeout(() => {
      commit()
    }, 120)
  }

  // a light validity hint: a chip without an @ is probably incomplete.
  function looksValid(addr: string): boolean {
    return addr.includes('@')
  }
</script>

{#if chipsEnabled}
  <div class="chips" role="group" aria-label={label}>
    {#each chips as chip, index (chip + index)}
      <span class="chip" class:invalid={!looksValid(chip)}>
        <span class="chip-text">{chip}</span>
        <button type="button" class="chip-x" aria-label={`${$t('compose.attach.remove')} ${chip}`} on:click={() => removeAt(index)}>
          <IconX size={11} stroke={2} />
        </button>
      </span>
    {/each}
    <div class="input-wrap">
      <input
        {id}
        type="text"
        autocomplete="off"
        bind:value={draft}
        placeholder={chips.length === 0 ? $t('compose.chipInput.placeholder') : ''}
        on:input={onInput}
        on:keydown={onKeydown}
        on:blur={onBlur}
      />
      {#if suggestions.length > 0}
        <ul class="suggest" role="listbox">
          {#each suggestions as s, i (s.email)}
            <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-noninteractive-element-interactions -->
            <li
              class="suggest-item"
              class:active={i === highlight}
              role="option"
              aria-selected={i === highlight}
              on:mousedown|preventDefault={() => commitSuggestion(s)}
              on:mouseenter={() => (highlight = i)}
            >
              <span class="s-name">{s.name || s.email}</span>
              {#if s.name}<span class="s-addr">{s.email}</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
{:else}
  <input
    {id}
    type="text"
    autocomplete="off"
    class="plain"
    {value}
    placeholder={$t('compose.chipInput.placeholderMulti')}
    on:input={(e) => dispatch('change', e.currentTarget.value)}
  />
{/if}

<style>
  .chips {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    min-width: 0;
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 1px var(--space-1) 1px var(--space-2);
    border-radius: 999px;
    background: var(--surface-sunken);
    border: var(--hairline) solid var(--border-subtle);
    font-size: var(--fz-meta);
    max-width: 100%;
  }

  .chip.invalid {
    border-color: var(--warning);
  }

  .chip-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chip-x {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    padding: 1px;
    border-radius: 999px;
    flex-shrink: 0;
  }

  .chip-x:hover {
    color: var(--danger);
  }

  .input-wrap {
    position: relative;
    flex: 1;
    min-width: 80px;
    display: flex;
  }

  input {
    flex: 1;
    min-width: 80px;
    border: none;
    background: transparent;
    outline: none;
    font-size: var(--fz-body);
  }

  input.plain {
    width: 100%;
  }

  .suggest {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 120;
    min-width: 240px;
    max-width: 360px;
    margin: 0;
    padding: var(--space-1);
    list-style: none;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
  }

  .suggest-item {
    display: flex;
    flex-direction: column;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }
  .suggest-item.active {
    background: var(--surface-hover);
  }
  .s-name {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }
  .s-addr {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
