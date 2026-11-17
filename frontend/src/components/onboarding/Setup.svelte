<script lang="ts">
  // the setup flow of the new design. it asks first what Pelton is for and
  // then shows only the steps that follow from it, each skippable, with a
  // picture of the app beside them that draws the chosen spaces and hints at
  // the one a step is about. where it stands is stored after every move, so
  // closing the window resumes on the same step. every step that was opened
  // stays mounted while hidden, so going back finds it as it was left.
  import { createEventDispatcher, onMount, tick } from 'svelte'
  import FlowButton from '../common/FlowButton.svelte'
  import LanguageSelect from '../common/LanguageSelect.svelte'
  import ShellSkeleton from '../settings2/skeletons/ShellSkeleton.svelte'
  import LookStep from './LookStep.svelte'
  import LayoutCards from './LayoutCards.svelte'
  import NavCards from './NavCards.svelte'
  import PurposeStep from './PurposeStep.svelte'
  import MailboxStep from './MailboxStep.svelte'
  import DavStep from './DavStep.svelte'
  import NotesStep from './NotesStep.svelte'
  import ChatStep from './ChatStep.svelte'
  import KeysStep from './KeysStep.svelte'
  import SummaryStep from './SummaryStep.svelte'
  import { prefs, setDesign, setHiddenSpaces, setLanguage } from '../../stores/prefs'
  import { sidebar } from '../../stores/accounts'
  import { chatSignedIn } from '../../stores/chat'
  import { getSetting, listAddressBooks, listCalendarAccounts, setSetting, SettingKeys } from '../../lib/api'
  import { spaces } from '../../lib/spaces'
  import {
    alreadySet,
    hiddenFor,
    isPiece,
    landingSpace,
    parseProgress,
    planSteps,
    primaryAction,
    resumeStep,
    serializeProgress,
    shownSpaces,
    spacesFor,
    startProgress,
    summarize,
    type SetupPiece,
    type SetupProgress,
    type SetupState,
    type SetupStep,
  } from '../../lib/setupplan'
  import { popupIsOpen } from '../../lib/popups'
  import { t } from '../../lib/i18n'
  import logo from '../../assets/images/icons/pelton-logo.png'
  import type { Account, Space } from '../../lib/types'

  /** Opened on purpose from Settings or the palette rather than on first launch. */
  export let rerun = false
  /** A dialog above the flow has the keyboard, so Enter is left alone. */
  export let blocked = false

  const dispatch = createEventDispatcher<{ finish: { landing: Space; rerun: boolean }; added: Account; classic: void }>()

  let progress: SetupProgress | null = null
  let davCount = 0
  let visited = new Set<SetupStep>()
  let direction: 1 | -1 = 1
  let hoverScene: Space | null = null
  let look: { closePopover: () => boolean } | undefined
  let scroller: HTMLElement
  const headings: Partial<Record<SetupStep, HTMLElement>> = {}

  $: accounts = ($sidebar.data?.accounts ?? []) as Account[]
  $: state = {
    mailboxes: accounts.length,
    davAccounts: davCount,
    vaultChosen: $prefs.notesVault !== '',
    chatSignedIn: $chatSignedIn,
  } satisfies SetupState
  $: chosen = progress ? spacesFor(progress.purpose, progress.custom) : []
  $: steps = planSteps(chosen)
  $: step = progress?.step ?? 'purpose'
  $: index = steps.indexOf(step)
  let counted: SetupStep[] = []
  $: counted = steps.filter((s) => s !== 'summary')
  $: action = progress ? primaryAction(step, progress, state) : 'continue'
  $: lines = progress ? summarize(steps, progress, state) : []

  const iconFor = new Map(spaces.map((s) => [s.key, s.icon]))
  $: previewSpaces = chosen.map((key) => ({ key, icon: iconFor.get(key), pending: key === 'chat' && !$prefs.chatEnabled }))
  $: scene = hoverScene ?? sceneFor(step, chosen)

  function sceneFor(s: SetupStep, list: Space[]): Space {
    if (s === 'dav') {
      return list.includes('calendar') ? 'calendar' : list.includes('contacts') ? 'contacts' : 'tasks'
    }
    if (s === 'notes' || s === 'chat') {
      return s
    }
    return 'mail'
  }

  onMount(async () => {
    let stored: SetupProgress | null = null
    try {
      const r = await getSetting(SettingKeys.onboardingProgress)
      stored = r.found ? parseProgress(r.value) : null
    } catch {
      stored = null
    }
    const start = stored ?? startProgress(rerun, shownSpaces($prefs.hiddenSpaces, $prefs.chatEnabled))
    const plan = planSteps(spacesFor(start.purpose, start.custom))
    progress = { ...start, rerun: start.rerun || rerun, step: resumeStep(start, plan) }
    visited = new Set([progress.step])
    persist()
    void focusStep()
    const [cals, books] = await Promise.allSettled([listCalendarAccounts(), listAddressBooks()])
    davCount = (cals.status === 'fulfilled' ? cals.value.length : 0) + (books.status === 'fulfilled' ? books.value.length : 0)
  })

  function persist(): void {
    if (progress) {
      void setSetting(SettingKeys.onboardingProgress, serializeProgress(progress)).catch(() => {})
    }
  }

  async function focusStep(): Promise<void> {
    await tick()
    scroller?.scrollTo?.({ top: 0 })
    headings[step]?.focus({ preventScroll: true })
  }

  function goTo(target: SetupStep, outcome?: 'done' | 'skipped'): void {
    if (!progress) {
      return
    }
    direction = steps.indexOf(target) >= index ? 1 : -1
    const outcomes = outcome ? { ...progress.outcomes, [step]: outcome } : progress.outcomes
    progress = { ...progress, step: target, outcomes }
    visited = new Set(visited).add(target)
    hoverScene = null
    persist()
    void focusStep()
  }

  function markDone(piece: SetupPiece): void {
    if (progress && progress.outcomes[piece] !== 'done') {
      progress = { ...progress, outcomes: { ...progress.outcomes, [piece]: 'done' } }
      persist()
    }
  }

  function chatToggled(on: boolean): void {
    if (on) {
      markDone('chat')
    } else if (progress) {
      const outcomes = { ...progress.outcomes }
      delete outcomes.chat
      progress = { ...progress, outcomes }
      persist()
    }
  }

  function next(): void {
    if (!progress || index < 0) {
      return
    }
    if (step === 'summary') {
      finish()
      return
    }
    if (step === 'purpose') {
      const hidden = hiddenFor(chosen)
      if (hidden !== $prefs.hiddenSpaces) {
        setHiddenSpaces(hidden)
      }
    }
    const outcome = isPiece(step) && action === 'skip' ? 'skipped' : 'done'
    goTo(steps[index + 1], outcome)
  }

  function back(): void {
    if (index > 0) {
      goTo(steps[index - 1])
    }
  }

  // skipping before the purpose was confirmed leaves the spaces as they were,
  // so it lands in mail.
  function finish(): void {
    void setSetting(SettingKeys.onboardingProgress, '').catch(() => {})
    const landing = progress?.outcomes.purpose === 'done' ? landingSpace(chosen, state) : 'mail'
    dispatch('finish', { landing, rerun: progress?.rerun ?? rerun })
  }

  function useClassic(): void {
    void setSetting(SettingKeys.onboardingProgress, '').catch(() => {})
    setDesign('classic')
    dispatch('classic')
  }

  function onMailboxAdded(account: Account): void {
    markDone('mailbox')
    dispatch('added', account)
  }

  // Enter moves on unless a field, a button or a popup has a use for it.
  // Escape closes what is open inside a step and nothing else, so it never
  // throws away what was entered.
  function onKeydown(event: KeyboardEvent): void {
    if (blocked || event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey || !progress) {
      return
    }
    if (event.key === 'Escape') {
      if (look?.closePopover()) {
        event.preventDefault()
      }
      return
    }
    if (event.key !== 'Enter' || step === 'mailbox' || popupIsOpen()) {
      return
    }
    const target = event.target as HTMLElement
    if (target.closest?.('input, textarea, select, [contenteditable="true"], form')) {
      return
    }
    const control = target.closest?.('button, a, [role="option"]')
    if (control && control.getAttribute('role') !== 'radio') {
      return
    }
    event.preventDefault()
    next()
  }

  $: titles = {
    purpose: $t('setup.purpose.title'),
    language: $t('onboarding.language'),
    look: $t('onboarding.v2.look.title'),
    mailbox: $t('onboarding.mailboxTitle'),
    dav: $t('setup.dav.title'),
    notes: $t('setup.notes.title'),
    chat: $t('setup.chat.title'),
    keys: $t('setup.keys.title'),
    summary: rerun ? $t('setup.summary.titleAgain') : $t('setup.summary.title'),
  } as Record<SetupStep, string>
  $: ledes = {
    purpose: $t('setup.purpose.lede'),
    language: $t('setup.language.lede'),
    look: $t('onboarding.v2.look.lede'),
    mailbox: $t('onboarding.mailboxSub'),
    dav: $t('setup.dav.lede'),
    notes: $t('setup.notes.lede'),
    chat: $t('setup.chat.lede'),
    keys: $t('setup.keys.lede'),
    summary: $t('setup.summary.lede'),
  } as Record<SetupStep, string>
  $: details = {
    mailbox: accounts.map((a) => a.email).join(', '),
    notes: $prefs.notesVault || $t('setup.notes.defaultShort'),
    chat: $chatSignedIn ? '' : $t('setup.chat.onLater'),
  } as Partial<Record<SetupPiece, string>>
  $: position = counted.indexOf(step)
  $: announce = progress
    ? step === 'summary'
      ? titles.summary
      : `${$t('setup.stepOf').replace('{n}', String(position + 1)).replace('{total}', String(counted.length))}: ${titles[step]}`
    : ''
  $: primaryLabel = action === 'finish' ? $t('onboarding.v2.open') : action === 'skip' ? $t('onboarding.skipForNow') : $t('onboarding.continue')
</script>

<svelte:window on:keydown={onKeydown} />

<div class="screen" role="dialog" aria-modal="true" aria-label={$t('setup.label')} inert={blocked || undefined}>
  <div class="steps">
    <header class="top">
      <span class="brand">
        <img src={logo} alt="" draggable="false" />
        <span class="wordmark">Pelton</span>
      </span>
      {#if progress && step !== 'summary'}
        <FlowButton variant="quiet" on:click={finish}>{$t('onboarding.v2.skipAll')}</FlowButton>
      {/if}
    </header>

    {#if progress}
      <div class="meter">
        <ol class="progress" aria-label={$t('setup.progress')}>
          {#each counted as s, i (s)}
            <li class:on={i <= position || step === 'summary'} aria-current={s === step ? 'step' : undefined}>
              <span class="sr">{titles[s]}</span>
            </li>
          {/each}
        </ol>
        {#if step !== 'summary'}
          <span class="counter" aria-hidden="true">
            {$t('common.stepCounter').replace('{n}', String(position + 1)).replace('{total}', String(counted.length))}
          </span>
        {/if}
      </div>
      <p class="sr" aria-live="polite">{announce}</p>

      <div class="scroll" bind:this={scroller}>
        {#each steps as s (s)}
          {#if visited.has(s)}
            <section class="step" hidden={s !== step} style={`--from:${direction}`} aria-labelledby={`setup-${s}`}>
              <h2 id={`setup-${s}`} tabindex="-1" bind:this={headings[s]}>{titles[s]}</h2>
              {#if ledes[s]}
                <p class="lede">{ledes[s]}</p>
              {/if}
              <div class="choice">
                {#if s === 'purpose'}
                  <PurposeStep bind:purpose={progress.purpose} bind:custom={progress.custom} on:scene={(e) => (hoverScene = e.detail)} />
                  {#if !progress.rerun}
                    <p class="aside">
                      {$t('setup.classic.text')}
                      <button type="button" class="link" on:click={useClassic}>{$t('setup.classic.button')}</button>
                    </p>
                  {/if}
                {:else if s === 'language'}
                  <LanguageSelect value={$prefs.language} onSelect={setLanguage} />
                {:else if s === 'look'}
                  <LookStep bind:this={look} />
                  <h3 class="label spaced">{$t('layout.label')}</h3>
                  <LayoutCards />
                  <h3 class="label spaced">{$t('nav.label')}</h3>
                  <NavCards />
                {:else if s === 'mailbox'}
                  <MailboxStep {accounts} on:added={(e) => onMailboxAdded(e.detail)} on:done={() => goTo(steps[index + 1], 'done')} />
                {:else if s === 'dav'}
                  <DavStep mailbox={accounts[0] ?? null} existing={davCount} on:added={() => markDone('dav')} />
                {:else if s === 'notes'}
                  <NotesStep on:done={() => markDone('notes')} />
                {:else if s === 'chat'}
                  <ChatStep on:enabled={(e) => chatToggled(e.detail)} on:signedin={() => markDone('chat')} />
                {:else if s === 'keys'}
                  <KeysStep />
                {:else if s === 'summary'}
                  <SummaryStep {lines} {details} on:goto={(e) => goTo(e.detail)} />
                {/if}
              </div>
            </section>
          {/if}
        {/each}
      </div>

      <footer class="nav">
        {#if index > 0}
          <FlowButton variant="quiet" on:click={back}>{$t('onboarding.back')}</FlowButton>
        {/if}
        <span class="keys" aria-hidden="true">{step === 'mailbox' ? '' : $t('setup.keysHint')}</span>
        <FlowButton variant={action === 'skip' ? 'secondary' : 'primary'} on:click={next}>{primaryLabel}</FlowButton>
      </footer>
    {/if}
  </div>

  <aside class="preview" aria-hidden="true">
    <span class="preview-label">{$t('onboarding.v2.preview')}</span>
    <div class="frame">
      <ShellSkeleton
        size="large"
        layout={$prefs.layoutMode}
        nav={$prefs.navStyle}
        railSide={$prefs.railSide}
        panes={$prefs.paneStyle}
        avatar={$prefs.avatarPlacement}
        spaces={previewSpaces}
        {scene}
      />
    </div>
    {#if progress}
      <ul class="legend">
        {#each previewSpaces as s (s.key)}
          <li class:pending={s.pending}>{$t(`space.${s.key}`)}</li>
        {/each}
      </ul>
    {/if}
  </aside>
</div>

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

  .meter {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-5) var(--space-6) 0;
  }

  .progress {
    display: flex;
    flex: 1;
    gap: var(--space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }

  .progress li {
    flex: 1;
    max-width: calc(var(--space-6) * 1.5);
    height: var(--space-1);
    border-radius: var(--space-1);
    background: var(--border-default);
    transition: background-color var(--motion-base) var(--ease-out);
  }

  .progress li.on {
    background: var(--accent);
  }

  .counter {
    font-family: var(--font-mono);
    font-size: var(--fz-meta);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-tertiary);
    white-space: nowrap;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
    border: 0;
  }

  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5) var(--space-6) var(--space-6);
  }

  .step {
    --flip: 1;
    max-width: 38rem;
    animation: step-in var(--motion-base) var(--ease-out);
  }

  .step[hidden] {
    display: none;
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

  .aside {
    margin: var(--space-6) 0 0;
    font-size: var(--fz-label);
    color: var(--text-tertiary);
  }

  .link {
    padding: 0;
    border: 0;
    background: none;
    color: var(--link);
    font: inherit;
    text-decoration: underline;
    text-underline-offset: 2px;
    cursor: var(--cursor-action);
  }

  .link:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
    border-radius: var(--radius-control);
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

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1) var(--space-3);
    max-width: 44rem;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--fz-label);
    color: var(--text-secondary);
  }

  .legend li.pending {
    color: var(--text-tertiary);
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
