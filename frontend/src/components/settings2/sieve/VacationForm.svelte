<script lang="ts">
  // the out of office form: on or off, the days it runs, the reply itself, how
  // often one sender hears it and which addresses it answers for. edits stay
  // here until Save, which hands the whole reply to the page.
  import { createEventDispatcher } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import DateTimePicker from '../../common/DateTimePicker.svelte'
  import PieProgress from '../../common/PieProgress.svelte'
  import { blankVacation, localDay, sameModel, vacationPhase } from '../../../lib/sieve'
  import { formatDateTimeMedium } from '../../../lib/format'
  import { t } from '../../../lib/i18n'
  import type { SieveVacation } from '../../../lib/types'

  /** The reply as it is on the server, null when none was ever saved. */
  export let saved: SieveVacation | null
  /** The account's own addresses. */
  export let addresses: string[]
  /** Whether the server can bound the reply by dates. */
  export let dates: boolean
  /** Whether Pelton's script is the active one on the server. */
  export let scriptActive: boolean
  /** True while a save is running. */
  export let busy: boolean = false
  /** The server's objection to the last save, '' for none. */
  export let problem: string = ''
  /** Today, YYYY-MM-DD; the state line is worked out against it. */
  export let today: string = localDay()

  const dispatch = createEventDispatcher<{ save: SieveVacation }>()

  let draft: SieveVacation = blankVacation(addresses)
  let base: SieveVacation = draft
  let lastSaved: SieveVacation | null | undefined = undefined
  $: if (saved !== lastSaved) {
    lastSaved = saved
    base = saved ? { ...saved, addresses: [...saved.addresses] } : blankVacation(addresses)
    draft = { ...base, addresses: [...base.addresses] }
  }

  $: dirty = !sameModel(draft, base)
  $: phase = vacationPhase(saved, scriptActive, today)
  $: phaseLine = phaseText(phase, saved)
  $: choices = [...addresses, ...draft.addresses.filter((a) => !addresses.includes(a))]
  $: invalid =
    draft.body.trim() === ''
      ? $t('sieve.vacation.noMessage')
      : draft.start && draft.end && draft.end < draft.start
        ? $t('sieve.vacation.badRange')
        : ''

  function day(iso: string): string {
    const [y, m, d] = iso.split('-').map(Number)
    return formatDateTimeMedium(new Date(y, m - 1, d), false)
  }

  function phaseText(p: string, v: SieveVacation | null): string {
    switch (p) {
      case 'scheduled':
        return $t('sieve.vacation.phase.scheduled').replace('{date}', day(v?.start ?? ''))
      case 'active':
        return v?.end ? $t('sieve.vacation.phase.activeUntil').replace('{date}', day(v.end)) : $t('sieve.vacation.phase.active')
      case 'ended':
        return $t('sieve.vacation.phase.ended').replace('{date}', day(v?.end ?? ''))
    }
    return $t(`sieve.vacation.phase.${p}`)
  }

  function toggleAddress(address: string): void {
    draft = {
      ...draft,
      addresses: draft.addresses.includes(address) ? draft.addresses.filter((a) => a !== address) : [...draft.addresses, address],
    }
  }

  function onDays(event: Event): void {
    const n = Math.round(Number((event.currentTarget as HTMLInputElement).value))
    draft = { ...draft, days: Number.isFinite(n) && n > 0 ? n : 0 }
  }

  function save(): void {
    if (!invalid) {
      dispatch('save', { ...draft, addresses: [...draft.addresses] })
    }
  }
</script>

<Card title={$t('sieve.vacation.reply')} id="vacation-reply">
  <div class="phase" class:on={phase === 'active'}>
    <span class="dot" aria-hidden="true"></span>
    <span>{phaseLine}</span>
  </div>
  <Row id="vacation-enabled" label={$t('sieve.vacation.enabled')} hint={$t('sieve.vacation.enabledHint')}>
    <ToggleSwitch
      checked={draft.enabled}
      label={$t('sieve.vacation.enabled')}
      on:change={(e) => (draft = { ...draft, enabled: e.detail })}
    />
  </Row>
  {#if dates}
    <Row id="vacation-dates" label={$t('sieve.vacation.dates')} hint={$t('sieve.vacation.datesHint')} stacked>
      <div class="dates">
        <div class="date">
          <span class="caption">{$t('sieve.vacation.start')}</span>
          <div class="date-control">
            <DateTimePicker mode="date" bind:value={draft.start} />
            {#if draft.start}
              <button type="button" class="s2-btn quiet" on:click={() => (draft = { ...draft, start: '' })}>
                {$t('sieve.vacation.clearDate')}
              </button>
            {/if}
          </div>
        </div>
        <div class="date">
          <span class="caption">{$t('sieve.vacation.end')}</span>
          <div class="date-control">
            <DateTimePicker mode="date" bind:value={draft.end} />
            {#if draft.end}
              <button type="button" class="s2-btn quiet" on:click={() => (draft = { ...draft, end: '' })}>
                {$t('sieve.vacation.clearDate')}
              </button>
            {/if}
          </div>
        </div>
      </div>
    </Row>
  {:else}
    <p class="sv-note pad">{$t('sieve.vacation.noDates')}</p>
  {/if}
  <Row id="vacation-subject" label={$t('sieve.vacation.subject')} stacked>
    <input
      class="sv-input"
      type="text"
      aria-label={$t('sieve.vacation.subject')}
      placeholder={$t('sieve.vacation.subjectPlaceholder')}
      bind:value={draft.subject}
    />
  </Row>
  <Row id="vacation-message" label={$t('sieve.vacation.message')} stacked>
    <textarea class="sv-input" rows="6" aria-label={$t('sieve.vacation.message')} bind:value={draft.body}></textarea>
  </Row>
  <Row id="vacation-days" label={$t('sieve.vacation.days')} hint={$t('sieve.vacation.daysHint')}>
    <input
      class="sv-input days"
      type="number"
      min="1"
      max="365"
      aria-label={$t('sieve.vacation.days')}
      value={draft.days || ''}
      on:change={onDays}
    />
  </Row>
  <Row id="vacation-addresses" label={$t('sieve.vacation.addresses')} hint={$t('sieve.vacation.addressesHint')} stacked>
    <div class="addresses">
      {#each choices as address (address)}
        <label class="address">
          <input type="checkbox" checked={draft.addresses.includes(address)} on:change={() => toggleAddress(address)} />
          <span>{address}</span>
        </label>
      {/each}
    </div>
  </Row>
</Card>

<div class="sv-bar">
  {#if problem}
    <p class="sv-problem">{problem}</p>
  {:else if dirty && invalid}
    <p class="sv-problem">{invalid}</p>
  {/if}
  {#if busy}
    <PieProgress label={$t('sieve.saving')} />
  {/if}
  {#if dirty}
    <button type="button" class="s2-btn quiet" disabled={busy} on:click={() => (draft = { ...base, addresses: [...base.addresses] })}>
      {$t('sieve.discard')}
    </button>
  {/if}
  <button type="button" class="s2-btn primary" disabled={busy || !dirty || invalid !== ''} on:click={save}>
    {$t('sieve.save')}
  </button>
</div>

<style>
  .phase {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-3) var(--space-4);
    font-size: var(--fz-list);
    color: var(--text-secondary);
  }

  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--text-tertiary);
    flex-shrink: 0;
  }

  .phase.on {
    color: var(--text-primary);
  }

  .phase.on .dot {
    background: var(--success);
  }

  .dates {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-5);
  }

  .date {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .date-control {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .caption {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .pad {
    padding: var(--space-3) var(--space-4);
  }

  .days {
    width: 6em;
  }

  .addresses {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .address {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .address input {
    accent-color: var(--accent);
  }
</style>
