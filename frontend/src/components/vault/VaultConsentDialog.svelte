<script lang="ts">
  // asked once, before the first file from a vault is written to the private
  // temporary folder an outside app can open it from.
  import { createEventDispatcher, onDestroy } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import { t } from '../../lib/i18n'

  /** Called with the user's answer; a close without one is a no. */
  export let answer: (yes: boolean) => void

  const dispatch = createEventDispatcher<{ close: void }>()
  let answered = false

  function reply(yes: boolean): void {
    answered = true
    answer(yes)
    dispatch('close')
  }

  onDestroy(() => {
    if (!answered) {
      answer(false)
    }
  })
</script>

<Modal title={$t('mailvault.temp.title')} size="small" on:close={() => reply(false)}>
  <p class="body">{$t('mailvault.temp.body')}</p>
  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" on:click={() => reply(false)}>{$t('settingsPanel.button.cancel')}</button>
    <button type="button" class="cal-btn primary" on:click={() => reply(true)}>{$t('mailvault.temp.open')}</button>
  </svelte:fragment>
</Modal>

<style>
  .body {
    margin: 0;
    font-size: var(--fz-body);
    line-height: 1.5;
    color: var(--text-secondary);
  }
</style>
