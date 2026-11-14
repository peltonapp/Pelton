<script lang="ts">
  // the first-run onboarding: a guided setup in two columns. the steps sit on
  // the left, one choice at a time; on the right a picture of the app follows
  // every pick, since each pick applies to the whole app the moment it is made.
  // it always wears the new design, whatever the user picks for the app.
  // completion is persisted by the parent, so this shows once, with a settings
  // action to run it again. the warranty acknowledgement is stored the moment
  // it is given, so quitting part way through does not lose it.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import Ph from '../common/Ph.svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import Confetti from '../common/Confetti.svelte'
  import LanguageSelect from '../common/LanguageSelect.svelte'
  import LiabilityNotice from '../common/LiabilityNotice.svelte'
  import ChoiceCards from '../settings2/ui/ChoiceCards.svelte'
  import ShellSkeleton from '../settings2/skeletons/ShellSkeleton.svelte'
  import AddMailboxWizard from '../wizard/AddMailboxWizard.svelte'
  import LookStep from './LookStep.svelte'
  import ListStep from './ListStep.svelte'
  import importIcon from '@phosphor-icons/core/assets/duotone/tray-arrow-down-duotone.svg?raw'
  import arrowIcon from '@phosphor-icons/core/assets/duotone/arrow-right-duotone.svg?raw'
  import { prefs, setDesign, setLayoutMode, setNavStyle, setRailSide, setLanguage } from '../../stores/prefs'
  import { defaultMailClientStatus, setDefaultMailClient } from '../../lib/api'
  import { acceptLiability } from '../../lib/liability'
  import { popupIsOpen } from '../../lib/popups'
  import { toastError, errorMessage } from '../../stores/toast'
  import { shortcutLabel, t } from '../../lib/i18n'
  import logo from '../../assets/images/icons/pelton-logo.png'
  import type { Account, DesignPref, LayoutMode, NavStyle, RailSide } from '../../lib/types'

  const dispatch = createEventDispatcher<{ finish: void; added: Account }>()

  type Step = 'welcome' | 'liability' | 'design' | 'layout' | 'nav' | 'look' | 'list' | 'mailbox' | 'done'
  const steps: Step[] = ['welcome', 'liability', 'design', 'layout', 'nav', 'look', 'list', 'mailbox', 'done']

  // the classic design keeps its own layout and navigation, so their steps go.
  $: classic = $prefs.design === 'classic'
  $: order = steps.filter((s) => !(classic && (s === 'layout' || s === 'nav')))
  // the finale is not counted: it is where the count ends.
  $: total = order.length - 1

  let step: Step = 'welcome'
  let direction: 1 | -1 = 1
  $: position = order.indexOf(step)

  let liabilityAccepted = false
  let settingDefault = false
  let offerDefaultMail = false
  let defaultMailSet = false
  let mailboxEmail = ''
  let skippedMailbox = false
  let showImport = false
  let look: { closePopover: () => boolean } | undefined

  // whether Pelton is already the default mail client. the offer is dropped
  // when it is, or where the platform cannot answer.
  onMount(async () => {
    try {
      const s = await defaultMailClientStatus()
      offerDefaultMail = s.known && !s.isDefault
    } catch {
      offerDefaultMail = false
    }
  })

  function go(to: Step, dir: 1 | -1): void {
    direction = dir
    step = to
  }

  function next(): void {
    const i = order.indexOf(step)
    if (i < order.length - 1) {
      go(order[i + 1], 1)
    }
  }

  function back(): void {
    const i = order.indexOf(step)
    if (i > 0) {
      go(order[i - 1], -1)
    }
  }

  async function confirmLiability(): Promise<void> {
    if (!liabilityAccepted) {
      return
    }
    try {
      await acceptLiability()
    } catch (err) {
      toastError(errorMessage(err))
    }
    next()
  }

  function skipMailbox(): void {
    skippedMailbox = mailboxEmail === ''
    go('done', 1)
  }

  function finish(): void {
    dispatch('finish')
  }

  // the one action that moves on from the current step, also behind Enter.
  function primary(): void {
    if (step === 'liability') {
      void confirmLiability()
    } else if (step === 'mailbox') {
      skipMailbox()
    } else if (step === 'done') {
      finish()
    } else {
      next()
    }
  }

  async function makeDefault(): Promise<void> {
    settingDefault = true
    try {
      await setDefaultMailClient()
      defaultMailSet = true
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      settingDefault = false
    }
  }

  function onMailboxAdded(event: CustomEvent<Account>): void {
    mailboxEmail = event.detail.email
    skippedMailbox = false
    dispatch('added', event.detail)
  }

  // Enter moves on and Escape goes back, unless the key belongs to a field, a
  // button or a popup that has a use for it.
  function onKeydown(event: KeyboardEvent): void {
    if (event.defaultPrevented || showImport || event.metaKey || event.ctrlKey || event.altKey) {
      return
    }
    const target = event.target as HTMLElement
    const field = target.closest?.('input, textarea, select, [contenteditable="true"]')
    if (event.key === 'Escape') {
      if (look?.closePopover() || popupIsOpen()) {
        event.preventDefault()
        return
      }
      if (field || position === 0) {
        return
      }
      event.preventDefault()
      back()
      return
    }
    if (event.key !== 'Enter' || field || step === 'mailbox') {
      return
    }
    const control = target.closest?.('button, a, [role="option"]')
    if (control && control.getAttribute('role') !== 'radio') {
      return
    }
    if (step === 'liability' && !liabilityAccepted) {
      return
    }
    event.preventDefault()
    primary()
  }

  function focusOnMount(node: HTMLElement): void {
    void tick().then(() => node.focus({ preventScroll: true }))
  }

  $: designs = [
    { key: 'new', label: $t('design.new'), hint: $t('design.new.hint') },
    { key: 'classic', label: $t('design.classic'), hint: $t('design.classic.hint') },
  ]
  $: layouts = [
    { key: 'columns', label: $t('layout.columns'), hint: $t('layout.columns.hint') },
    { key: 'stacked', label: $t('layout.stacked'), hint: $t('layout.stacked.hint') },
  ]
  $: navs = [
    { key: 'rail', label: $t('nav.rail'), hint: $t('nav.rail.hint') },
    { key: 'footer', label: $t('nav.footer'), hint: $t('nav.footer.hint') },
    { key: 'tabs', label: $t('nav.tabs'), hint: $t('nav.tabs.hint') },
    { key: 'hidden', label: $t('nav.hidden'), hint: $t('nav.hidden.hint') },
  ]
  $: sides = [
    { key: 'left', label: $t('nav.side.left') },
    { key: 'right', label: $t('nav.side.right') },
  ]

  $: titles = {
    welcome: $t('onboarding.welcome'),
    liability: $t('liability.title'),
    design: $t('onboarding.v2.design.title'),
    layout: $t('layout.label'),
    nav: $t('nav.label'),
    look: $t('onboarding.v2.look.title'),
    list: $t('onboarding.v2.list.title'),
    mailbox: $t('onboarding.mailboxTitle'),
    done: $t('onboarding.doneTitle'),
  } as Record<Step, string>
  $: ledes = {
    welcome: $t('onboarding.v2.lede'),
    liability: '',
    design: $t('design.hint'),
    layout: $t('layout.description'),
    nav: $t('nav.description'),
    look: $t('onboarding.v2.look.lede'),
    list: $t('onboarding.v2.list.lede'),
    mailbox: $t('onboarding.mailboxSub'),
    done: '',
  } as Record<Step, string>

  $: themeName =
    $prefs.theme === 'schedule' ? $t('settingsPanel.theme.schedule') : $t(`onboarding.theme.${$prefs.theme}`)
  $: summary = [
    { label: $t('design.label'), value: $t(`design.${$prefs.design}`) },
    ...(classic
      ? []
      : [
          { label: $t('layout.label'), value: $t(`layout.${$prefs.layoutMode}`) },
          {
            label: $t('nav.label'),
            value: $prefs.navStyle === 'rail' ? `${$t('nav.rail')}, ${$t(`nav.side.${$prefs.railSide}`)}` : $t(`nav.${$prefs.navStyle}`),
          },
        ]),
    { label: $t('settingsPanel.label.theme'), value: themeName },
    { label: $t('settingsPanel.label.density'), value: $t(`onboarding.density.${$prefs.density}`) },
    { label: $t('onboarding.v2.summary.mailbox'), value: mailboxEmail || $t('onboarding.v2.summary.none') },
  ]

  const addMailboxHint = shortcutLabel('mod+m')
</script>

<svelte:window on:keydown={onKeydown} />

<div class="screen" role="dialog" aria-modal="true" aria-label={$t('onboarding.welcome')}>
  <div class="steps">
    <header class="top">
      <span class="brand">
        <img src={logo} alt="" draggable="false" />
        <span class="wordmark">Pelton</span>
      </span>
      <!-- no skip before the acknowledgement is given: it is the one thing
           the flow does not let anyone walk past. -->
      {#if position > 1 && step !== 'done'}
        <FlowButton variant="quiet" on:click={finish}>{$t('onboarding.v2.skipAll')}</FlowButton>
      {/if}
    </header>

    <div class="scroll">
      {#key step}
        <div class="step" style={`--from:${direction}`}>
          {#if step !== 'done'}
            <div class="progress" aria-hidden="true">
              {#each order.slice(0, -1) as s, i (s)}
                <i class:on={i <= position}></i>
              {/each}
            </div>
            <span class="counter">{$t('common.stepCounter').replace('{n}', String(position + 1)).replace('{total}', String(total))}</span>
          {/if}
          <h2 tabindex="-1" use:focusOnMount>{titles[step]}</h2>
          {#if ledes[step]}
            <p class="lede">{ledes[step]}</p>
          {/if}

          <div class="choice">
            {#if step === 'welcome'}
              <h3 class="label">{$t('onboarding.language')}</h3>
              <LanguageSelect value={$prefs.language} onSelect={setLanguage} />
            {:else if step === 'liability'}
              <LiabilityNotice bind:accepted={liabilityAccepted} />
            {:else if step === 'design'}
              <ChoiceCards
                label={$t('design.label')}
                value={$prefs.design}
                options={designs}
                minWidth={180}
                on:change={(e) => setDesign(e.detail as DesignPref)}
                let:option
              >
                <ShellSkeleton
                  design={option.key as DesignPref}
                  layout={$prefs.layoutMode}
                  nav={$prefs.navStyle}
                  railSide={$prefs.railSide}
                  panes={$prefs.paneStyle}
                  height={88}
                />
              </ChoiceCards>
            {:else if step === 'layout'}
              <ChoiceCards
                label={$t('layout.label')}
                value={$prefs.layoutMode}
                options={layouts}
                minWidth={180}
                on:change={(e) => setLayoutMode(e.detail as LayoutMode)}
                let:option
              >
                <ShellSkeleton
                  layout={option.key as LayoutMode}
                  nav={$prefs.navStyle}
                  railSide={$prefs.railSide}
                  panes={$prefs.paneStyle}
                  height={88}
                />
              </ChoiceCards>
            {:else if step === 'nav'}
              <ChoiceCards
                label={$t('nav.label')}
                value={$prefs.navStyle}
                options={navs}
                minWidth={150}
                on:change={(e) => setNavStyle(e.detail as NavStyle)}
                let:option
              >
                <ShellSkeleton
                  layout={$prefs.layoutMode}
                  nav={option.key as NavStyle}
                  railSide={$prefs.railSide}
                  panes={$prefs.paneStyle}
                  height={72}
                />
              </ChoiceCards>
              {#if $prefs.navStyle === 'rail'}
                <h3 class="label spaced">{$t('nav.side.label')}</h3>
                <ChoiceCards
                  label={$t('nav.side.label')}
                  value={$prefs.railSide}
                  options={sides}
                  minWidth={150}
                  on:change={(e) => setRailSide(e.detail as RailSide)}
                  let:option
                >
                  <ShellSkeleton
                    layout={$prefs.layoutMode}
                    nav="rail"
                    railSide={option.key as RailSide}
                    panes={$prefs.paneStyle}
                    height={56}
                  />
                </ChoiceCards>
              {/if}
            {:else if step === 'look'}
              <LookStep bind:this={look} />
            {:else if step === 'list'}
              <ListStep />
            {:else if step === 'mailbox'}
              <AddMailboxWizard embedded offerImport={false} on:added={onMailboxAdded} on:done={() => go('done', 1)} />
              <button type="button" class="import" on:click={() => (showImport = true)}>
                <Ph svg={importIcon} size={18} />
                <span class="import-text">
                  <span class="import-title">{$t('wizard.start.importTitle')}</span>
                  <span class="muted">{$t('import.entry.sub')}</span>
                </span>
                <Ph svg={arrowIcon} size={14} />
              </button>
            {:else if step === 'done'}
              <p class="lede">
                {#if skippedMailbox}
                  {$t('onboarding.doneLedeSkippedBefore')} <kbd>{addMailboxHint}</kbd> {$t('onboarding.doneLedeSkippedAfter')}
                {:else}
                  {$t('onboarding.doneLedeDefault')}
                {/if}
              </p>
              <dl class="summary">
                {#each summary as row (row.label)}
                  <div class="summary-row">
                    <dt>{row.label}</dt>
                    <dd>{row.value}</dd>
                  </div>
                {/each}
              </dl>
              {#if offerDefaultMail && !defaultMailSet}
                <div class="default-mail">
                  <span class="dm-text">
                    <span class="dm-title">{$t('onboarding.defaultMailTitle')}</span>
                    <span class="muted">{$t('onboarding.defaultMailSub')}</span>
                  </span>
                  <FlowButton disabled={settingDefault} on:click={makeDefault}>{$t('onboarding.defaultMailSet')}</FlowButton>
                </div>
              {/if}
            {/if}
          </div>
        </div>
      {/key}
    </div>

    <footer class="nav">
      {#if position > 0}
        <FlowButton variant="quiet" on:click={back}>{$t('onboarding.back')}</FlowButton>
      {/if}
      <span class="keys" aria-hidden="true">{$t('onboarding.v2.keys')}</span>
      {#if step === 'mailbox'}
        <FlowButton on:click={skipMailbox}>{mailboxEmail ? $t('onboarding.continue') : $t('onboarding.skipForNow')}</FlowButton>
      {:else}
        <FlowButton variant="primary" disabled={step === 'liability' && !liabilityAccepted} on:click={primary}>
          {step === 'welcome' ? $t('onboarding.getStarted') : step === 'done' ? $t('onboarding.v2.open') : $t('onboarding.continue')}
        </FlowButton>
      {/if}
    </footer>
  </div>

  <aside class="preview" aria-hidden="true">
    <span class="preview-label">{$t('onboarding.v2.preview')}</span>
    <div class="frame">
      <ShellSkeleton
        size="large"
        design={$prefs.design}
        layout={$prefs.layoutMode}
        nav={$prefs.navStyle}
        railSide={$prefs.railSide}
        panes={$prefs.paneStyle}
        avatar={$prefs.avatarPlacement}
      />
    </div>
  </aside>
</div>

{#if step === 'done'}
  <Confetti />
{/if}

{#if showImport}
  {#await import('../import/ImportDialog.svelte') then m}
    <svelte:component this={m.default} on:close={() => (showImport = false)} />
  {/await}
{/if}

<style>
  /* covers the whole window, so it keeps the macOS traffic lights clear itself
     (zero elsewhere); the app's drag strip lies above it there. */
  .screen {
    position: fixed;
    inset: 0;
    z-index: 120;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.1fr);
    background: var(--surface-base);
    color: var(--text-primary);
  }

  .steps {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    padding-top: var(--titlebar-lights);
  }

  .top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    min-height: calc(var(--control-height) + var(--space-5) * 2);
    padding: var(--space-5) var(--space-6) 0;
  }

  .brand {
    display: inline-flex;
    align-items: center;
    gap: var(--space-3);
  }

  .brand img {
    width: calc(var(--control-height) * 0.8);
    height: calc(var(--control-height) * 0.8);
    object-fit: contain;
  }

  .wordmark {
    font-size: var(--fz-list);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.01em;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-6);
  }

  .step {
    --flip: 1;
    max-width: 36rem;
    animation: step-in var(--motion-base) var(--ease-out);
  }

  :global([dir='rtl']) .step {
    --flip: -1;
  }

  /* a step slides in a few px from the side it comes from, and fades. */
  @keyframes step-in {
    from {
      opacity: 0;
      transform: translateX(calc(var(--space-3) * var(--from) * var(--flip)));
    }
  }

  .progress {
    display: flex;
    gap: var(--space-1);
    margin-bottom: var(--space-4);
  }

  .progress i {
    flex: 1;
    max-width: calc(var(--space-6) * 1.5);
    height: var(--space-1);
    border-radius: var(--space-1);
    background: var(--border-default);
    transition: background-color var(--motion-base) var(--ease-out);
  }

  .progress i.on {
    background: var(--accent);
  }

  .counter {
    display: block;
    margin-bottom: var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  h2 {
    margin: 0;
    font-size: var(--fz-title);
    font-weight: var(--fw-semibold);
    letter-spacing: -0.01em;
    line-height: 1.25;
    color: var(--text-primary);
    overflow-wrap: anywhere;
    outline: none;
  }

  .lede {
    margin: var(--space-2) 0 0;
    max-width: 52ch;
    font-size: var(--fz-body);
    line-height: 1.55;
    color: var(--text-secondary);
  }

  .choice {
    margin-top: var(--space-6);
  }

  .choice > .lede {
    margin-top: 0;
  }

  .label {
    margin: 0 0 var(--space-3);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    font-weight: var(--fw-medium);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .label.spaced {
    margin-top: var(--space-6);
  }

  .muted {
    font-size: var(--fz-label);
    line-height: 1.45;
    color: var(--text-tertiary);
  }

  .import {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    width: 100%;
    margin-top: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
    color: var(--text-secondary);
    text-align: start;
    cursor: var(--cursor-action);
    transition:
      border-color var(--motion-fast) var(--ease-out),
      transform var(--motion-fast) var(--ease-press);
  }

  .import:hover {
    border-color: var(--border-strong);
  }

  .import:active {
    transform: scale(0.99);
  }

  .import-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
  }

  .import-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
    color: var(--text-primary);
  }

  .summary {
    margin: var(--space-5) 0 0;
    border-top: var(--hairline) solid var(--border-default);
  }

  .summary-row {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: var(--space-2) var(--space-5);
    padding: var(--space-3) 0;
    border-bottom: var(--hairline) solid var(--border-default);
  }

  .summary dt {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .summary dd {
    margin: 0;
    min-width: 0;
    font-size: var(--fz-list);
    color: var(--text-primary);
    overflow-wrap: anywhere;
  }

  .default-mail {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    margin-top: var(--space-6);
    padding: var(--space-4);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-card);
    background: var(--surface-raised);
  }

  .dm-text {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 14em;
  }

  .dm-title {
    font-size: var(--fz-list);
    font-weight: var(--fw-medium);
  }

  kbd {
    padding: 0 var(--space-2);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
  }

  .nav {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-4) var(--space-6);
    border-top: var(--hairline) solid var(--border-default);
  }

  .keys {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    text-align: end;
    color: var(--text-tertiary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .preview {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--space-3);
    min-width: 0;
    padding: calc(var(--titlebar-lights) + var(--space-6)) calc(var(--space-6) * 2) var(--space-6);
    border-inline-start: var(--hairline) solid var(--border-default);
    background: var(--surface-sunken);
  }

  .preview-label {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
  }

  .frame {
    width: 100%;
    max-width: 44rem;
  }

  /* a narrow window keeps the steps and lets the picture go. */
  @media (max-width: 860px) {
    .screen {
      grid-template-columns: minmax(0, 1fr);
    }

    .preview {
      display: none;
    }
  }
</style>
