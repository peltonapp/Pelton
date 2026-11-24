<script lang="ts">
  // one of the user's accounts; the answer is its id.
  import Select from '../../common/Select.svelte'
  import { t } from '../../../lib/i18n'
  import { accountLabel } from '../../../lib/format'
  import type { ScriptAsk } from '../../../lib/scripting'
  import { sidebar } from '../../../stores/accounts'

  /** The question. */
  export let ask: ScriptAsk
  /** The picked account id. */
  export let value: unknown = undefined

  $: accounts = $sidebar.data?.accounts ?? []
  $: items = accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))
  $: if (value === undefined && accounts.length > 0) {
    value = accounts[0].id
  }
</script>

<Select ariaLabel={ask.prompt || $t('scripts.ask.answer')} value={String(value ?? '')} {items} on:change={(e) => (value = Number(e.detail))} />
