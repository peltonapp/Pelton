<script lang="ts">
  // the Tasks page: which list a new task goes to and the time a due date
  // gets, what the rail badge counts, whether tasks show in the calendar, how
  // long completed tasks stay listed, and the order lists are sorted in.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import Select from '../../common/Select.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import PromiseSettings from '../PromiseSettings.svelte'
  import { loadTaskSettings, loadTasks, saveTaskSettings, taskLists, taskSettings } from '../../../stores/tasks'
  import { atMinutes } from '../../../lib/caldate'
  import { formatTime } from '../../../lib/calformat'
  import { prefs } from '../../../stores/prefs'
  import { t } from '../../../lib/i18n'
  import type { TimeFormat } from '../../../lib/format'
  import type { TaskBadge, TaskSort } from '../../../lib/types'

  onMount(() => {
    void loadTaskSettings()
    void loadTasks()
  })

  $: s = $taskSettings

  $: listItems = [
    { value: '0', label: $t('taskSettings.defaultList.first') },
    ...$taskLists.filter((l) => !l.readOnly).map((l) => ({ value: String(l.id), label: l.name || $t('tasks.defaultList') })),
  ]

  $: dueTimeItems = [
    { value: '-1', label: $t('taskSettings.dueTime.none') },
    ...Array.from({ length: 48 }, (_, i) => i * 30).map((m) => ({ value: String(m), label: formatTime(atMinutes(new Date(2026, 0, 5), m), $prefs.timeFormat as TimeFormat) })),
  ]

  $: badgeOptions = [
    { key: 'overdue', label: $t('taskSettings.badge.overdue') },
    { key: 'today', label: $t('taskSettings.badge.today') },
    { key: 'off', label: $t('taskSettings.badge.off') },
  ]

  $: completedItems = [
    { value: '-1', label: $t('taskSettings.completed.keep') },
    ...[0, 7, 30, 90].map((n) => ({ value: String(n), label: n === 0 ? $t('taskSettings.completed.now') : $t('taskSettings.completed.after').replace('{n}', String(n)) })),
  ]

  $: sortItems = (['manual', 'due', 'priority', 'title', 'created'] as TaskSort[]).map((key) => ({ value: key, label: $t(`tasks.sort.${key}`) }))
</script>

<Card title={$t('taskSettings.newTasks')}>
  <Row id="tasks-default-list" label={$t('taskSettings.defaultList')} hint={$t('taskSettings.defaultList.hint')}>
    <Select value={String(s.defaultListId)} items={listItems} ariaLabel={$t('taskSettings.defaultList')} on:change={(e) => saveTaskSettings({ defaultListId: Number(e.detail) })} />
  </Row>
  <Row id="tasks-due-time" label={$t('taskSettings.dueTime')} hint={$t('taskSettings.dueTime.hint')}>
    <Select value={String(s.defaultDueMinutes)} items={dueTimeItems} ariaLabel={$t('taskSettings.dueTime')} on:change={(e) => saveTaskSettings({ defaultDueMinutes: Number(e.detail) })} />
  </Row>
</Card>

<Card title={$t('taskSettings.display')}>
  <Row id="tasks-badge" label={$t('taskSettings.badge')} hint={$t('taskSettings.badge.hint')}>
    <Segmented value={s.badge} options={badgeOptions} label={$t('taskSettings.badge')} on:change={(e) => saveTaskSettings({ badge: e.detail as TaskBadge })} />
  </Row>
  <Row id="tasks-in-calendar" label={$t('taskSettings.inCalendar')} hint={$t('taskSettings.inCalendar.hint')}>
    <ToggleSwitch checked={s.showInCalendar} label={$t('taskSettings.inCalendar')} on:change={(e) => saveTaskSettings({ showInCalendar: e.detail })} />
  </Row>
  <Row id="tasks-completed" label={$t('taskSettings.completed')} hint={$t('taskSettings.completed.hint')}>
    <Select value={String(s.completedDays)} items={completedItems} ariaLabel={$t('taskSettings.completed')} on:change={(e) => saveTaskSettings({ completedDays: Number(e.detail) })} />
  </Row>
  <Row id="tasks-sort" label={$t('taskSettings.sort')} hint={$t('taskSettings.sort.hint')}>
    <Select value={s.sort} items={sortItems} ariaLabel={$t('taskSettings.sort')} on:change={(e) => saveTaskSettings({ sort: e.detail as TaskSort })} />
  </Row>
</Card>

{#if $prefs.design === 'new'}
  <PromiseSettings />
{/if}
