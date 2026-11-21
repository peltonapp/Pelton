<script lang="ts">
  // the to/cc/bcc and subject inputs for a compose session. recipients render as
  // chips; cc and bcc are hidden until revealed. changes write straight back to
  // the session store.
  import ChipInput from './ChipInput.svelte'
  import { updateCompose, type ComposeSession } from '../../stores/compose'
  import { prefs } from '../../stores/prefs'
  import { t } from '../../lib/i18n'

  export let session: ComposeSession
</script>

<div class="fields">
  <div class="field">
    <label for={`to-${session.id}`}>{$t('compose.field.to')}</label>
    <ChipInput
      id={`to-${session.id}`}
      label={$t('compose.field.to')}
      value={session.to}
      chipsEnabled={$prefs.composeChips}
      autocompleteEnabled={$prefs.composeAutocomplete}
      on:change={(e) => updateCompose(session.id, { to: e.detail })}
    />
    <div class="reveal">
      {#if !session.showCc}
        <button type="button" on:click={() => updateCompose(session.id, { showCc: true })}>{$t('compose.field.cc')}</button>
      {/if}
      {#if !session.showBcc}
        <button type="button" on:click={() => updateCompose(session.id, { showBcc: true })}>{$t('compose.field.bcc')}</button>
      {/if}
    </div>
  </div>

  {#if session.showCc}
    <div class="field">
      <label for={`cc-${session.id}`}>{$t('compose.field.cc')}</label>
      <ChipInput
        id={`cc-${session.id}`}
        label={$t('compose.field.cc')}
        value={session.cc}
        chipsEnabled={$prefs.composeChips}
        autocompleteEnabled={$prefs.composeAutocomplete}
        on:change={(e) => updateCompose(session.id, { cc: e.detail })}
      />
    </div>
  {/if}

  {#if session.showBcc}
    <div class="field">
      <label for={`bcc-${session.id}`}>{$t('compose.field.bcc')}</label>
      <ChipInput
        id={`bcc-${session.id}`}
        label={$t('compose.field.bcc')}
        value={session.bcc}
        chipsEnabled={$prefs.composeChips}
        autocompleteEnabled={$prefs.composeAutocomplete}
        on:change={(e) => updateCompose(session.id, { bcc: e.detail })}
      />
    </div>
  {/if}

  <div class="field">
    <label for={`subject-${session.id}`}>{$t('compose.field.subject')}</label>
    <input
      id={`subject-${session.id}`}
      type="text"
      value={session.subject}
      on:input={(e) => updateCompose(session.id, { subject: e.currentTarget.value })}
    />
  </div>
</div>

<style>
  .fields {
    display: flex;
    flex-direction: column;
  }

  .field {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
    padding: var(--space-2) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  label {
    width: 52px;
    flex-shrink: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
    /* nudge the label to baseline with the first chip/input row. */
    padding-top: 3px;
  }

  input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    outline: none;
    font-size: var(--fz-body);
  }

  .reveal {
    display: flex;
    gap: var(--space-2);
    flex-shrink: 0;
  }

  .reveal button {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-control);
  }

  .reveal button:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }
</style>
