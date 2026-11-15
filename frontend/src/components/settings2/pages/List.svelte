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
  import SelectionPreview from '../skeletons/SelectionPreview.svelte'
  import ThreadPreview from '../skeletons/ThreadPreview.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import {
    prefs,
    setAvatarPlacement,
    setRowTemplate,
    setRowShowSnippet,
    setPreviewLines,
    setFlagHighlight,
    setToggle,
    setMultiSelectEnabled,
    setShowSelectedCount,
    setSelectAllScope,
    setSelectAllUnified,
    setSearchSort,
    searchSortPref,
    setAvatarSource,
    setAvatarStyle,
    setSwipeEnabled,
    setSwipeLeftAction,
    setSwipeRightAction,
    setThreadedList,
    setAfterMove,
  } from '../../../stores/prefs'
  import AfterMoveSkeleton from '../skeletons/AfterMoveSkeleton.svelte'
  import { t } from '../../../lib/i18n'
  import { pfpDataUri, type PfpStyle } from '../../../lib/pfp'
  import { initials } from '../../../lib/format'
  import { searchSorts, automaticSort } from '../../../lib/searchsort'
  import type { SelectItem } from '../../../lib/selectnav'
  import type {
    AvatarPlacement,
    RowTemplate,
    SelectAllScope,
    SearchKind,
    SearchSortPref,
    AfterMove,
  } from '../../../lib/types'

  $: afterMoveOptions = [
    { key: 'older', label: $t('list.afterMove.older') },
    { key: 'newer', label: $t('list.afterMove.newer') },
    { key: 'list', label: $t('list.afterMove.list') },
  ]

  $: snippetCapable = $prefs.rowTemplate === 'relaxed' || $prefs.rowTemplate === 'comfortable'

  // the option under the pointer in each card group, which the preview shows
  // in place of the saved choice.
  let trialAvatar: string | null = null
  let trialTemplate: string | null = null
  let trialFlag: string | null = null
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

<Card title={$t('list.card.layout')}>
  <Row id="avatar-placement" label={$t('list.avatarPlacement.label')} hint={$t('list.avatarPlacement.hint')} stacked>
    <ListPreview slot="preview" trial={{ avatar: trialAvatar ?? undefined }} />
    <div class="fill">
      <ChoiceCards
        on:hover={(e) => (trialAvatar = e.detail)}
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
    <ListPreview slot="preview" trial={{ template: trialTemplate ?? undefined }} />
    <div class="fill">
      <ChoiceCards
        on:hover={(e) => (trialTemplate = e.detail)}
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
    <ListPreview slot="preview" />
    <ToggleSwitch
      checked={$prefs.rowShowSnippet}
      disabled={!snippetCapable}
      label={$t('settingsPanel.toggle.showMessagePreview')}
      on:change={(e) => setRowShowSnippet(e.detail)}
    />
  </Row>
  <Row id="preview-lines" label={$t('settingsPanel.label.previewLines')} disabled={!snippetCapable || !$prefs.rowShowSnippet}>
    <ListPreview slot="preview" />
    <Segmented
      label={$t('settingsPanel.label.previewLines')}
      value={String($prefs.previewLines)}
      options={previewLineOptions}
      on:change={(e) => setPreviewLines(Number(e.detail))}
    />
  </Row>
  <Row id="flag-highlight" label={$t('settingsPanel.label.flaggedHighlight')} hint={$t('settingsPanel.hint.barIconFlag')} stacked>
    <ListPreview slot="preview" trial={{ flag: trialFlag ?? undefined }} />
    <ChoiceCards
      on:hover={(e) => (trialFlag = e.detail)}
      label={$t('settingsPanel.label.flaggedHighlight')}
      value={$prefs.flagHighlight}
      options={flagOptions}
      minWidth={120}
      on:change={(e) => setFlagHighlight(e.detail)}
      let:option
    >
      <FlagSkeleton mode={option.key as 'flag' | 'left' | 'both' | 'off'} />
    </ChoiceCards>
  </Row>
</Card>

<Card title={$t('list.card.conversations')}>
  <Row id="threaded-list" label={$t('list.threaded.label')} hint={$t('list.threaded.hint')}>
    <ThreadPreview slot="preview" threaded={$prefs.threadedList} />
    <ToggleSwitch
      checked={$prefs.threadedList}
      label={$t('list.threaded.label')}
      on:change={(e) => setThreadedList(e.detail)}
    />
  </Row>
  <Row id="after-move" label={$t('list.afterMove.label')} hint={$t('list.afterMove.hint')}>
    <AfterMoveSkeleton slot="preview" mode={$prefs.afterMove} />
    <Segmented
      label={$t('list.afterMove.label')}
      value={$prefs.afterMove}
      options={afterMoveOptions}
      on:change={(e) => setAfterMove(e.detail as AfterMove)}
    />
  </Row>
</Card>

<Card title={$t('list.card.dateTime')}>
  <Row id="date-time" label={$t('settingsPanel.dateTime')} hint={$t('settingsPanel.dateTimeNote')}>
    <ListPreview slot="preview" />
    <ToggleSwitch
      checked={$prefs.showDateTime}
      label={$t('settingsPanel.dateTime')}
      on:change={(e) => setToggle('showDateTime', e.detail)}
    />
  </Row>
</Card>

<Card title={$t('list.card.selection')}>
  <Row id="multi-select" label={$t('settingsPanel.toggle.multiSelect')} hint={$t('settingsPanel.hint.multiSelect')}>
    <SelectionPreview slot="preview" enabled={$prefs.multiSelectEnabled} showCount={$prefs.showSelectedCount} />
    <ToggleSwitch
      checked={$prefs.multiSelectEnabled}
      label={$t('settingsPanel.toggle.multiSelect')}
      on:change={(e) => setMultiSelectEnabled(e.detail)}
    />
  </Row>
  <Row id="selected-count" label={$t('settingsPanel.toggle.showSelectedCount')} disabled={!multi}>
    <SelectionPreview slot="preview" showCount={$prefs.showSelectedCount} />
    <ToggleSwitch
      checked={$prefs.showSelectedCount}
      disabled={!multi}
      label={$t('settingsPanel.toggle.showSelectedCountAria')}
      on:change={(e) => setShowSelectedCount(e.detail)}
    />
  </Row>
  <Row id="select-all-scope" label={$t('settingsPanel.label.selectAllScope')} hint={$t('settingsPanel.hint.selectAllScope')} disabled={!multi}>
    <SelectionPreview slot="preview" showCount scope={$prefs.selectAllScope as 'offer' | 'all' | 'loaded'} />
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

</style>
