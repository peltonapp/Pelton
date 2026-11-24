<script lang="ts">
  // the create/rename/delete mailbox dialog (#132), which also confirms emptying
  // the trash. they share a shape: a folder, a confirmation and a server round
  // trip that can fail. deleting a mailbox asks for its name to be typed rather
  // than offering a plain confirm button; emptying the trash only confirms,
  // since the messages in there have already been deleted once.
  import { IconAlertTriangle } from '@tabler/icons-svelte'
  import Modal from '../common/Modal.svelte'
  import '../../theme/controls.css'
  import { folderDialog, closeFolderDialog } from '../../stores/folderdialog'
  import {
    createFolder,
    renameFolder,
    deleteFolder,
    emptyTrash,
    setFolderRole,
    assignableFolderRoles,
  } from '../../lib/api'
  import { refreshSidebar } from '../../stores/accounts'
  import { selection, selectView, openMessageId } from '../../stores/selection'
  import { loadList } from '../../stores/messages'
  import { toastError, toastInfo, errorMessage } from '../../stores/toast'
  import { t } from '../../lib/i18n'

  let name = ''
  let confirmName = ''
  let busy = false
  // the role to assign, '' meaning "detect automatically".
  let role = ''
  // the request the input was seeded from, so reopening the dialog for a
  // different folder reseeds it but typing does not fight the reactive block.
  let seededFor: unknown = null

  $: request = $folderDialog
  $: if (request !== seededFor) {
    seededFor = request
    name = request?.mode === 'rename' ? (request.folder?.name ?? '') : ''
    confirmName = ''
    role = request?.folder?.roleOverride ?? ''
    busy = false
  }

  $: title =
    request?.mode === 'create'
      ? request.folder
        ? $t('folders.createSubtitle').replace('{parent}', request.folder.name)
        : $t('folders.createTitle')
      : request?.mode === 'rename'
        ? $t('folders.renameTitle')
        : request?.mode === 'empty'
          ? $t('folders.emptyTrashTitle')
          : request?.mode === 'role'
            ? $t('folders.roleTitle').replace('{name}', request.folder?.name ?? '')
            : $t('folders.deleteTitle')

  // delete needs the exact folder name typed back; empty only needs the confirm
  // button; the others just need a name that is non-empty and, for rename,
  // actually different.
  $: canSubmit =
    request?.mode === 'delete'
      ? confirmName.trim() === request.folder?.name
      : request?.mode === 'empty'
        ? true
        : request?.mode === 'role'
          ? role !== (request.folder?.roleOverride ?? '')
          : name.trim() !== '' && !(request?.mode === 'rename' && name.trim() === request.folder?.name)

  function close(): void {
    closeFolderDialog()
  }

  async function submit(): Promise<void> {
    if (!request || busy || !canSubmit) {
      return
    }
    busy = true
    try {
      if (request.mode === 'create') {
        await createFolder(request.accountId, request.folder?.id ?? 0, name.trim())
        toastInfo($t('folders.created').replace('{name}', name.trim()))
      } else if (request.mode === 'rename') {
        await renameFolder(request.folder!.id, name.trim())
        toastInfo($t('folders.renamed').replace('{name}', name.trim()))
      } else if (request.mode === 'role') {
        const target = request.folder!
        await setFolderRole(target.id, role)
        toastInfo(
          role === ''
            ? $t('folders.roleCleared').replace('{name}', target.name)
            : $t('folders.roleSet')
                .replace('{name}', target.name)
                .replace('{role}', $t(`folders.role.${role}`)),
        )
      } else if (request.mode === 'empty') {
        const emptied = request.folders ?? [request.folder!]
        let removed = 0
        for (const folder of emptied) {
          removed += await emptyTrash(folder.id)
        }
        // the rows are hidden from the store's queries the moment they are
        // marked, but the list already in memory still holds them, so a trash
        // the user is looking at has to be re-read rather than left stale.
        const looking =
          ($selection.kind === 'folder' && emptied.some((f) => f.id === $selection.folderId)) ||
          ($selection.kind === 'view' && $selection.view === 'trash')
        if (looking) {
          // anything open came from the folder just emptied, so the reading
          // pane would be showing a message that no longer exists.
          openMessageId.set(null)
          await loadList($selection)
        }
        toastInfo($t('folders.trashEmptied').replace('{count}', String(removed)))
      } else {
        const gone = request.folder!
        await deleteFolder(gone.id)
        // the deleted folder cannot stay selected: its messages are gone and the
        // list would keep querying a folder id that no longer exists.
        if ($selection.kind === 'folder' && $selection.folderId === gone.id) {
          selectView('inbox', $t('sidebar.unifiedInbox'))
        }
        toastInfo($t('folders.deleted').replace('{name}', gone.name))
      }
      await refreshSidebar()
      close()
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      busy = false
    }
  }

  $: dirty = request?.mode === 'rename' ? name.trim() !== (request.folder?.name ?? '') : request?.mode === 'create' && name.trim() !== ''
</script>

{#if request}
  <Modal {title} size="small" closable={!busy} {dirty} on:close={close}>
  <form class="body" id="folder-form" on:submit|preventDefault={() => void submit()}>
    {#if request.mode === 'delete'}
      <div class="warn">
        <span class="warn-icon"><IconAlertTriangle size={17} stroke={1.8} /></span>
        <p>{$t('folders.deleteWarning').replace('{name}', request.folder?.name ?? '')}</p>
      </div>
      <label class="field">
        <span>{$t('folders.deleteConfirmLabel').replace('{name}', request.folder?.name ?? '')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input type="text" bind:value={confirmName} autofocus spellcheck="false" />
      </label>
    {:else if request.mode === 'empty'}
      <div class="warn">
        <span class="warn-icon"><IconAlertTriangle size={17} stroke={1.8} /></span>
        <p>
          {$t('folders.emptyTrashWarning')
            .replace('{count}', String((request.folders ?? [request.folder]).reduce((n, f) => n + (f?.totalCount ?? 0), 0)))
            .replace('{name}', request.folders ? $t('sidebar.view.trash') : (request.folder?.name ?? ''))}
        </p>
      </div>
    {:else if request.mode === 'role'}
      <p class="hint">{$t('folders.roleHint')}</p>
      <div class="roles" role="radiogroup" aria-label={title}>
        <!-- "detect automatically" is first and is the default, so the picker
             reads as an override of what Pelton worked out rather than the only
             way a folder ever gets a role. -->
        <label class="role">
          <input type="radio" bind:group={role} value="" />
          <span class="role-name">{$t('folders.roleAuto')}</span>
          <span class="role-note">{$t(`folders.role.${request.folder?.role ?? 'normal'}`)}</span>
        </label>
        {#each assignableFolderRoles as option}
          <label class="role">
            <input type="radio" bind:group={role} value={option} />
            <span class="role-name">{$t(`folders.role.${option}`)}</span>
          </label>
        {/each}
      </div>
    {:else}
      <label class="field">
        <span>{$t('folders.nameLabel')}</span>
        <!-- svelte-ignore a11y-autofocus -->
        <input
          type="text"
          bind:value={name}
          autofocus
          spellcheck="false"
          placeholder={$t('folders.namePlaceholder')}
        />
      </label>
    {/if}
  </form>

  <svelte:fragment slot="footer">
    <button type="button" class="cal-btn" disabled={busy} on:click={close}>{$t('folders.cancel')}</button>
    <button
      type="submit"
      form="folder-form"
      class="cal-btn primary"
      class:danger={request.mode === 'delete' || request.mode === 'empty'}
      disabled={!canSubmit || busy}
    >
      {#if request.mode === 'delete'}
        {$t('action.delete')}
      {:else if request.mode === 'empty'}
        {$t('folders.emptyTrashConfirm')}
      {:else}
        {$t('folders.save')}
      {/if}
    </button>
  </svelte:fragment>
  </Modal>
{/if}

<style>
  .body {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .warn {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-3);
    border-radius: var(--radius-control);
    background: var(--danger-bg);
  }

  .warn-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--danger);
  }

  .warn p {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .hint {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .roles {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .role {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-control);
    cursor: var(--cursor-action);
  }
  .role:hover {
    background: var(--surface-hover);
  }

  .role-name {
    font-size: var(--fz-body);
    color: var(--text-primary);
  }

  /* what detection currently resolves to, so "automatic" is not a blind choice. */
  .role-note {
    margin-inline-start: auto;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .field span {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
  }

  .field input {
    padding: var(--space-2) var(--space-3);
    font-family: var(--font-ui);
    font-size: var(--fz-body);
    color: var(--text-primary);
    background: var(--surface-raised);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
  }

  .field input:focus {
    outline: none;
    border-color: var(--accent);
  }

</style>
