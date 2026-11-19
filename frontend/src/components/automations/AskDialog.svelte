<script lang="ts">
  // a running script's question: the prompt, the control its kind names
  // (see ask/controls.ts) and the answer buttons. cancelling ends the run. a
  // script nobody started gives up after a while and takes its default; the
  // dialog counts that down.
  import { createEventDispatcher, onDestroy, onMount } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import Live from './Live.svelte'
  import { t } from '../../lib/i18n'
  import type { ScriptAsk } from '../../lib/scripting'
  import { askControl } from './ask/controls'
  import '../../theme/controls.css'

  /** The question. */
  export let ask: ScriptAsk

  const dispatch = createEventDispatcher<{ answer: unknown; cancel: void }>()
  const control = askControl(ask.control)

  let value: unknown = ask.default ?? control.empty
  let left = ask.timeoutSeconds
  let timer: ReturnType<typeof setInterval> | undefined

  const promptId = `ask-prompt-${Math.random().toString(36).slice(2, 9)}`
  let spoken = ''
  let promptEl: HTMLElement

  // the countdown is read out when it starts and at 30 and 10 seconds, not
  // every tick.
  function speak(n: number): void {
    const key = ask.default != null ? 'scripts.ask.timeoutDefault' : 'scripts.ask.timeoutStop'
    spoken = $t(key).replace('{n}', String(n))
  }

  onMount(() => {
    promptEl?.closest('[role="dialog"]')?.setAttribute('aria-describedby', promptId)
    if (left > 0) {
      speak(left)
      timer = setInterval(() => {
        left = Math.max(0, left - 1)
        if (left === 30 || left === 10) {
          speak(left)
        }
      }, 1000)
    }
  })

  onDestroy(() => clearInterval(timer))

  function answer(v: unknown): void {
    dispatch('answer', v)
  }
</script>

<Modal title={$t('scripts.ask.title').replace('{name}', ask.scriptName)} size="small" on:close={() => dispatch('cancel')}>
  <div class="body">
    {#if ask.dry}<p class="badge">{$t('scripts.ask.dry')}</p>{/if}
    <p class="prompt" id={promptId} bind:this={promptEl}>{ask.prompt}</p>
    {#if control.component}
      <svelte:component this={control.component} {ask} bind:value on:answer={(e) => answer(e.detail)} />
    {/if}
    <Live text={spoken} />
    {#if ask.timeoutSeconds > 0}
      <p class="hint">{$t(ask.default != null ? 'scripts.ask.timeoutDefault' : 'scripts.ask.timeoutStop').replace('{n}', String(left))}</p>
    {/if}
  </div>
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" on:click={() => dispatch('cancel')}>{$t('scripts.ask.cancel')}</button>
    {#if control.yesNo}
      <button type="button" class="cal-btn" on:click={() => answer(false)}>{$t('scripts.ask.no')}</button>
      <button type="button" class="cal-btn primary" on:click={() => answer(true)}>{$t('scripts.ask.yes')}</button>
    {:else}
      <button type="button" class="cal-btn primary" disabled={value === undefined || value === null} on:click={() => answer(value)}>{$t('scripts.ask.ok')}</button>
    {/if}
  </svelte:fragment>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .prompt {
    margin: 0;
    font-size: var(--fz-body);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .badge {
    align-self: flex-start;
    margin: 0;
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }
</style>
