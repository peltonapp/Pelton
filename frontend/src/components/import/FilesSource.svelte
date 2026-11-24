<script lang="ts">
  // importing .eml and .mbox files (#308), picked or dropped onto the dialog.
  // Imported mail lands in Local Folders, which sync never touches, so nothing
  // here can reach a server or change what is on one.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import filesIcon from '@phosphor-icons/core/assets/duotone/files-duotone.svg?raw'
  import { chooseMailFiles } from '../../lib/api'
  import { toastError, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** True while a mail import runs. */
  export let running = false

  const dispatch = createEventDispatcher<{ importFiles: string[] }>()

  async function choose(): Promise<void> {
    try {
      const paths = await chooseMailFiles()
      if (paths.length > 0) {
        dispatch('importFiles', paths)
      }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<div class="source">
  <p class="hint">{$t('import.files.hint')}</p>
  <div class="zone">
    <span class="zone-icon"><Ph svg={filesIcon} size={28} /></span>
    <span class="zone-text">{$t('import.drop.zone')}</span>
    <FlowButton disabled={running} on:click={choose}>{$t('import.files.choose')}</FlowButton>
  </div>
  <p class="hint">{$t('import.files.intro')}</p>
</div>

<style>
  .source {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.5;
    color: var(--text-tertiary);
  }

  .zone {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-6) var(--space-5);
    border: var(--hairline) dashed var(--border-strong);
    border-radius: var(--radius-card);
    background: var(--surface-sunken);
    text-align: center;
  }

  .zone-icon {
    display: inline-flex;
    color: var(--text-tertiary);
  }

  .zone-text {
    font-size: var(--fz-list);
    color: var(--text-secondary);
  }
</style>
