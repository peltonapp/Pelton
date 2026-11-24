<script lang="ts">
  // hosts whichever vault dialog the store asks for. It is loaded on first use,
  // so none of the dialogs below are part of the window's startup.
  import { vaultDialog } from '../../stores/vaultdialog'
  import VaultUnlockDialog from './VaultUnlockDialog.svelte'
  import VaultCreateDialog from './VaultCreateDialog.svelte'
  import VaultConsentDialog from './VaultConsentDialog.svelte'

  function close(): void {
    vaultDialog.set(null)
  }
</script>

{#if $vaultDialog?.kind === 'unlock'}
  <VaultUnlockDialog vaultId={$vaultDialog.vaultId} on:close={close} />
{:else if $vaultDialog?.kind === 'create'}
  <VaultCreateDialog member={$vaultDialog.member} on:close={close} />
{:else if $vaultDialog?.kind === 'consent'}
  <VaultConsentDialog answer={$vaultDialog.answer} on:close={close} />
{/if}
