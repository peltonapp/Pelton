<script lang="ts">
  // a mail folder: the account first, then one of its folders. the answer is
  // the folder's path, which scripts resolve in a message's own account.
  import Select from '../../common/Select.svelte'
  import { t } from '../../../lib/i18n'
  import { accountLabel } from '../../../lib/format'
  import { listFolders } from '../../../lib/api'
  import type { ScriptAsk } from '../../../lib/scripting'
  import type { Folder } from '../../../lib/types'
  import { sidebar } from '../../../stores/accounts'
  import { errorMessage, toastError } from '../../../stores/toast'

  /** The question. */
  export let ask: ScriptAsk
  /** The picked folder's path. */
  export let value: unknown = ''

  let account = 0
  let loaded = -1
  let folders: Folder[] = []

  $: accounts = $sidebar.data?.accounts ?? []
  $: if (account === 0 && accounts.length > 0) {
    account = accounts[0].id
  }
  $: if (account !== 0 && account !== loaded) {
    loaded = account
    void load(account)
  }
  $: items = folders.map((f) => ({ value: f.imapPath, label: f.imapPath }))

  async function load(id: number): Promise<void> {
    try {
      folders = await listFolders(id)
      if (!folders.some((f) => f.imapPath === value)) {
        value = folders[0]?.imapPath ?? ''
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<div class="folder">
  {#if accounts.length > 1}
    <Select ariaLabel={$t('scripts.ask.account')} value={String(account)} items={accounts.map((a) => ({ value: String(a.id), label: accountLabel(a) }))} on:change={(e) => (account = Number(e.detail))} />
  {/if}
  <Select ariaLabel={ask.prompt || $t('scripts.ask.answer')} value={String(value ?? '')} {items} on:change={(e) => (value = e.detail)} />
</div>

<style>
  .folder {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
</style>
