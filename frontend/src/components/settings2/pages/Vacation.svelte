<script lang="ts">
  // the Out of office page: an automatic reply kept on the mail server, so it
  // answers while Pelton is closed.
  import '../../../theme/settings2-sections-system.css'
  import '../../../theme/settings2-sieve.css'
  import Card from '../ui/Card.svelte'
  import SieveFrame from '../sieve/SieveFrame.svelte'
  import VacationForm from '../sieve/VacationForm.svelte'
  import RawScript from '../sieve/RawScript.svelte'
  import { runSave } from '../sieve/sieveSave'
  import { saveSieveVacation, saveSieveScript, checkSieveScript } from '../../../lib/api'
  import { toastError, errorMessage } from '../../../stores/toast'
  import { t } from '../../../lib/i18n'
  import type { SieveState, SieveVacation } from '../../../lib/types'

  let state: SieveState | null = null
  let busy = false
  let problem = ''
  let line = 0
  let valid = false

  $: if (state) {
    problem = ''
    line = 0
    valid = false
  }

  async function save(vacation: SieveVacation): Promise<void> {
    if (!state) {
      return
    }
    const id = state.accountId
    busy = true
    const outcome = await runSave(() => saveSieveVacation(id, vacation))
    busy = false
    if (outcome.state) {
      state = outcome.state
    }
    problem = outcome.problem
  }

  async function checkRaw(text: string): Promise<void> {
    if (!state) {
      return
    }
    busy = true
    try {
      const result = await checkSieveScript(state.accountId, text)
      problem = result.problem
      line = result.line
      valid = result.problem === '' && result.state.status === 'ok'
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  async function saveRaw(text: string): Promise<void> {
    if (!state) {
      return
    }
    const id = state.accountId
    busy = true
    const outcome = await runSave(() => saveSieveScript(id, text))
    busy = false
    if (outcome.state) {
      state = outcome.state
    }
    problem = outcome.problem
    line = outcome.line
  }

  $: otherNote = !state?.otherActive
    ? ''
    : state.caps.include
      ? $t('sieve.otherActive.include').replace('{name}', state.otherActive)
      : $t('sieve.otherActive.replace').replace('{name}', state.otherActive)
</script>

<SieveFrame anchor="vacation" bind:state bind:busy />

{#if state && state.status === 'ok'}
  {#if !state.caps.vacation}
    <Card id="vacation-reply">
      <p class="sv-note pad">{$t('sieve.vacation.unsupported')}</p>
    </Card>
  {:else if !state.editable}
    <Card title={$t('sieve.raw.title')} description={$t('sieve.foreign')} id="vacation-script" bare>
      <RawScript
        source={state.raw}
        {busy}
        {problem}
        {line}
        {valid}
        on:edit={() => (valid = false)}
        on:check={(e) => checkRaw(e.detail)}
        on:save={(e) => saveRaw(e.detail)}
      />
    </Card>
  {:else}
    {#if otherNote}
      <p class="sv-note other">{otherNote}</p>
    {/if}
    <VacationForm
      saved={state.script.vacation}
      addresses={state.addresses}
      dates={state.caps.dates}
      scriptActive={state.active}
      {busy}
      {problem}
      on:save={(e) => save(e.detail)}
    />
  {/if}
{/if}

<style>
  .pad {
    padding: var(--space-3) var(--space-4);
  }

  .other {
    margin: 0 0 var(--space-4);
  }
</style>
