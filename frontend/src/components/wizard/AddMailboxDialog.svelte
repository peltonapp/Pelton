<script lang="ts">
  // the add-mailbox wizard as the main window uses it: lazy-loaded, and left
  // open after 'added' so the folder picker and the done step can still be
  // reached. It closes only when the wizard itself says so.
  import { createEventDispatcher } from 'svelte'
  import { get } from 'svelte/store'
  import { t } from '../../lib/i18n'
  import { refreshSidebar } from '../../stores/accounts'
  import { toastInfo } from '../../stores/toast'

  const dispatch = createEventDispatcher<{ close: void; added: void }>()

  function onAdded(): void {
    void refreshSidebar().then(() => dispatch('added'))
    toastInfo(get(t)('app.toast.mailboxAdded'))
  }
</script>

{#await import('./AddMailboxWizard.svelte') then m}
  <svelte:component this={m.default} on:close on:added={onAdded} />
{/await}
