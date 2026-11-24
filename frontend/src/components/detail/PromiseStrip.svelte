<script lang="ts">
  // the promises found in one mail, as a quiet strip under its header: who
  // owes what and when, with Accept, Edit and Dismiss. several fold into one
  // line until opened. pointing at one, or focusing it, marks its sentence
  // in the body. nothing becomes a task until it is accepted.
  import { tick } from 'svelte'
  import handshakeIcon from '@phosphor-icons/core/assets/duotone/handshake-duotone.svg?raw'
  import caretIcon from '@phosphor-icons/core/assets/regular/caret-down.svg?raw'
  import '../../theme/controls.css'
  import Ph from '../common/Ph.svelte'
  import {
    acceptSuggestion,
    dismissSuggestion,
    editOf,
    promiseHighlight,
    promises,
    quietSender,
    suggestionsFor,
  } from '../../stores/promises'
  import { noteTipContext } from '../../stores/firsttips'
  import { ownerKey, personOf, shortDate } from '../../lib/promisetext'
  import { locale, t } from '../../lib/i18n'
  import type { MailPromise, PromiseDirection, PromiseEdit } from '../../lib/types'

  /** The local id of the message shown, for marking its body. */
  export let messageId: number
  /** The account the message is in. */
  export let accountId: number
  /** The message's Message-ID. */
  export let mailId: string

  $: items = suggestionsFor($promises, accountId, mailId)
  $: if (items.length > 0) {
    noteTipContext('promises')
  }

  let open = false
  let editing: PromiseEdit | null = null
  let announcement = ''
  let titleInput: HTMLInputElement

  $: folded = items.length > 1 && !open
  $: if (editing && !items.some((p) => p.id === editing?.id)) {
    editing = null
  }

  function mark(p: MailPromise | null): void {
    promiseHighlight.set(p ? { messageId, quote: p.quote } : null)
  }

  async function startEdit(p: MailPromise): Promise<void> {
    editing = { ...editOf(p), due: p.due.length > 10 ? p.due.slice(0, 10) : p.due }
    await tick()
    titleInput?.focus()
  }

  async function accept(edit: PromiseEdit): Promise<void> {
    if (await acceptSuggestion(edit)) {
      announcement = $t('promises.accepted.announce').replace('{title}', edit.title)
      editing = null
      mark(null)
    }
  }

  async function dismiss(p: MailPromise): Promise<void> {
    mark(null)
    await dismissSuggestion(p)
    announcement = $t('promises.dismissed')
  }

  async function quiet(p: MailPromise): Promise<void> {
    mark(null)
    await quietSender(p)
    announcement = $t('promises.quieted').replace('{name}', personOf(p))
  }

  function onEditKey(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.stopPropagation()
      editing = null
    }
  }

  function setDirection(value: string): void {
    if (editing) {
      editing = { ...editing, direction: value as PromiseDirection }
    }
  }
</script>

<p class="sr-only" aria-live="polite">{announcement}</p>
{#if items.length > 0}
  <section class="strip" aria-label={$t('promises.strip.label')}>
    {#if folded}
      <button type="button" class="fold" aria-expanded="false" on:click={() => (open = true)}>
        <Ph svg={handshakeIcon} size={14} />
        <span class="text">{$t('promises.strip.many').replace('{n}', String(items.length))}</span>
        <Ph svg={caretIcon} size={12} />
      </button>
    {:else}
      {#if items.length > 1}
        <button type="button" class="fold" aria-expanded="true" on:click={() => (open = false)}>
          <Ph svg={handshakeIcon} size={14} />
          <span class="text">{$t('promises.strip.many').replace('{n}', String(items.length))}</span>
          <span class="flip"><Ph svg={caretIcon} size={12} /></span>
        </button>
      {/if}
      <ul class="list" role="list">
        {#each items as p (p.id)}
          <li
            class="item"
            on:mouseenter={() => mark(p)}
            on:mouseleave={() => mark(null)}
            on:focusin={() => mark(p)}
            on:focusout={() => mark(null)}
          >
            {#if editing && editing.id === p.id}
              <form class="edit" on:submit|preventDefault={() => editing && void accept(editing)}>
                <input class="cal-field title" bind:this={titleInput} bind:value={editing.title} aria-label={$t('promises.edit.title')} on:keydown={onEditKey} />
                <div class="row">
                  <select class="cal-field" value={editing.direction} aria-label={$t('promises.edit.owner')} on:change={(e) => setDirection(e.currentTarget.value)} on:keydown={onEditKey}>
                    <option value="mine">{$t('promises.edit.mine')}</option>
                    <option value="theirs">{$t('promises.edit.theirs')}</option>
                  </select>
                  <input class="cal-field" bind:value={editing.personEmail} aria-label={$t('promises.edit.person')} on:keydown={onEditKey} />
                  <input class="cal-field" type="date" bind:value={editing.due} aria-label={$t('promises.edit.due')} on:keydown={onEditKey} />
                </div>
                <div class="row end">
                  <button type="button" class="cal-btn" on:click={() => (editing = null)}>{$t('promises.edit.cancel')}</button>
                  <button type="submit" class="cal-btn primary" disabled={!editing.title.trim()}>{$t('promises.accept')}</button>
                </div>
              </form>
            {:else}
              <span class="icon" aria-hidden="true"><Ph svg={handshakeIcon} size={14} /></span>
              <span class="what">
                <span class="owner">{$t(ownerKey(p)).replace('{name}', personOf(p))}</span>
                <q class="quote">{p.quote}</q>
                {#if p.due}
                  <span class="due">{$t('promises.due').replace('{when}', shortDate(p.due, $locale))}</span>
                {/if}
              </span>
              <span class="acts">
                <button type="button" class="cal-btn primary" aria-label={$t('promises.acceptOne').replace('{title}', p.title)} on:click={() => void accept(editOf(p))}>
                  {$t('promises.accept')}
                </button>
                <button type="button" class="cal-btn" aria-label={$t('promises.editOne').replace('{title}', p.title)} on:click={() => void startEdit(p)}>
                  {$t('promises.edit')}
                </button>
                <button type="button" class="cal-btn" aria-label={$t('promises.dismissOne').replace('{title}', p.title)} on:click={() => void dismiss(p)}>
                  {$t('promises.dismiss')}
                </button>
                {#if p.personEmail}
                  <button type="button" class="cal-btn quiet" on:click={() => void quiet(p)}>
                    {$t('promises.quiet').replace('{name}', personOf(p))}
                  </button>
                {/if}
              </span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{/if}

<style>
  .strip {
    margin: 0 var(--pane-pad) var(--space-3);
    border-left: var(--hairline) solid var(--accent);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .fold {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: var(--cursor-action);
  }

  .fold:hover {
    color: var(--text-primary);
  }

  .fold:focus-visible {
    outline: var(--hairline) solid var(--accent);
  }

  .flip {
    display: inline-flex;
    transform: rotate(180deg);
  }

  .text {
    flex: 1;
    min-width: 0;
  }

  .list {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .item {
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
  }

  .item + .item {
    border-top: var(--hairline) solid var(--border-subtle);
  }

  .icon {
    display: inline-flex;
    padding-top: var(--space-1);
    color: var(--accent);
  }

  .what {
    display: flex;
    flex: 1;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-1) var(--space-2);
    min-width: 0;
  }

  .owner {
    color: var(--text-primary);
    font-weight: var(--fw-medium);
  }

  .quote {
    min-width: 0;
    overflow: hidden;
    color: var(--text-secondary);
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .due {
    color: var(--text-tertiary);
  }

  .acts {
    display: flex;
    flex-shrink: 0;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .quiet {
    color: var(--text-tertiary);
  }

  .edit {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-2);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .row.end {
    justify-content: flex-end;
  }

  .title {
    width: 100%;
  }
</style>
