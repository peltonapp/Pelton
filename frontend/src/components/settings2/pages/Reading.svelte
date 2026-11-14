<script lang="ts">
  // the Reading page: how a message is shown once it is open (type size, fonts,
  // character encoding, technical badges), what opening a message does, and the
  // status bar.
  import { onMount } from 'svelte'
  import Card from '../ui/Card.svelte'
  import Row from '../ui/Row.svelte'
  import ToggleSwitch from '../../common/ToggleSwitch.svelte'
  import Select from '../../common/Select.svelte'
  import {
    prefs,
    setToggle,
    setRestoreTabs,
    setMessageFontSize,
    setBodyFont,
    setSenderFonts,
    setUIFont,
    setMonoFont,
    setMarkReadOnOpen,
    setOpenOnArrow,
    setStatusClock,
    setStatusUnread,
  } from '../../../stores/prefs'
  import { bodyFonts, uiFonts, monoFonts, bodyFontStack, uiFontStack, monoFontStack } from '../../../lib/fonts'
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
  $: uiFontOptions = uiFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))
  $: monoFontOptions = monoFonts.map((f) => ({ key: f.key, label: f.label ?? $t(f.labelKey ?? '') }))

  // every installed system font, loaded once when the page opens. the list is
  // backend-cached, so reopening settings is instant.
  let systemFonts: string[] = []
  onMount(() => {
    listSystemFonts()
      .then((fonts) => (systemFonts = fonts))
      .catch(() => (systemFonts = []))
  })

  // the three font pickers share a shape: a curated list, then whatever the
  // system has, prefixed so the backend can tell the two apart.
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
  $: uiStack = uiFontStack($prefs.uiFont) ?? 'var(--font-ui)'
  $: monoStack = monoFontStack($prefs.monoFont) ?? 'var(--font-mono)'

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
    <ToggleSwitch checked={$prefs.senderFonts} label={$t('settingsPanel.toggle.senderFonts')} on:change={(e) => setSenderFonts(e.detail)} />
  </Row>
  <Row id="ui-font" label={$t('settingsPanel.label.uiFont')} hint={$t('settingsPanel.hint.uiFont')}>
    <div slot="preview" class="sample" style={`font-family:${uiStack}`}>{$t('reading.sample')}</div>
    <Select
      value={$prefs.uiFont}
      ariaLabel={$t('settingsPanel.label.uiFont')}
      items={fontItems(uiFontOptions)}
      on:change={(e) => setUIFont(e.detail)}
    />
  </Row>
  <Row id="mono-font" label={$t('settingsPanel.label.monoFont')} hint={$t('settingsPanel.hint.monoFont')}>
    <div slot="preview" class="sample" style={`font-family:${monoStack}`}>{$t('reading.sample')}</div>
    <Select
      value={$prefs.monoFont}
      ariaLabel={$t('settingsPanel.label.monoFont')}
      items={fontItems(monoFontOptions)}
      on:change={(e) => setMonoFont(e.detail)}
    />
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
      <ToggleSwitch checked={$prefs[item.key]} label={item.label} on:change={(e) => setToggle(item.key, e.detail)} />
    </Row>
  {/each}
</Card>

<Card title={$t('reading.card.opening')}>
  <Row id="mark-read" label={$t('settingsPanel.toggle.markReadOnOpen')}>
    <ToggleSwitch checked={$prefs.markReadOnOpen} label={$t('settingsPanel.toggle.markReadOnOpen')} on:change={(e) => setMarkReadOnOpen(e.detail)} />
  </Row>
  <Row id="open-on-arrow" label={$t('settingsPanel.toggle.openOnArrow')}>
    <ToggleSwitch checked={$prefs.openOnArrow} label={$t('settingsPanel.toggle.openOnArrow')} on:change={(e) => setOpenOnArrow(e.detail)} />
  </Row>
  <Row id="restore-tabs" label={$t('settingsPanel.toggle.restoreTabs')} hint={$t('settingsPanel.hint.restoreTabs')}>
    <ToggleSwitch checked={$prefs.restoreTabs} label={$t('settingsPanel.toggle.restoreTabs')} on:change={(e) => setRestoreTabs(e.detail)} />
  </Row>
</Card>

<Card title={$t('settingsPanel.heading.statusBar')}>
  <Row id="status-clock" label={$t('settingsPanel.toggle.statusClock')}>
    <ToggleSwitch checked={$prefs.statusClock} label={$t('settingsPanel.toggle.statusClock')} on:change={(e) => setStatusClock(e.detail)} />
  </Row>
  <Row id="status-unread" label={$t('settingsPanel.toggle.statusUnread')} hint={$t('settingsPanel.hint.statusUnread')}>
    <ToggleSwitch checked={$prefs.statusUnread} label={$t('settingsPanel.toggle.statusUnread')} on:change={(e) => setStatusUnread(e.detail)} />
  </Row>
</Card>

<style>
  .sample {
    color: var(--text-primary);
    line-height: 1.45;
  }
</style>
