<script lang="ts">
  // the details of one time block: its title, whether it is done, and whether
  // it is also written to a calendar as a busy event and what that event is
  // called. a block out of step with its event says what happened and offers
  // the ways to settle it. a block whose mail is in a locked vault shows as
  // busy and keeps its title out of reach.
  import { createEventDispatcher } from 'svelte'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import Select from '../common/Select.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { formatWhen } from '../../lib/calformat'
  import { t } from '../../lib/i18n'
  import type { TimeFormat } from '../../lib/format'
  import type { Calendar, TimeBlock, TimeBlockRequest, TimeBlockSettings } from '../../lib/types'

  /** The block shown. */
  export let block: TimeBlock
  /** The calendars a block can be written to. */
  export let calendars: Calendar[]
  export let settings: TimeBlockSettings
  export let timeFormat: TimeFormat = 'auto'

  const dispatch = createEventDispatcher<{
    close: void
    save: Partial<TimeBlockRequest>
    remove: void
    settle: 'follow' | 'rewrite' | 'forget'
  }>()

  let title = block.masked ? '' : block.title
  let done = block.done
  let calendarId = String(block.calendarId)
  let eventTitle = block.eventTitle

  $: calendarItems = [
    { value: '0', label: $t('timeblock.dialog.deviceOnly') },
    ...calendars.filter((c) => !c.readOnly || c.id === block.calendarId).map((c) => ({ value: String(c.id), label: c.name || $t('calendar.defaultName') })),
  ]
  $: titleItems = [
    {
      value: '',
      label: settings.includeTitle ? $t('timeblock.dialog.eventTitle.followOn') : $t('timeblock.dialog.eventTitle.followOff').replace('{title}', settings.eventTitle),
    },
    { value: 'generic', label: $t('timeblock.dialog.eventTitle.generic').replace('{title}', settings.eventTitle) },
    { value: 'title', label: $t('timeblock.dialog.eventTitle.own') },
  ]

  function save(): void {
    dispatch('save', { title: block.masked ? '' : title.trim(), done, calendarId: Number(calendarId), eventTitle })
  }
</script>

<Modal
  title={block.masked ? $t('calendar.block.busy') : block.title || $t('calendar.block.untitled')}
  hint={formatWhen(new Date(block.start), new Date(block.end), false, timeFormat)}
  size="small"
  on:close={() => dispatch('close')}
>
  <form class="fields" on:submit|preventDefault={save}>
    {#if block.masked}
      <p class="note">{$t('timeblock.dialog.masked')}</p>
    {:else}
      <label class="field">
        <span>{$t('timeblock.dialog.title')}</span>
        <input class="cal-field" bind:value={title} placeholder={$t('calendar.block.untitled')} />
      </label>
    {/if}
    <div class="row">
      <span>{$t('timeblock.dialog.done')}</span>
      <ToggleSwitch checked={done} label={$t('timeblock.dialog.done')} on:change={(e) => (done = e.detail)} />
    </div>
    <div class="row">
      <span>{$t('timeblock.dialog.calendar')}</span>
      <Select value={calendarId} items={calendarItems} ariaLabel={$t('timeblock.dialog.calendar')} on:change={(e) => (calendarId = e.detail)} />
    </div>
    {#if calendarId !== '0'}
      <div class="row">
        <span>{$t('timeblock.dialog.eventTitle')}</span>
        <Select value={eventTitle} items={titleItems} ariaLabel={$t('timeblock.dialog.eventTitle')} on:change={(e) => (eventTitle = e.detail)} />
      </div>
      {#if block.hasMail}
        <p class="note">{$t('timeblock.dialog.mailPrivacy')}</p>
      {/if}
    {/if}
    {#if block.linkState}
      <div class="link" role="status">
        <p>{$t(block.linkState === 'event_gone' ? 'timeblock.dialog.gone' : 'timeblock.dialog.outOfStep')}</p>
        <div class="actions">
          {#if block.linkState === 'out_of_step'}
            <button type="button" class="cal-btn" on:click={() => dispatch('settle', 'follow')}>{$t('timeblock.settle.follow')}</button>
          {/if}
          <button type="button" class="cal-btn" on:click={() => dispatch('settle', 'rewrite')}>{$t('timeblock.settle.rewrite')}</button>
          <button type="button" class="cal-btn" on:click={() => dispatch('settle', 'forget')}>{$t('timeblock.settle.forget')}</button>
        </div>
      </div>
    {/if}
    <button type="submit" hidden aria-label={$t('timeblock.dialog.save')}></button>
  </form>
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn danger" on:click={() => dispatch('remove')}>{$t('timeblock.delete')}</button>
    <span class="spacer"></span>
    <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('confirm.cancel')}</button>
    <button type="button" class="cal-btn primary" on:click={save}>{$t('timeblock.dialog.save')}</button>
  </svelte:fragment>
</Modal>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .link {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    border-inline-start: var(--cal-edge) solid var(--warning);
    background: var(--warning-bg);
    font-size: var(--fz-label);
  }

  .link p {
    margin: 0;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .spacer {
    flex: 1;
  }
</style>
