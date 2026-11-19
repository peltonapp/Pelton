<script lang="ts">
  // the quick compose settings: the switch that registers the system-wide
  // shortcut and the shortcut itself, recorded from a key press. The backend
  // registers before it stores, so a shortcut another app holds is refused
  // here with the reason, and the previous one stays.
  import { onDestroy, onMount } from 'svelte'
  import Row from './ui/Row.svelte'
  import ToggleSwitch from '../common/ToggleSwitch.svelte'
  import { prefs, setQuickComposePref } from '../../stores/prefs'
  import { recording } from '../../stores/shortcuts'
  import { getQuickComposeStatus } from '../../lib/api'
  import { recordShortcut, shortcutKeys } from '../../lib/quickshortcut'
  import { errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'
  import type { QuickComposeStatus } from '../../lib/types'

  let status: QuickComposeStatus | null = null
  let capturing = false
  let busy = false
  let error = ''

  onMount(async () => {
    status = await getQuickComposeStatus().catch(() => null)
  })

  $: shortcut = $prefs.quickComposeShortcut || status?.shortcut || ''
  // on but not held: the system refused the stored shortcut at launch.
  $: notHeld = $prefs.quickCompose && status !== null && !status.registered

  async function apply(enabled: boolean, accelerator: string): Promise<void> {
    busy = true
    error = ''
    try {
      const next = await setQuickComposePref(enabled, accelerator)
      status = next
      if (next.error === 'taken') {
        error = $t('quickCompose.settings.taken').replace('{shortcut}', shortcutKeys(accelerator).join('+'))
      } else if (next.error === 'invalid') {
        error = $t('quickCompose.settings.invalid')
      }
    } catch (err) {
      error = errorMessage(err)
    } finally {
      busy = false
    }
  }

  function startCapture(): void {
    error = ''
    capturing = true
    recording.set(true)
  }

  function stopCapture(): void {
    capturing = false
    recording.set(false)
  }

  function onKeydown(event: KeyboardEvent): void {
    if (!capturing) {
      return
    }
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape' && !event.ctrlKey && !event.altKey && !event.metaKey) {
      stopCapture()
      return
    }
    const got = recordShortcut(event)
    if (got.kind === 'pending') {
      return
    }
    stopCapture()
    if (got.kind === 'invalid') {
      error = $t('quickCompose.settings.invalid')
      return
    }
    void apply($prefs.quickCompose, got.accelerator)
  }

  onDestroy(() => {
    if (capturing) {
      recording.set(false)
    }
  })
</script>

<svelte:window on:keydown|capture={onKeydown} />

<Row id="quick-compose" label={$t('quickCompose.settings.toggle')} hint={$t('quickCompose.settings.hint')}>
  <ToggleSwitch
    checked={$prefs.quickCompose}
    disabled={busy}
    label={$t('quickCompose.settings.toggle')}
    on:change={(e) => void apply(e.detail, shortcut)}
  />
</Row>
<Row id="quick-compose-shortcut" label={$t('quickCompose.settings.shortcut')} hint={$t('quickCompose.settings.shortcutHint')}>
  <button type="button" class="recorder" class:capturing disabled={busy} on:click={capturing ? stopCapture : startCapture}>
    {#if capturing}
      <span class="prompt">{$t('quickCompose.settings.recording')}</span>
    {:else}
      {#each shortcutKeys(shortcut) as key, i (i)}
        <kbd>{key}</kbd>
      {/each}
    {/if}
  </button>
</Row>
{#if error || notHeld || status?.wayland}
  <div class="notes">
    {#if error}
      <p class="error" role="alert">{error}</p>
    {:else if notHeld}
      <p class="error">{$t('quickCompose.settings.notHeld')}</p>
    {/if}
    {#if status?.wayland}
      <p>{$t('quickCompose.settings.wayland')}</p>
    {/if}
  </div>
{/if}

<style>
  .recorder {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-width: calc(var(--control-height) * 3);
    height: var(--control-height);
    padding: 0 var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-base);
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
    transition: border-color var(--motion-fast) var(--ease-out);
  }

  .recorder:hover {
    background: var(--surface-hover);
  }

  .recorder.capturing {
    border-color: var(--accent);
  }

  .recorder:disabled {
    opacity: 0.5;
  }

  kbd {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  kbd + kbd::before {
    content: '+';
    margin-inline-end: var(--space-2);
    color: var(--text-tertiary);
  }

  .prompt {
    color: var(--text-tertiary);
  }

  .notes {
    padding: var(--space-3) var(--space-4);
  }

  .notes p {
    margin: 0;
    font-size: var(--fz-label);
    line-height: 1.4;
    color: var(--text-tertiary);
  }

  .notes p + p {
    margin-top: var(--space-2);
  }

  .notes .error {
    color: var(--danger);
  }
</style>
