<script lang="ts">
  // the AI switches of a saved script: whether its run log keeps the text
  // sent to the model and the answers, and whether a test run really asks the
  // model. both are off until the user turns them on.
  import { onMount } from 'svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { aiScriptOptions, setAIScriptOptions } from '../../lib/api'
  import type { AIScriptOptions } from '../../lib/ai'
  import { errorMessage, toastError } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  /** The script, which must be saved. */
  export let scriptId: number

  let options: AIScriptOptions = { logText: false, liveTest: false }

  onMount(async () => {
    try {
      options = await aiScriptOptions(scriptId)
    } catch (err) {
      toastError(errorMessage(err))
    }
  })

  async function set(change: Partial<AIScriptOptions>): Promise<void> {
    const next = { ...options, ...change }
    try {
      await setAIScriptOptions(scriptId, next)
      options = next
    } catch (err) {
      toastError(errorMessage(err))
    }
  }
</script>

<div class="options">
  <div class="option">
    <div class="text">
      <span>{$t('ai.options.logText')}</span>
      <span class="hint">{$t('ai.options.logText.hint')}</span>
    </div>
    <ToggleSwitch checked={options.logText} label={$t('ai.options.logText')} on:change={(e) => set({ logText: e.detail })} />
  </div>
  <div class="option">
    <div class="text">
      <span>{$t('ai.options.liveTest')}</span>
      <span class="hint">{$t('ai.options.liveTest.hint')}</span>
    </div>
    <ToggleSwitch checked={options.liveTest} label={$t('ai.options.liveTest')} on:change={(e) => set({ liveTest: e.detail })} />
  </div>
</div>

<style>
  .options {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    margin-top: var(--space-2);
  }

  .option {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
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
