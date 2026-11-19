<script lang="ts">
  // says which scripts a library change switched off, and walks through them
  // one at a time: what changed in the library since each was approved, what
  // it may do, and a button to approve it and switch it back on.
  import { createEventDispatcher } from 'svelte'
  import Modal from '../common/Modal.svelte'
  import PermissionList from './PermissionList.svelte'
  import LibraryDiff from './LibraryDiff.svelte'
  import { t } from '../../lib/i18n'
  import { scriptLibraryNote } from '../../lib/api'
  import type { ScriptCatalog, ScriptLibraryNote } from '../../lib/scripting'
  import { approveAndEnable, scripts, type LibraryReview } from '../../stores/scripts'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import '../../theme/controls.css'

  /** The library change and the scripts it switched off. */
  export let review: LibraryReview
  /** The registry. */
  export let catalog: ScriptCatalog

  const dispatch = createEventDispatcher<{ close: void }>()

  let step = -1
  let busy = false
  let note: ScriptLibraryNote | null = null
  let approved = 0

  $: current = step >= 0 ? review.scripts[step] : null
  $: script = current ? ($scripts.find((s) => s.id === current.id) ?? null) : null
  $: void loadNote(current?.id ?? 0)

  async function loadNote(id: number): Promise<void> {
    note = null
    if (id === 0) {
      return
    }
    try {
      const n = await scriptLibraryNote(id)
      if (current?.id === id) {
        note = n.exists ? n : null
      }
    } catch {
      note = null
    }
  }

  function next(): void {
    if (step + 1 < review.scripts.length) {
      step += 1
      return
    }
    if (approved > 0) {
      toastSuccess($t(approved === 1 ? 'scripts.library.review.done.one' : 'scripts.library.review.done').replace('{n}', String(approved)))
    }
    dispatch('close')
  }

  async function approve(): Promise<void> {
    if (!script) {
      next()
      return
    }
    busy = true
    try {
      await approveAndEnable(script)
      approved += 1
      next()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  $: title = current
    ? $t('scripts.library.review.stepTitle').replace('{name}', current.name).replace('{i}', String(step + 1)).replace('{n}', String(review.scripts.length))
    : $t('scripts.library.review.title').replace('{name}', review.library)
</script>

<Modal {title} size="large" on:close>
  <div class="body">
    {#if !current}
      <p class="lead">{$t('scripts.library.review.body').replace('{name}', review.library)}</p>
      <ul class="names">
        {#each review.scripts as s (s.id)}
          <li>{s.name}</li>
        {/each}
      </ul>
    {:else}
      <h3 class="heading">{$t('scripts.library.review.changed').replace('{name}', review.library)}</h3>
      {#if note}
        <LibraryDiff {note} />
      {/if}
      {#if script}
        <h3 class="heading">{$t('scripts.permissions')}</h3>
        <PermissionList {catalog} declared={script.permissions} approved={script.approved} />
      {:else}
        <p class="lead">{$t('scripts.library.review.gone')}</p>
      {/if}
    {/if}
  </div>
  <svelte:fragment slot="footer">
    {#if !current}
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('scripts.library.review.later')}</button>
      <button type="button" class="cal-btn primary" on:click={() => (step = 0)}>{$t('scripts.library.review.start')}</button>
    {:else}
      <button type="button" class="cal-btn" disabled={busy} on:click={next}>{$t('scripts.library.review.skip')}</button>
      <button type="button" class="cal-btn primary" disabled={busy || !script} on:click={() => void approve()}>{$t('scripts.library.review.approve')}</button>
    {/if}
  </svelte:fragment>
</Modal>

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }

  .lead {
    margin: 0;
  }

  .names {
    margin: 0;
    padding-inline-start: var(--space-5);
  }

  .heading {
    margin: 0;
    font-size: var(--fz-label);
    font-weight: var(--fw-semibold);
  }
</style>
