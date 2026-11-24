<script lang="ts">
  // the AI switch of a saved script: whether its run log keeps the text sent
  // to the model and the answers. it is off until the user turns it on.
  import { onMount } from 'svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { aiScriptOptions, setAIScriptOptions } from '../../lib/api'
  import type { AIScriptOptions } from '../../lib/ai'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** The script, which must be saved. */
  export let scriptId: number

  let options: AIScriptOptions = { logText: false }

  onMount(async () => {
    try {
      options = await aiScriptOptions(scriptId)
    } catch (err) {
      toastError(errorMessage(err))
    }
  })

  async function setLogText(on: boolean): Promise<void> {
    const next = { ...options, logText: on }
    try {
      await setAIScriptOptions(scriptId, next)
      options = next
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<div class="option">
  <div class="text">
    <span>{$t('ai.options.logText')}</span>
    <span class="hint">{$t('ai.options.logText.hint')}</span>
  </div>
  <ToggleSwitch checked={options.logText} label={$t('ai.options.logText')} on:change={(e) => setLogText(e.detail)} />
</div>

<style>
  .option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: var(--space-2);
  }

  .text {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .hint {
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
