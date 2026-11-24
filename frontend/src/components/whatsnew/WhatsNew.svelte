<script lang="ts">
  // the What's new page: the notable additions of the running release, one
  // line each, with a button that opens the feature. the notes ship with the
  // app; nothing is fetched.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import '../../theme/controls.css'
  import { whatsNewRelease } from '../../stores/whatsnew'
  import { openSpace } from '../../stores/space'
  import { openSettingsAt } from '../../stores/settingsnav'
  import { historyOpen } from '../../stores/undohistory'
  import { calendarView } from '../../stores/calendar'
  import { t } from '../../lib/i18n'
  import type { WhatsNewTarget } from '../../lib/whatsnew'

  const dispatch = createEventDispatcher<{ close: void }>()

  function show(target: WhatsNewTarget): void {
    dispatch('close')
    switch (target.kind) {
      case 'space':
        openSpace(target.space)
        break
      case 'settings':
        openSettingsAt(target.anchor)
        break
      case 'history':
        openSpace('mail')
        historyOpen.set(true)
        break
      case 'calendar-view':
        calendarView.set(target.view)
        openSpace('calendar')
        break
    }
  }

  $: release = $whatsNewRelease
  $: hint = release ? $t('whatsnew.hint').replace('{version}', release.line) : ''
</script>

<Modal title={$t('whatsnew.title')} {hint} size="medium" on:close={() => dispatch('close')}>
  {#if release && release.entries.length > 0}
    <ul class="entries">
      {#each release.entries as entry (entry.id)}
        <li>
          <span class="text">
            <span class="title">{$t(entry.titleKey)}</span>
            <span class="body">{$t(entry.bodyKey)}</span>
          </span>
          <button type="button" class="cal-btn" aria-label={`${$t('whatsnew.show')}: ${$t(entry.titleKey)}`} on:click={() => show(entry.target)}>
            {$t('whatsnew.show')}
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="none">{$t('whatsnew.none')}</p>
  {/if}
</Modal>

<style>
  .entries {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  li {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  li:last-child {
    border-bottom: 0;
  }

  .text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }

  .title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .body {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-secondary);
  }

  .cal-btn {
    flex-shrink: 0;
  }

  .none {
    margin: 0;
    font-size: var(--fz-body);
    color: var(--text-secondary);
  }
</style>
