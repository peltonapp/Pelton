<script lang="ts">
  // what the right pane (or the grid's drawer) holds: the editor when one is
  // open, else the selected contact, else a hint to pick one.
  import { createEventDispatcher } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import ContactDetail from './ContactDetail.svelte'
  import ContactForm from './ContactForm.svelte'
  import bookIcon from '@phosphor-icons/core/assets/duotone/address-book-duotone.svg?raw'
  import backIcon from '@phosphor-icons/core/assets/regular/arrow-left.svg?raw'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import { t } from '../../lib/i18n'
  import type { Contact, ContactDraft, MessageSummary } from '../../lib/types'

  /** The contact to show, or null for none. */
  export let contact: Contact | null
  /** The contact being edited or created, which takes the place of the detail. */
  export let draft: ContactDraft | null
  /** A way out above the content, for the narrow and grid layouts. */
  export let dismiss: 'back' | 'close' | null = null

  const dispatch = createEventDispatcher<{
    compose: string
    edit: Contact
    closeEditor: void
    dirty: boolean
    dismiss: void
    openMail: MessageSummary
  }>()
</script>

<div class="pane">
  {#if draft}
    {#if dismiss}
      <div class="top">
        <button type="button" class="icon" aria-label={$t('action.close')} on:click={() => dispatch('dismiss')}>
          <Ph svg={dismiss === 'back' ? backIcon : closeIcon} size={16} />
        </button>
      </div>
    {/if}
    {#key draft.id}
      <ContactForm {draft} on:close={() => dispatch('closeEditor')} on:dirty={(e) => dispatch('dirty', e.detail)} />
    {/key}
  {:else if contact}
    <ContactDetail
      {contact}
      {dismiss}
      on:compose={(e) => dispatch('compose', e.detail)}
      on:edit={(e) => dispatch('edit', e.detail)}
      on:dismiss={() => dispatch('dismiss')}
      on:openMail={(e) => dispatch('openMail', e.detail)}
    />
  {:else}
    <div class="empty">
      <Ph svg={bookIcon} size={32} />
      <p>{$t('contacts.pickOne')}</p>
    </div>
  {/if}
</div>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    overflow-y: auto;
    background: var(--surface-raised);
  }

  .pane > :global(.detail),
  .pane > :global(.form) {
    flex: 1;
  }

  .top {
    display: flex;
    padding: var(--space-2) var(--space-3) 0;
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--control-height);
    height: var(--control-height);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-5);
    color: var(--text-tertiary);
    font-size: var(--fz-body);
  }

  .empty p {
    margin: 0;
  }
</style>
