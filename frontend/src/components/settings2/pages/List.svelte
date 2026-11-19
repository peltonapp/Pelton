<script lang="ts">
  // the List page: how the message list looks and behaves. the rows at the top
  // follow every choice below, and most choices are made by picture.
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ChoiceCards from '../ui/ChoiceCards.svelte'
  import Segmented from '../ui/Segmented.svelte'
  import ListPreview from '../skeletons/ListPreview.svelte'
  import RowSkeleton from '../skeletons/RowSkeleton.svelte'
  import FlagSkeleton from '../skeletons/FlagSkeleton.svelte'
  import SwipeSkeleton from '../skeletons/SwipeSkeleton.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import {
    prefs,
    setAvatarPlacement,
    setRowTemplate,
    setRowShowSnippet,
    setPreviewLines,
    setFlagHighlight,
    setShowAccountEmail,
    setToggle,
    setTimeFormat,
    setMultiSelectEnabled,
    setShowSelectedCount,
    setSelectAllScope,
    setSelectAllUnified,
    setSearchSort,
    searchSortPref,
    setSidebarIndentGuides,
    setShowUnsyncedFolder,
    setShowFlaggedCount,
    setStartupSelection,
    setViewsPlacement,
    setAvatarSource,
    setAvatarStyle,
    setSwipeEnabled,
    setSwipeLeftAction,
    setSwipeRightAction,
  } from '../../../stores/prefs'
  import { sidebar } from '../../../stores/accounts'
  import { t } from '../../../lib/i18n'
  import { pfpDataUri, type PfpStyle } from '../../../lib/pfp'
  import { initials } from '../../../lib/format'
  import { searchSorts, automaticSort } from '../../../lib/searchsort'
  import type { SelectItem } from '../../../lib/selectnav'
  import type {
    AvatarPlacement,
    RowTemplate,
    ViewsPlacement,
    SelectAllScope,
    SearchKind,
    SearchSortPref,
  } from '../../../lib/types'

  $: snippetCapable = $prefs.rowTemplate === 'relaxed' || $prefs.rowTemplate === 'comfortable'
  $: multi = $prefs.multiSelectEnabled

  $: placements = [
    { key: 'column', label: $t('list.avatar.column'), hint: $t('list.avatar.column.hint') },
    { key: 'inline', label: $t('list.avatar.inline'), hint: $t('list.avatar.inline.hint') },
    { key: 'hidden', label: $t('list.avatar.hidden'), hint: $t('list.avatar.hidden.hint') },
  ]

  $: templates = [
    { key: 'relaxed', label: $t('onboarding.row.relaxed') },
    { key: 'comfortable', label: $t('onboarding.row.comfortable') },
    { key: 'compact', label: $t('onboarding.row.compact') },
    { key: 'single', label: $t('onboarding.row.single') },
  ]

  $: previewLineOptions = [
    { key: '1', label: $t('settingsPanel.previewLines.1') },
    { key: '2', label: $t('settingsPanel.previewLines.2') },
    { key: '3', label: $t('settingsPanel.previewLines.3') },
  ]

  $: flagOptions = [
    { key: 'flag', label: $t('onboarding.flagopt.icon') },
    { key: 'left', label: $t('onboarding.flagopt.left') },
    { key: 'both', label: $t('onboarding.flagopt.both') },
    { key: 'off', label: $t('onboarding.flagopt.off') },
  ]

  $: timeFormatOptions = [
    { key: 'auto', label: $t('settingsPanel.timeFormat.auto') },
    { key: '12', label: $t('settingsPanel.timeFormat.h12') },
    { key: '24', label: $t('settingsPanel.timeFormat.h24') },
  ]

  $: selectAllScopeItems = [
    { value: 'offer', label: $t('settingsPanel.selectAllScope.offer') },
    { value: 'all', label: $t('settingsPanel.selectAllScope.all') },
    { value: 'loaded', label: $t('settingsPanel.selectAllScope.loaded') },
  ]

  const searchSortRows: { kind: SearchKind; label: string }[] = [
    { kind: 'text', label: 'settingsPanel.searchSort.text' },
    { kind: 'dated', label: 'settingsPanel.searchSort.dated' },
    { kind: 'filtered', label: 'settingsPanel.searchSort.filtered' },
  ]

  function searchSortItems(kind: SearchKind): SelectItem[] {
    return [
      {
        value: 'auto',
        label: `${$t('settingsPanel.searchSort.auto')} (${$t(`messageList.search.sort.${automaticSort(kind)}`)})`,
      },
      ...searchSorts.map((option) => ({ value: option, label: $t(`messageList.search.sort.${option}`) })),
    ]
  }

  $: viewsPlacementOptions = [
    { key: 'hidden', label: $t('views.setting.hidden') },
    { key: 'sidebar', label: $t('views.setting.sidebar') },
    { key: 'tab', label: $t('views.setting.tab') },
  ]

  // unified view names are localized by key in the sidebar; reuse that here so
  // the setting and the row it points at read the same.
  function unifiedViewName(key: string, fallback: string): string {
    const lookup = key === 'inbox' ? 'sidebar.unifiedInbox' : `sidebar.view.${key}`
    const translated = $t(lookup)
    return translated === lookup ? fallback : translated
  }

  // "last used", then every unified view and every folder grouped by account.
  $: startupItems = [
    { value: 'last', label: $t('settingsPanel.startup.lastUsed') },
    ...($sidebar.data
      ? [
          {
            label: $t('sidebar.unifiedViews.heading'),
            options: $sidebar.data.views.map((view) => ({
              value: `view:${view.key}`,
              label: unifiedViewName(view.key, view.label),
            })),
          },
          ...$sidebar.data.accounts.map((account) => ({
            label: account.email,
            options: ($sidebar.data?.foldersByAccount[account.id] ?? []).map((folder) => ({
              value: `folder:${folder.id}`,
              label: folder.imapPath,
            })),
          })),
        ]
      : []),
  ] satisfies SelectItem[]

  $: avatarSourceItems = [
    { value: 'bimi_gravatar', label: $t('settingsPanel.avatarSource.bimiGravatar') },
    { value: 'gravatar_bimi', label: $t('settingsPanel.avatarSource.gravatarBimi') },
    { value: 'bimi', label: $t('settingsPanel.avatarSource.bimiOnly') },
    { value: 'pfp', label: $t('settingsPanel.avatarSource.generated') },
  ]

  const sampleEmail = 'potato@pelton.email'
  const sampleInitials = initials('', sampleEmail)
  $: avatarStyles = [
    { key: 'initials', label: $t('onboarding.avatar.classic') },
    { key: 'mono', label: $t('onboarding.avatar.mono') },
    { key: 'pixel', label: $t('onboarding.avatar.pixel') },
    { key: 'geometric', label: $t('onboarding.avatar.geometric') },
  ]

  $: swipeActionItems = [
    { value: 'none', label: $t('settingsPanel.swipeAction.none') },
    { value: 'delete', label: $t('settingsPanel.swipeAction.delete') },
    { value: 'read', label: $t('settingsPanel.swipeAction.read') },
    { value: 'unread', label: $t('settingsPanel.swipeAction.unread') },
    { value: 'flag', label: $t('settingsPanel.swipeAction.flag') },
    { value: 'archive', label: $t('settingsPanel.swipeAction.archive') },
    { value: 'snooze', label: $t('settingsPanel.swipeAction.snooze') },
  ]

  function swipeLabel(action: string): string {
    return swipeActionItems.find((item) => item.value === action)?.label ?? ''
  }
</script>

<ListPreview />

<Card title={$t('list.card.layout')}>
  <Row id="avatar-placement" label={$t('list.avatarPlacement.label')} hint={$t('list.avatarPlacement.hint')} stacked>
    <div class="fill">
      <ChoiceCards
        label={$t('list.avatarPlacement.label')}
        value={$prefs.avatarPlacement}
        options={placements}
        on:change={(e) => setAvatarPlacement(e.detail as AvatarPlacement)}
        let:option
      >
        <RowSkeleton avatar={option.key as AvatarPlacement} template={$prefs.rowTemplate as RowTemplate} />
      </ChoiceCards>
    </div>
    {#if $prefs.rowTemplate === 'single' && $prefs.avatarPlacement === 'column'}
      <p class="note">{$t('settingsPanel.hint.avatarHiddenSingleLine')}</p>
    {/if}
  </Row>
  <Row id="row-template" label={$t('settingsPanel.label.rowLayout')} stacked>
    <div class="fill">
      <ChoiceCards
        label={$t('settingsPanel.label.rowLayout')}
        value={$prefs.rowTemplate}
        options={templates}
        minWidth={120}
        on:change={(e) => setRowTemplate(e.detail)}
        let:option
      >
        <RowSkeleton avatar={$prefs.avatarPlacement} template={option.key as RowTemplate} height={84} />
      </ChoiceCards>
    </div>
  </Row>
  <Row id="snippet" label={$t('settingsPanel.toggle.showMessagePreview')} hint={$t('settingsPanel.hint.previewShowsOn')} disabled={!snippetCapable}>
    <span slot="preview">
      <RowSkeleton avatar={$prefs.avatarPlacement} template="relaxed" height={84} />
    </span>
    <ToggleSwitch
      checked={$prefs.rowShowSnippet}
      disabled={!snippetCapable}
      label={$t('settingsPanel.toggle.showMessagePreview')}
      on:change={(e) => setRowShowSnippet(e.detail)}
    />
  </Row>
  <Row id="preview-lines" label={$t('settingsPanel.label.previewLines')} disabled={!snippetCapable || !$prefs.rowShowSnippet}>
    <span slot="preview" class="lines-pic">
      {#each Array($prefs.previewLines) as _, i (i)}
        <span class="bar" class:short={i === $prefs.previewLines - 1}></span>
      {/each}
    </span>
    <Segmented
      label={$t('settingsPanel.label.previewLines')}
      value={String($prefs.previewLines)}
      options={previewLineOptions}
      on:change={(e) => setPreviewLines(Number(e.detail))}
    />
  </Row>
  <Row id="account-email" label={$t('settingsPanel.toggle.showMailboxEmail')}>
    <ToggleSwitch
      checked={$prefs.showAccountEmail}
      label={$t('settingsPanel.toggle.showMailboxEmail')}
      on:change={(e) => setShowAccountEmail(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settingsPanel.label.flaggedHighlight')} description={$t('settingsPanel.hint.barIconFlag')} bare>
  <div data-setting="flag-highlight">
    <ChoiceCards
      label={$t('settingsPanel.label.flaggedHighlight')}
      value={$prefs.flagHighlight}
      options={flagOptions}
      minWidth={120}
      on:change={(e) => setFlagHighlight(e.detail)}
      let:option
    >
      <FlagSkeleton mode={option.key as 'flag' | 'left' | 'both' | 'off'} />
    </ChoiceCards>
  </div>
</Card>

<Card title={$t('list.card.dateTime')}>
  <Row id="date-time" label={$t('settingsPanel.dateTime')} hint={$t('settingsPanel.dateTimeNote')}>
    <ToggleSwitch
      checked={$prefs.showDateTime}
      label={$t('settingsPanel.dateTime')}
      on:change={(e) => setToggle('showDateTime', e.detail)}
    />
  </Row>
  <Row id="time-format" label={$t('settingsPanel.label.timeFormat')} hint={$t('settingsPanel.hint.timeFormat')}>
    <Segmented
      label={$t('settingsPanel.label.timeFormat')}
      value={$prefs.timeFormat}
      options={timeFormatOptions}
      on:change={(e) => setTimeFormat(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('list.card.selection')}>
  <Row id="multi-select" label={$t('settingsPanel.toggle.multiSelect')} hint={$t('settingsPanel.hint.multiSelect')}>
    <ToggleSwitch
      checked={$prefs.multiSelectEnabled}
      label={$t('settingsPanel.toggle.multiSelect')}
      on:change={(e) => setMultiSelectEnabled(e.detail)}
    />
  </Row>
  <Row id="selected-count" label={$t('settingsPanel.toggle.showSelectedCount')} disabled={!multi}>
    <ToggleSwitch
      checked={$prefs.showSelectedCount}
      disabled={!multi}
      label={$t('settingsPanel.toggle.showSelectedCountAria')}
      on:change={(e) => setShowSelectedCount(e.detail)}
    />
  </Row>
  <Row id="select-all-scope" label={$t('settingsPanel.label.selectAllScope')} hint={$t('settingsPanel.hint.selectAllScope')} disabled={!multi}>
    <Select
      value={$prefs.selectAllScope}
      disabled={!multi}
      ariaLabel={$t('settingsPanel.label.selectAllScope')}
      items={selectAllScopeItems}
      on:change={(e) => setSelectAllScope(e.detail as SelectAllScope)}
    />
  </Row>
  <Row id="select-all-unified" label={$t('settingsPanel.toggle.selectAllUnified')} hint={$t('settingsPanel.hint.selectAllUnified')} disabled={!multi}>
    <ToggleSwitch
      checked={$prefs.selectAllUnified}
      disabled={!multi}
      label={$t('settingsPanel.toggle.selectAllUnified')}
      on:change={(e) => setSelectAllUnified(e.detail)}
    />
  </Row>
</Card>

<Card title={$t('settingsPanel.subhead.searchSort')} description={$t('settingsPanel.hint.searchSort')}>
  {#each searchSortRows as row (row.kind)}
    <Row id={`search-sort-${row.kind}`} label={$t(row.label)}>
      <Select
        value={searchSortPref($prefs, row.kind)}
        ariaLabel={$t(row.label)}
        items={searchSortItems(row.kind)}
        on:change={(e) => setSearchSort(row.kind, e.detail as SearchSortPref)}
      />
    </Row>
  {/each}
</Card>

<Card title={$t('settingsPanel.category.sidebar')}>
  <Row id="indent-guides" label={$t('settingsPanel.toggle.indentGuides')} hint={$t('settingsPanel.hint.indentGuides')}>
    <ToggleSwitch
      checked={$prefs.sidebarIndentGuides}
      label={$t('settingsPanel.toggle.indentGuides')}
      on:change={(e) => setSidebarIndentGuides(e.detail)}
    />
  </Row>
  <Row id="unsynced-folder" label={$t('settingsPanel.toggle.unsyncedFolder')} hint={$t('settingsPanel.hint.unsyncedFolder')}>
    <ToggleSwitch
      checked={$prefs.showUnsyncedFolder}
      label={$t('settingsPanel.toggle.unsyncedFolder')}
      on:change={(e) => setShowUnsyncedFolder(e.detail)}
    />
  </Row>
  <Row id="flagged-count" label={$t('settingsPanel.toggle.flaggedCount')} hint={$t('settingsPanel.hint.flaggedCount')}>
    <ToggleSwitch
      checked={$prefs.showFlaggedCount}
      label={$t('settingsPanel.toggle.flaggedCount')}
      on:change={(e) => setShowFlaggedCount(e.detail)}
    />
  </Row>
  <Row id="startup-selection" label={$t('settingsPanel.label.startupSelection')} hint={$t('settingsPanel.hint.startupSelection')}>
    <Select
      value={$prefs.startupSelection}
      ariaLabel={$t('settingsPanel.label.startupSelection')}
      items={startupItems}
      on:change={(e) => setStartupSelection(e.detail)}
    />
  </Row>
  <Row id="views-placement" label={$t('views.setting.label')} hint={$t('views.setting.hint')}>
    <Segmented
      label={$t('views.setting.label')}
      value={$prefs.viewsPlacement}
      options={viewsPlacementOptions}
      on:change={(e) => setViewsPlacement(e.detail as ViewsPlacement)}
    />
  </Row>
</Card>

<Card title={$t('settingsPanel.category.avatars')}>
  <Row id="avatar-source" label={$t('settingsPanel.label.senderPhotos')} hint={$t('settingsPanel.hint.avatarSource')}>
    <Select
      value={$prefs.avatarSource}
      ariaLabel={$t('settingsPanel.label.senderPhotos')}
      items={avatarSourceItems}
      on:change={(e) => setAvatarSource(e.detail)}
    />
  </Row>
  <Row
    id="avatar-style"
    label={$t('settingsPanel.label.generatedStyle')}
    hint={`${$t('settingsPanel.hint.previewSender')} ${sampleEmail}`}
    stacked
  >
    <div class="fill">
      <ChoiceCards
        label={$t('settingsPanel.label.generatedStyle')}
        value={$prefs.avatarStyle}
        options={avatarStyles}
        minWidth={110}
        on:change={(e) => setAvatarStyle(e.detail as PfpStyle)}
        let:option
      >
        <span class="sample">
          <img
            src={pfpDataUri(option.key as PfpStyle, sampleEmail, sampleInitials)}
            alt=""
            aria-hidden="true"
            width="48"
            height="48"
          />
        </span>
      </ChoiceCards>
    </div>
  </Row>
</Card>

<Card title={$t('settingsPanel.heading.gestures')} description={$t('settingsPanel.hint.swipeWork')}>
  <Row id="swipe-enabled" label={$t('settingsPanel.toggle.swipeEnabled')} hint={$t('settingsPanel.hint.swipeEnable')}>
    <span slot="preview">
      <SwipeSkeleton direction="left" action={$prefs.swipeLeftAction} label={swipeLabel($prefs.swipeLeftAction)} />
    </span>
    <ToggleSwitch
      checked={$prefs.swipeEnabled}
      label={$t('settingsPanel.toggle.swipeEnabled')}
      on:change={(e) => setSwipeEnabled(e.detail)}
    />
  </Row>
  <Row id="swipe-left" label={$t('settingsPanel.label.swipeLeft')} disabled={!$prefs.swipeEnabled}>
    <span slot="preview">
      <SwipeSkeleton direction="left" action={$prefs.swipeLeftAction} label={swipeLabel($prefs.swipeLeftAction)} />
    </span>
    <Select
      disabled={!$prefs.swipeEnabled}
      value={$prefs.swipeLeftAction}
      ariaLabel={$t('settingsPanel.label.swipeLeft')}
      items={swipeActionItems}
      on:change={(e) => setSwipeLeftAction(e.detail)}
    />
  </Row>
  <Row id="swipe-right" label={$t('settingsPanel.label.swipeRight')} disabled={!$prefs.swipeEnabled}>
    <span slot="preview">
      <SwipeSkeleton direction="right" action={$prefs.swipeRightAction} label={swipeLabel($prefs.swipeRightAction)} />
    </span>
    <Select
      disabled={!$prefs.swipeEnabled}
      value={$prefs.swipeRightAction}
      ariaLabel={$t('settingsPanel.label.swipeRight')}
      items={swipeActionItems}
      on:change={(e) => setSwipeRightAction(e.detail)}
    />
  </Row>
</Card>

<style>
  .fill {
    flex: 1;
    min-width: 0;
  }

  .note {
    margin: 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .sample {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 72px;
    background: var(--surface-sunken);
  }

  .sample img {
    border-radius: 50%;
  }

  .lines-pic {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }

  .bar {
    display: block;
    height: 6px;
    border-radius: 3px;
    background: var(--border-strong);
  }

  .bar.short {
    width: 60%;
  }
</style>
