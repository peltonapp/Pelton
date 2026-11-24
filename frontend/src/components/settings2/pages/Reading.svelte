<script lang="ts">
  // the Reading page: how a message is shown once it is open (type size, fonts,
  // character encoding, technical badges) and what opening a message does.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import SenderFontPreview from '../skeletons/SenderFontPreview.svelte'
  import BadgesPreview from '../skeletons/BadgesPreview.svelte'
  import OpeningPreview from '../skeletons/OpeningPreview.svelte'
  import {
    prefs,
    setToggle,
    setMessageFontSize,
    setBodyFont,
    setSenderFonts,
    setMarkReadOnOpen,
    setOpenOnArrow,
    setSenderPanel,
    setMailDates,
    setProjectSuggestions,
    setHtmlColors,
  } from '../../../stores/prefs'
  import Segmented from '../ui/Segmented.svelte'
  import { ensureSenderReading, forgetSenderReading, senderReading } from '../../../stores/senderreading'
  import { errorMessage, toastError, toastSuccess } from '../../../stores/toast'
  import type { HtmlColors } from '../../../lib/types'
  import { bodyFonts, bodyFontStack } from '../../../lib/fonts'
  import { listSystemFonts, getSetting, setSetting } from '../../../lib/api'
  import { t } from '../../../lib/i18n'
  import type { SelectItem } from '../../../lib/selectnav'

  $: messageFontItems = [
    { value: '12', label: $t('onboarding.font.small') },
    { value: '14', label: $t('onboarding.font.default') },
    { value: '16', label: $t('onboarding.font.large') },
    { value: '18', label: $t('onboarding.font.larger') },
    { value: '20', label: $t('onboarding.font.largest') },
  ]

  $: bodyFontOptions = bodyFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))

  // every installed system font, loaded once when the page opens. the list is
  // backend-cached, so reopening settings is instant.
  let systemFonts: string[] = []
  onMount(() => {
    listSystemFonts()
      .then((fonts) => (systemFonts = fonts))
      .catch(() => (systemFonts = []))
  })

  // a curated list, then whatever the system has, prefixed so the backend can
  // tell the two apart.
  function fontItems(curated: { key: string; label: string }[]): SelectItem[] {
    const items: SelectItem[] = [{ label: $t('settingsPanel.bodyFont.groupCurated'), options: curated.map((o) => ({ value: o.key, label: o.label })) }]
    if (systemFonts.length > 0) {
      items.push({
        label: $t('settingsPanel.bodyFont.groupSystem'),
        options: systemFonts.map((family) => ({ value: `sys:${family}`, label: family })),
      })
    }
    return items
  }

  $: bodyStack = bodyFontStack($prefs.bodyFont) ?? 'var(--font-ui)'

  // how mail that names no encoding, or names one nothing knows, is read. It is
  // a parser setting rather than a ui preference, so it is read from and written
  // to the store directly. 'auto' detects from the bytes and is what almost
  // everyone should leave it on; the named encodings are for someone who
  // receives mail from one system that is always wrong in the same way.
  const charsetKey = 'charset_fallback'
  const charsetOptions = [
    'windows-1252',
    'iso-8859-2',
    'iso-8859-7',
    'iso-8859-9',
    'koi8-r',
    'windows-1251',
    'shift_jis',
    'euc-jp',
    'gb18030',
    'big5',
    'euc-kr',
    'utf-8',
  ]
  let charsetFallback = 'auto'
  onMount(async () => {
    try {
      const stored = await getSetting(charsetKey)
      if (stored.found && stored.value !== '') {
        charsetFallback = stored.value
      }
    } catch {
      // the store is not open yet; the default is what the parser uses anyway.
    }
  })

  function onCharsetFallback(event: CustomEvent<string>): void {
    charsetFallback = event.detail
    void setSetting(charsetKey, charsetFallback)
  }

  $: htmlColorOptions = [
    { key: 'auto', label: $t('reader.colors.auto') },
    { key: 'adapt', label: $t('reader.colors.always') },
    { key: 'original', label: $t('reader.colors.never') },
  ]

  onMount(() => void ensureSenderReading())

  async function forgetSenders(): Promise<void> {
    try {
      const n = await forgetSenderReading()
      toastSuccess($t('reader.senders.forgotten').replace('{n}', String(n)))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  type ToggleKey = 'showMailboxBadge' | 'showPgp' | 'showAuth'

  $: techInfo = [
    { key: 'showMailboxBadge' as ToggleKey, id: 'mailbox-badge', label: $t('settingsPanel.mailboxBadge'), note: $t('settingsPanel.mailboxBadgeNote') },
    { key: 'showPgp' as ToggleKey, id: 'pgp-status', label: $t('settingsPanel.pgpStatus'), note: $t('settingsPanel.pgpStatusNote') },
    { key: 'showAuth' as ToggleKey, id: 'auth-status', label: $t('settingsPanel.authStatus'), note: $t('settingsPanel.authStatusNote') },
  ]
</script>

<Card title={$t('settingsPanel.category.reading')}>
  <Row id="message-font-size" label={$t('onboarding.extras.fontSize')} hint={$t('settingsPanel.hint.fontSize')}>
    <div slot="preview" class="sample" style={`font-family:${bodyStack};font-size:${$prefs.messageFontSize}px`}>
      {$t('reading.sample')}
    </div>
    <Select
      value={String($prefs.messageFontSize)}
      ariaLabel={$t('onboarding.extras.fontSize')}
      items={messageFontItems}
      on:change={(e) => setMessageFontSize(Number(e.detail))}
    />
  </Row>
  <Row id="body-font" label={$t('settingsPanel.label.bodyFont')} hint={$t('settingsPanel.hint.bodyFont')}>
    <div slot="preview" class="sample" style={`font-family:${bodyStack};font-size:${$prefs.messageFontSize}px`}>
      {$t('reading.sample')}
    </div>
    <Select
      value={$prefs.bodyFont}
      ariaLabel={$t('settingsPanel.label.bodyFont')}
      items={fontItems(bodyFontOptions)}
      on:change={(e) => setBodyFont(e.detail)}
    />
  </Row>
  <Row id="sender-fonts" label={$t('settingsPanel.toggle.senderFonts')} hint={$t('settingsPanel.hint.senderFonts')}>
    <SenderFontPreview slot="preview" senderFonts={$prefs.senderFonts} />
    <ToggleSwitch checked={$prefs.senderFonts} label={$t('settingsPanel.toggle.senderFonts')} on:change={(e) => setSenderFonts(e.detail)} />
  </Row>
  <Row id="html-colors" label={$t('reader.colors.setting')} hint={$t('reader.colors.settingHint')}>
    <Segmented
      label={$t('reader.colors.setting')}
      value={$prefs.htmlColors}
      options={htmlColorOptions}
      on:change={(e) => setHtmlColors(e.detail as HtmlColors)}
    />
  </Row>
  <Row id="sender-reading" label={$t('reader.senders.setting')} hint={$t('reader.senders.settingHint')}>
    <button type="button" class="s2-btn" disabled={$senderReading.size === 0} on:click={() => void forgetSenders()}>
      {$t('reader.senders.forget')}
    </button>
  </Row>
  <Row id="charset-fallback" label={$t('settingsPanel.label.charsetFallback')} hint={$t('settingsPanel.hint.charsetFallback')}>
    <Select
      value={charsetFallback}
      ariaLabel={$t('settingsPanel.label.charsetFallback')}
      items={[{ value: 'auto', label: $t('settingsPanel.charset.auto') }, ...charsetOptions.map((name) => ({ value: name, label: name }))]}
      on:change={onCharsetFallback}
    />
  </Row>
</Card>

<Card title={$t('settingsPanel.techInfo')}>
  {#each techInfo as item (item.key)}
    <Row id={item.id} label={item.label} hint={item.note}>
      <BadgesPreview slot="preview" />
      <ToggleSwitch checked={$prefs[item.key]} label={item.label} on:change={(e) => setToggle(item.key, e.detail)} />
    </Row>
  {/each}
</Card>

<Card title={$t('reading.card.opening')}>
  <Row id="mark-read" label={$t('settingsPanel.toggle.markReadOnOpen')}>
    <OpeningPreview slot="preview" markRead={$prefs.markReadOnOpen} openOnArrow />
    <ToggleSwitch checked={$prefs.markReadOnOpen} label={$t('settingsPanel.toggle.markReadOnOpen')} on:change={(e) => setMarkReadOnOpen(e.detail)} />
  </Row>
  <Row id="open-on-arrow" label={$t('settingsPanel.toggle.openOnArrow')}>
    <OpeningPreview slot="preview" markRead={$prefs.markReadOnOpen} openOnArrow={$prefs.openOnArrow} />
    <ToggleSwitch checked={$prefs.openOnArrow} label={$t('settingsPanel.toggle.openOnArrow')} on:change={(e) => setOpenOnArrow(e.detail)} />
  </Row>
  <Row id="sender-panel" label={$t('senderPanel.setting')} hint={$t('senderPanel.settingHint')}>
    <ToggleSwitch checked={$prefs.senderPanel} label={$t('senderPanel.setting')} on:change={(e) => setSenderPanel(e.detail)} />
  </Row>
  <Row id="project-suggestions" label={$t('projects.settings.suggestions')} hint={$t('projects.settings.suggestionsHint')}>
    <ToggleSwitch checked={$prefs.projectSuggestions} label={$t('projects.settings.suggestions')} on:change={(e) => setProjectSuggestions(e.detail)} />
  </Row>
</Card>

<Card title={$t('reading.card.dates')}>
  <Row id="mail-dates" label={$t('reading.mailDates')} hint={$t('reading.mailDates.hint')}>
    <ToggleSwitch checked={$prefs.mailDates} label={$t('reading.mailDates')} on:change={(e) => setMailDates(e.detail)} />
  </Row>
</Card>

<style>
  .sample {
    color: var(--text-primary);
    line-height: 1.45;
  }
</style>
