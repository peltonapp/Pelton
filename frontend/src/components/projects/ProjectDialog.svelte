<script lang="ts">
  // the editor of a project: its name, what it is about, its color and icon,
  // when it starts and is due, and the task list its cards are made in. a new
  // project also picks how its board starts: with the usual three columns or
  // empty.
  import { createEventDispatcher } from 'svelte'
  import closeIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import '../../theme/controls.css'
  import Modal from '../common/Modal.svelte'
  import Ph from '../common/Ph.svelte'
  import Select from '../common/Select.svelte'
  import ColorSwatches from '../calendar/ColorSwatches.svelte'
  import DateField from '../calendar/DateField.svelte'
  import { paletteHex } from '../../lib/calformat'
  import { dayKey, parseDayKey } from '../../lib/caldate'
  import { projectIcon, projectIcons } from '../../lib/projecticons'
  import { projectColor } from '../../lib/projectlogic'
  import { taskLists } from '../../stores/tasks'
  import { saveProject } from '../../stores/projects'
  import { t } from '../../lib/i18n'
  import type { Project, ProjectDraft } from '../../lib/types'

  /** The project edited, or null for a new one. */
  export let project: Project | null = null
  /** The first day of the week for the date pickers. */
  export let weekStart: number = 1

  const dispatch = createEventDispatcher<{ close: void; saved: Project }>()

  let name = project?.name ?? ''
  let description = project?.description ?? ''
  let color = project?.color ?? ''
  let icon = project?.icon ?? ''
  let startDate = project?.startDate ?? ''
  let dueDate = project?.dueDate ?? ''
  let listId = String(project?.listId ?? 0)
  let template: 'columns' | 'empty' = 'columns'
  let saving = false

  $: dirty = project
    ? name !== project.name || description !== project.description || color !== project.color || icon !== project.icon || startDate !== project.startDate || dueDate !== project.dueDate || Number(listId) !== project.listId
    : name.trim() !== '' || description.trim() !== ''
  $: swatch = projectColor({ id: project?.id ?? 0, color })
  $: listItems = [
    { value: '0', label: $t('projects.dialog.defaultList') },
    ...$taskLists.filter((l) => !l.readOnly).map((l) => ({ value: String(l.id), label: l.name || $t('tasks.defaultList') })),
  ]
  $: templateItems = [
    { value: 'columns', label: $t('projects.dialog.templateColumns') },
    { value: 'empty', label: $t('projects.dialog.templateEmpty') },
  ]

  async function submit(): Promise<void> {
    if (!name.trim() || saving) {
      return
    }
    saving = true
    const draft: ProjectDraft = {
      id: project?.id ?? 0,
      name: name.trim(),
      color,
      icon,
      description: description.trim(),
      startDate,
      dueDate,
      listId: Number(listId),
      columns:
        project || template === 'empty'
          ? []
          : [
              { name: $t('projects.column.todo'), wipLimit: 0, done: false },
              { name: $t('projects.column.doing'), wipLimit: 0, done: false },
              { name: $t('projects.column.done'), wipLimit: 0, done: true },
            ],
    }
    const saved = await saveProject(draft)
    saving = false
    if (saved) {
      dispatch('saved', saved)
      dispatch('close')
    }
  }
</script>

<Modal title={project ? $t('projects.dialog.editTitle') : $t('projects.dialog.newTitle')} size="medium" {dirty} on:close>
  <form class="form" id="project-form" on:submit|preventDefault={submit}>
    <div class="name-row">
      <span class="mark" style={`--project:${swatch}`} aria-hidden="true">
        <Ph svg={projectIcon(icon)} size={18} />
      </span>
      <!-- svelte-ignore a11y-autofocus -->
      <input
        class="cal-field name"
        bind:value={name}
        placeholder={$t('projects.dialog.namePlaceholder')}
        aria-label={$t('projects.dialog.name')}
        autofocus
        maxlength="120"
      />
    </div>

    <label class="field">
      <span class="label">{$t('projects.dialog.description')}</span>
      <textarea class="cal-field" rows="3" bind:value={description} placeholder={$t('projects.dialog.descriptionPlaceholder')}></textarea>
    </label>

    <div class="field">
      <span class="label">{$t('projects.dialog.color')}</span>
      <ColorSwatches value={color} on:pick={(e) => (color = paletteHex(e.detail))} />
    </div>

    <div class="field">
      <span class="label" id="project-icon-label">{$t('projects.dialog.icon')}</span>
      <div class="icons" role="radiogroup" aria-labelledby="project-icon-label">
        {#each projectIcons as i (i.name)}
          <button
            type="button"
            class="icon"
            class:on={(icon || 'kanban') === i.name}
            role="radio"
            aria-checked={(icon || 'kanban') === i.name}
            aria-label={$t(`projects.icon.${i.name}`)}
            title={$t(`projects.icon.${i.name}`)}
            style={`--project:${swatch}`}
            on:click={() => (icon = i.name === 'kanban' ? '' : i.name)}
          >
            <Ph svg={i.svg} size={16} />
          </button>
        {/each}
      </div>
    </div>

    <div class="dates">
      <div class="field">
        <span class="label">{$t('projects.dialog.start')}</span>
        <div class="date">
          {#if startDate}
            <DateField value={parseDayKey(startDate)} {weekStart} label={$t('projects.dialog.start')} on:change={(e) => (startDate = dayKey(e.detail))} />
            <button type="button" class="cal-icon" aria-label={$t('projects.dialog.clearStart')} on:click={() => (startDate = '')}>
              <Ph svg={closeIcon} size={12} />
            </button>
          {:else}
            <button type="button" class="link" on:click={() => (startDate = dayKey(new Date()))}>{$t('projects.dialog.addDate')}</button>
          {/if}
        </div>
      </div>
      <div class="field">
        <span class="label">{$t('projects.dialog.due')}</span>
        <div class="date">
          {#if dueDate}
            <DateField value={parseDayKey(dueDate)} {weekStart} label={$t('projects.dialog.due')} on:change={(e) => (dueDate = dayKey(e.detail))} />
            <button type="button" class="cal-icon" aria-label={$t('projects.dialog.clearDue')} on:click={() => (dueDate = '')}>
              <Ph svg={closeIcon} size={12} />
            </button>
          {:else}
            <button type="button" class="link" on:click={() => (dueDate = dayKey(new Date()))}>{$t('projects.dialog.addDate')}</button>
          {/if}
        </div>
      </div>
    </div>

    <div class="pair">
      <div class="field">
        <span class="label">{$t('projects.dialog.list')}</span>
        <Select value={listId} items={listItems} ariaLabel={$t('projects.dialog.list')} on:change={(e) => (listId = e.detail)} />
      </div>
      {#if !project}
        <div class="field">
          <span class="label">{$t('projects.dialog.board')}</span>
          <Select value={template} items={templateItems} ariaLabel={$t('projects.dialog.board')} on:change={(e) => (template = e.detail === 'empty' ? 'empty' : 'columns')} />
        </div>
      {/if}
    </div>
  </form>

  <svelte:fragment slot="footer">
    <div class="actions">
      <button type="button" class="cal-btn" on:click={() => dispatch('close')}>{$t('projects.dialog.cancel')}</button>
      <button type="submit" form="project-form" class="cal-btn primary" disabled={!name.trim() || saving}>
        {project ? $t('projects.dialog.save') : $t('projects.dialog.create')}
      </button>
    </div>
  </svelte:fragment>
</Modal>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }

  .name-row {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }

  .mark {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    width: calc(var(--control-height) + 2px);
    height: calc(var(--control-height) + 2px);
    border-radius: var(--radius-control);
    background: color-mix(in srgb, var(--project) 16%, transparent);
    color: var(--project);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .name {
    flex: 1;
    font-size: var(--fz-body);
    font-weight: var(--fw-medium);
  }

  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    min-width: 0;
  }

  .label {
    font-size: var(--fz-label);
    font-weight: var(--fw-medium);
    color: var(--text-secondary);
  }

  .icons {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }

  .icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: calc(var(--control-height) - 2px);
    height: calc(var(--control-height) - 2px);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .icon:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .icon.on {
    border-color: var(--border-default);
    background: color-mix(in srgb, var(--project) 14%, transparent);
    color: var(--project);
  }

  .icon:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .dates,
  .pair {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(calc(var(--popover-width) * 0.6), 1fr));
    gap: var(--space-4);
  }

  .date {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: var(--control-height);
  }

  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--link);
    font: inherit;
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .link:hover {
    text-decoration: underline;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
