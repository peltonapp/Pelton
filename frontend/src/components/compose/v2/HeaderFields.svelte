<script lang="ts">
  // the header of the new composer: borderless rows on one label column,
  // split by hairlines. From is a compact account picker shown only with more
  // than one account; Cc and Bcc are revealed from small buttons at the end of
  // the To row; Subject is the larger last line. Every edit goes straight to
  // the session store.
  import { slide } from 'svelte/transition'
  import ChipInput from '../ChipInput.svelte'
  import Select from '../../common/Select.svelte'
  import { updateCompose, type ComposeSession } from '../../../stores/compose'
  import { sidebar } from '../../../stores/accounts'
  import { prefs } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'

  /** The session the fields edit. */
  export let session: ComposeSession

  $: accounts = $sidebar.data?.accounts ?? []
  $: motion = $prefs.reduceMotion ? 0 : 170

  function accountLabel(acc: { email: string; displayName?: string }): string {
    return acc.displayName ? `${acc.displayName} <${acc.email}>` : acc.email
  }
</script>

<div class="fields">
  {#if accounts.length > 1}
    <div class="row">
      <label for={`from-${session.id}`}>{$t('compose.field.from')}</label>
      <Select
        id={`from-${session.id}`}
        class="c2-from"
        value={String(session.accountId)}
        items={accounts.map((acc) => ({ value: String(acc.id), label: accountLabel(acc) }))}
        on:change={(e) => updateCompose(session.id, { accountId: Number(e.detail) })}
      />
    </div>
  {/if}

  <div class="row">
    <label for={`to-${session.id}`}>{$t('compose.field.to')}</label>
    <div class="value">
      <ChipInput
        id={`to-${session.id}`}
        label={$t('compose.field.to')}
        value={session.to}
        chipsEnabled={$prefs.composeChips}
        autocompleteEnabled={$prefs.composeAutocomplete}
        on:change={(e) => updateCompose(session.id, { to: e.detail })}
      />
    </div>
    {#if !session.showCc || !session.showBcc}
      <div class="reveal">
        {#if !session.showCc}
          <button type="button" on:click={() => updateCompose(session.id, { showCc: true })}>{$t('compose.field.cc')}</button>
        {/if}
        {#if !session.showBcc}
          <button type="button" on:click={() => updateCompose(session.id, { showBcc: true })}>{$t('compose.field.bcc')}</button>
        {/if}
      </div>
    {/if}
  </div>

  {#if session.showCc}
    <div class="row" transition:slide={{ duration: motion }}>
      <label for={`cc-${session.id}`}>{$t('compose.field.cc')}</label>
      <div class="value">
        <ChipInput
          id={`cc-${session.id}`}
          label={$t('compose.field.cc')}
          value={session.cc}
          chipsEnabled={$prefs.composeChips}
          autocompleteEnabled={$prefs.composeAutocomplete}
          on:change={(e) => updateCompose(session.id, { cc: e.detail })}
        />
      </div>
    </div>
  {/if}

  {#if session.showBcc}
    <div class="row" transition:slide={{ duration: motion }}>
      <label for={`bcc-${session.id}`}>{$t('compose.field.bcc')}</label>
      <div class="value">
        <ChipInput
          id={`bcc-${session.id}`}
          label={$t('compose.field.bcc')}
          value={session.bcc}
          chipsEnabled={$prefs.composeChips}
          autocompleteEnabled={$prefs.composeAutocomplete}
          on:change={(e) => updateCompose(session.id, { bcc: e.detail })}
        />
      </div>
    </div>
  {/if}

  <div class="row subject">
    <label for={`subject-${session.id}`}>{$t('compose.field.subject')}</label>
    <input
      id={`subject-${session.id}`}
      type="text"
      autocomplete="off"
      value={session.subject}
      on:input={(e) => updateCompose(session.id, { subject: e.currentTarget.value })}
    />
  </div>
</div>

<style>
  .fields {
    display: flex;
    flex-direction: column;
    padding: 0 var(--pane-pad);
  }

  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    min-height: var(--control-height);
    padding: calc(var(--row-pad-y) / 2) 0;
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  label {
    width: var(--compose-label-width);
    flex-shrink: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .value {
    display: flex;
    flex: 1;
    min-width: 0;
  }

  input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    outline: none;
    color: var(--text-primary);
    font-family: var(--font-ui);
    font-size: var(--fz-body);
  }

  .subject {
    padding: var(--row-pad-y) 0;
  }

  .subject input {
    font-size: var(--fz-heading);
    font-weight: var(--fw-medium);
  }

  .reveal {
    display: flex;
    gap: var(--space-1);
    flex-shrink: 0;
  }

  .reveal button {
    padding: var(--space-1) var(--space-3);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    font-family: var(--font-ui);
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .reveal button:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .row :global(.c2-from) {
    flex: 1;
    min-width: 0;
    border-color: transparent;
    background: transparent;
    padding-inline: 0;
    font-size: var(--fz-body);
  }

  .row :global(.c2-from:hover) {
    border-color: var(--border-subtle);
  }
</style>
