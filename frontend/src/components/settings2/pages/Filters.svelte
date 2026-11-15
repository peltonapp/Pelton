<script lang="ts">
  // the Server filters page: rules the mail server runs on new mail, so they
  // work while Pelton is closed. shown as rules, or as the sieve script itself.
  import '../../../theme/settings2-sections-system.css'
  import '../../../theme/settings2-sieve.css'
  import Card from '../ui/Card.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import SieveFrame from '../sieve/SieveFrame.svelte'
  import RuleList from '../sieve/RuleList.svelte'
  import RawScript from '../sieve/RawScript.svelte'
  import { runSave } from '../sieve/sieveSave'
  import { saveSieveRules, saveSieveScript, checkSieveScript, listFolders } from '../../../lib/api'
  import { toastError, errorMessage } from '../../../stores/toast'
  import { t } from '../../../lib/i18n'
  import type { SelectOption } from '../../../lib/selectnav'
  import type { Folder, SieveRule, SieveState } from '../../../lib/types'

  let state: SieveState | null = null
  let busy = false
  let problem = ''
  let line = 0
  let valid = false
  let view = 'rules'
  let folders: SelectOption[] = []
  let foldersFor = 0

  $: if (state) {
    problem = ''
    line = 0
    valid = false
  }

  $: if (state && state.status === 'ok' && state.accountId !== foldersFor) {
    foldersFor = state.accountId
    void loadFolders(state.accountId)
  }

  async function loadFolders(accountId: number): Promise<void> {
    try {
      const list = await listFolders(accountId)
      if (foldersFor === accountId) {
        folders = folderOptions(list)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // folders by their server path, nested ones shown with their parents.
  function folderOptions(list: Folder[]): SelectOption[] {
    return list
      .filter((f) => !f.attributes?.some((a) => a.toLowerCase() === '\\noselect'))
      .map((f) => ({ value: f.imapPath, label: f.delimiter ? f.imapPath.split(f.delimiter).join(' / ') : f.imapPath }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }

  async function saveRules(rules: SieveRule[]): Promise<void> {
    if (!state) {
      return
    }
    const id = state.accountId
    busy = true
    const outcome = await runSave(() => saveSieveRules(id, rules))
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

  $: views = [
    { key: 'rules', label: $t('sieve.filters.view.rules') },
    { key: 'script', label: $t('sieve.filters.view.script') },
  ]

  $: otherNote = !state?.otherActive
    ? ''
    : state.caps.include
      ? $t('sieve.otherActive.include').replace('{name}', state.otherActive)
      : $t('sieve.otherActive.replace').replace('{name}', state.otherActive)
</script>

<SieveFrame anchor="filters" bind:state bind:busy />

{#if state && state.status === 'ok'}
  {#if state.editable}
    <div class="views">
      <Segmented label={$t('sieve.filters.view')} value={view} options={views} on:change={(e) => (view = e.detail)} />
    </div>
  {/if}
  {#if state.editable && view === 'rules'}
    <Card title={$t('sieve.filters.rules')} description={$t('sieve.filters.rulesHint')} id="filters-rules" bare>
      {#if otherNote}
        <p class="sv-note other">{otherNote}</p>
      {/if}
      <RuleList saved={state.script.rules} caps={state.caps} {folders} {busy} {problem} on:save={(e) => saveRules(e.detail)} />
    </Card>
  {:else}
    <Card
      title={$t('sieve.raw.title')}
      description={state.editable ? $t('sieve.raw.hint') : $t('sieve.foreign')}
      id="filters-script"
      bare
    >
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
  {/if}
{/if}

<style>
  .views {
    display: flex;
    justify-content: flex-end;
    margin-bottom: var(--space-4);
  }

  .other {
    margin: 0 0 var(--space-3);
  }
</style>
