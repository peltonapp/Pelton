<script lang="ts">
  // the duplicates view of the contacts space: the groups of contacts that
  // look like one person, the likeliest first, each with the reasons it was
  // flagged. a group opens in the side by side compare to be merged, or is
  // dismissed as not duplicates for good. after a merge a bar offers to undo it.
  import { createEventDispatcher } from 'svelte'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import Ph from '../common/Ph.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import ContactMergeCompare from './ContactMergeCompare.svelte'
  import { addressBooks } from '../../stores/contacts'
  import {
    dismissGroup,
    dismissedPairs,
    duplicateGroups,
    duplicatesLoading,
    lastMerge,
    showDismissedAgain,
    undoLastMerge,
  } from '../../stores/duplicates'
  import { matchLevel, reasonText } from '../../lib/contactmerge'
  import { t } from '../../lib/i18n'
  import type { DuplicateGroup } from '../../lib/types'

  const dispatch = createEventDispatcher<{ back: void }>()

  let comparing: DuplicateGroup | null = null

  function nameOf(c: DuplicateGroup['contacts'][number]): string {
    return c.fullName || c.emails[0]?.value || c.phones[0]?.value || ''
  }
</script>

<section class="duplicates" aria-label={$t('contacts.duplicates.title')}>
  <header class="head">
    <button type="button" class="back" on:click={() => dispatch('back')}>
      <span class="flip"><Ph svg={caretLeft} size={14} /></span>
      {$t('contacts.duplicates.back')}
    </button>
    <div class="titles">
      <h3>{$t('contacts.duplicates.title')}</h3>
      <p>{$t('contacts.duplicates.hint')}</p>
    </div>
  </header>

  {#if $lastMerge}
    <div class="undo" role="status">
      <span>{$t('contacts.duplicates.lastMerge').replace('{n}', String($lastMerge.count)).replace('{name}', $lastMerge.name)}</span>
      <button type="button" class="link" on:click={() => void undoLastMerge()}>{$t('contacts.merge.undo')}</button>
    </div>
  {/if}

  <div class="body">
    {#if $duplicatesLoading && $duplicateGroups.length === 0}
      <p class="state"><PieProgress label={$t('contacts.duplicates.finding')} /> {$t('contacts.duplicates.finding')}</p>
    {:else if $duplicateGroups.length === 0}
      <p class="state">{$t('contacts.duplicates.none')}</p>
    {:else}
      <ul class="groups">
        {#each $duplicateGroups as group, i (i)}
          <li class="group">
            <div class="top">
              <span class="level {matchLevel(group.score)}">{$t(`contacts.duplicates.level.${matchLevel(group.score)}`)}</span>
              <span class="score">{$t('contacts.duplicates.score').replace('{score}', String(group.score))}</span>
            </div>
            <ul class="people">
              {#each group.contacts as c (c.id)}
                <li>
                  <span class="name">{nameOf(c)}</span>
                  <span class="meta">{[c.organization, c.emails[0]?.value, c.bookName].filter(Boolean).join(' · ')}</span>
                </li>
              {/each}
            </ul>
            <ul class="reasons" aria-label={$t('contacts.duplicates.reasons')}>
              {#each group.reasons as reason, r (r)}
                <li>{reasonText(reason, $t)}</li>
              {/each}
            </ul>
            <div class="actions">
              <button type="button" class="btn primary" on:click={() => (comparing = group)}>{$t('contacts.duplicates.compare')}</button>
              <button type="button" class="btn" on:click={() => void dismissGroup(group)}>{$t('contacts.duplicates.notDuplicates')}</button>
            </div>
          </li>
        {/each}
      </ul>
    {/if}

    {#if $dismissedPairs > 0}
      <button type="button" class="link again" on:click={() => void showDismissedAgain()}>{$t('contacts.duplicates.showDismissed')}</button>
    {/if}
  </div>
</section>

{#if comparing}
  <ContactMergeCompare group={comparing.contacts} books={$addressBooks} on:close={() => (comparing = null)} on:merged={() => (comparing = null)} />
{/if}

<style>
  .duplicates {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--surface-raised);
  }

  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .back {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .back:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .flip {
    display: inline-flex;
  }

  :global([dir='rtl']) .flip {
    transform: scaleX(-1);
  }

  .titles h3 {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .titles p {
    margin: 0;
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .undo {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-sunken);
    font-size: var(--fz-label);
  }

  .body {
    flex: 1;
    min-height: 0;
    padding: var(--space-4);
    overflow-y: auto;
  }

  .state {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    margin: var(--space-6) 0;
    color: var(--text-secondary);
    font-size: var(--fz-body);
  }

  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .groups {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-width: calc(var(--fz-body) * 48);
    margin-inline: auto;
  }

  .group {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .top {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
  }

  .level {
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
    color: var(--text-primary);
  }

  .level.low {
    color: var(--text-secondary);
  }

  .score {
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    font-variant-numeric: tabular-nums;
  }

  .people {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .people li {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-3);
  }

  .name {
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
  }

  .meta {
    min-width: 0;
    color: var(--text-secondary);
    font-size: var(--fz-label);
    overflow-wrap: anywhere;
  }

  .reasons {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    padding-inline-start: var(--space-3);
    border-inline-start: var(--hairline) solid var(--border-default);
    color: var(--text-secondary);
    font-size: var(--fz-label);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .btn {
    height: var(--control-height);
    padding: 0 var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .btn:hover {
    background: var(--surface-hover);
  }

  .btn.primary {
    border-color: transparent;
    background: var(--accent);
    color: var(--accent-fg);
  }

  .btn.primary:hover {
    background: var(--accent);
    filter: brightness(1.06);
  }

  .link {
    padding: 0;
    border: none;
    background: transparent;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .again {
    display: block;
    margin: var(--space-4) auto 0;
  }
</style>
