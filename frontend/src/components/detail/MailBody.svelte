<script lang="ts">
  // renders the message body. html mail goes into a sandboxed iframe, as
  // defense in depth on top of the backend sanitization: a strict CSP allows
  // exactly one nonce-scoped inline script (Pelton's own click handler, built
  // fresh per render - see buildSrcdoc), so nothing from the email itself can
  // ever execute even though the sandbox now permits scripts to run at all.
  // plaintext renders in the mono font, with bare urls linkified. remote
  // images are blocked by the backend by default; a per-message affordance
  // asks the backend to re-render with remote content allowed.
  import { createEventDispatcher, onDestroy, tick } from 'svelte'
  import { get } from 'svelte/store'
  import { openURL } from '../../lib/browser'
  import { IconPhoto, IconUserCheck, IconWorldCheck, IconMailCheck, IconShieldSearch, IconChevronDown, IconEyeOff, IconCopy, IconExternalLink } from '@tabler/icons-svelte'
  import { resolveLink } from '../../lib/linkclean'
  import { prefs } from '../../stores/prefs'
  import { getMessageHtml, trustSenderImages, allowDomainImages, allowRemoteForMessage, scanUrl } from '../../lib/api'
  import { setBodyHtml } from '../../stores/message'
  import { openContextMenu, type MenuItem } from '../../stores/contextmenu'
  import { virusTotal, linkVerdicts, putLinkVerdict, scanEnabled } from '../../stores/virustotal'
  import VerdictBadge from '../common/VerdictBadge.svelte'
  import { errorMessage, toastError, toastSuccess } from '../../stores/toast'
  import { displayName, linkifySegments } from '../../lib/format'
  import { bodyFontStack } from '../../lib/fonts'
  import { frameKeyRelay, relayedKey, replayKey } from '../../lib/framekeys'
  import { t } from '../../lib/i18n'
  import type { BodyDetail, RemoteSource, Verdict } from '../../lib/types'
  import { markClass, markDocument } from '../../lib/highlight'
  import { highlightTerms, searchOpenedId } from '../../stores/search'
  import { highlightTerms as promiseTerms, promiseHighlight } from '../../stores/promises'
  import Highlight from '../common/Highlight.svelte'
  import Ph from '../common/Ph.svelte'
  import dotsIcon from '@phosphor-icons/core/assets/duotone/dots-three-duotone.svg?raw'
  import calendarIcon from '@phosphor-icons/core/assets/duotone/calendar-blank-duotone.svg?raw'
  import { splitHtmlQuote, splitPlainQuote } from '../../lib/quotes'
  import DatePeek from './DatePeek.svelte'
  import { scanDates } from '../../lib/datescan'
  import { collectText, dateAttr, markRanges, unmarkDates, dateClass } from '../../lib/datemarks'
  import { formatWhen } from '../../lib/calformat'
  import { currentUIScale } from '../../theme/theme'
  import { locale } from '../../lib/i18n'
  import type { DateMention } from '../../lib/eventphrase'
  import type { TimeFormat } from '../../lib/format'
  import { adaptHtml, colorMode, defaultMode, mailStyle, type ColorMode, type MailStyle } from '../../lib/mailcolors'
  import { choiceFor, darkTheme, messageColors, rememberColors, senderReading, setMessageColors } from '../../stores/senderreading'
  import { noteTipContext } from '../../stores/firsttips'
  import paletteIcon from '@phosphor-icons/core/assets/duotone/circle-half-duotone.svg?raw'

  export let detail: BodyDetail
  /** Folds the quoted original at the end of a reply behind a toggle. */
  export let collapseQuotes: boolean = false
  /**
   * Where the body's remote content comes from when it is not a stored
   * message, such as a feed entry: the load buttons then ask this instead of
   * the mail backend, and offer its own "always" choice in place of trusting a
   * sender. Dates in the body are not marked for such a body.
   */
  export let remote: RemoteSource | null = null
  /**
   * Lets html mail follow a dark theme (lib/mailcolors.ts decides how), with
   * a toggle for the message and a choice remembered for the sender. Off,
   * html mail always renders as sent on a light page.
   */
  export let adaptive: boolean = false
  /** The text size in px, or 0 for the reader's global size. */
  export let fontSize: number = 0
  /** Sets the text with the reading view's roomier line height. */
  export let roomy: boolean = false

  // html tells a holder of its own copy of the message, such as the Feeds
  // reader, that the body was rendered again with remote content.
  const dispatch = createEventDispatcher<{ html: { id: number; html: string } }>()

  $: canScan = scanEnabled($virusTotal)
  // the link menu offers copy and open original as well as the scan. The
  // classic design has none of that, so it keeps the system menu unless a
  // scan is on offer.
  $: offerMenu = canScan || $prefs.design !== 'classic'
  $: cleanOn = $prefs.cleanLinks

  // the quoted original, split off when it is folded. showQuote resets with
  // the message.
  let showQuote = false
  let quoteFor = -1
  $: if (detail.id !== quoteFor) {
    quoteFor = detail.id
    showQuote = false
  }
  $: quoteSplit = !collapseQuotes
    ? null
    : detail.isHtml
      ? splitHtmlQuote(detail.bodyHtmlSafe)
      : splitPlainQuote(detail.bodyPlain)
  $: folded = quoteSplit?.hasQuote === true && !showQuote
  $: shownHtml = folded && detail.isHtml && quoteSplit ? quoteSplit.head : detail.bodyHtmlSafe
  $: shownPlain = folded && !detail.isHtml && quoteSplit ? quoteSplit.head : detail.bodyPlain

  // remoteLoaded starts true when the backend already rendered remote content
  // because the sender/domain is trusted (or the global override is on).
  let remoteLoaded = detail.remoteAllowed

  // reset the remote-loaded affordance when a different message opens.
  let lastId = -1
  $: if (detail.id !== lastId) {
    lastId = detail.id
    remoteLoaded = detail.remoteAllowed
    frameHeight = 320
    pixelsBlocked = false
    pixelMenu = null
    pixelDetails = false
  }

  $: senderLabel = displayName(detail.fromName, detail.fromAddress)
  $: senderDomain = detail.fromAddress.includes('@') ? detail.fromAddress.split('@').pop() : ''

  // readVar reads a resolved css token value so the iframe document, which cannot
  // see the parent stylesheet, can match the current theme. $prefs.theme is a
  // dependency so the srcdoc rebuilds when the theme changes.
  function readVar(name: string): string {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  }

  // buildSrcdoc wraps the already-sanitized body in a minimal document. the
  // style tag name is concatenated so the literal token never appears in the
  // component source, where svelte's parser would mistake it for a real style
  // block and parse the css that follows as the component's styles.
  //
  // the background/text colors here are fixed light values, not theme
  // tokens, and deliberately do not follow dark mode. html mail almost always
  // assumes it is being rendered on a white page and sets its own colors (or
  // none at all) on that assumption - senders that hardcode dark/black text
  // with no background of their own used to inherit our dark-theme background,
  // producing unreadable black-on-near-black text. every other mail client
  // (Thunderbird, Apple Mail, Gmail) renders html mail on a fixed light
  // background for the same reason; a message's own styles still override
  // this when it sets them explicitly.
  //
  // a content-security-policy is set as defense in depth on top of the backend
  // sanitizer: when remote content is not allowed, img-src is limited to data:
  // (our inlined cid images) so nothing can phone home even if a remote url ever
  // slipped past the sanitizer. when the user opts in, http(s) image sources are
  // permitted.
  //
  // script-src is scoped to a single nonce, generated fresh per render, and
  // that nonce is used on exactly one inline <script> block below (Pelton's
  // own, never sender content). Any <script> tag the sanitizer might have let
  // through from the email itself has no nonce and so still can't execute:
  // the sandbox no longer omits allow-scripts, but the CSP keeps the "sender
  // html can never run code" guarantee. allow-scripts had to come back
  // because clicks inside a script-less sandboxed iframe never reached a
  // cross-frame listener registered from the parent on some webview engines
  // (contentDocument access itself worked - measuring the body's height was
  // fine - but no click ever arrived at a listener attached that way). A
  // click handler that runs natively inside the iframe's own document, using
  // postMessage to hand the url back to the parent, has no such dependency on
  // cross-frame event delivery.
  function buildSrcdoc(html: string, allowRemote: boolean, fontSize: number, bodyFont: string, nonce: string, offerMenu: boolean, still: boolean, theme: FramePalette | null, leading: string): string {
    // the reader font preference sets the base font-family, so it applies to
    // mail that declares none. Mail that declares its own overrides it, unless
    // the sender-fonts setting is off, in which case the sanitizer has already
    // taken those declarations out and this is what everything renders in.
    const font = bodyFontStack(bodyFont) ?? readVar('--font-ui')
    const accent = readVar('--accent') || '#465af2'
    // the .pelton-vt colors are the light-theme semantic token values written
    // out literally, for the same reason the page colors above are: this
    // document always renders on a fixed white background, so a badge that
    // followed the dark theme would be a dark-mode green on white. Mail
    // adapted to a dark theme renders on the theme's own colours instead, and
    // takes the theme's tokens for everything here.
    const c = theme ?? lightPage
    const css = `
  html,body{margin:0;background:${c.page};color:${c.text};font-family:${font};font-size:${fontSize}px;line-height:${leading};}
  body{padding:4px 2px;word-wrap:break-word;overflow-wrap:break-word;}
  a{color:${c.link};}
  img{max-width:100%;height:auto;}
  video{max-width:100%;height:auto;}
  blockquote{margin:0;margin-inline-start:8px;padding-inline-start:10px;border-inline-start:2px solid ${c.rule};color:${c.quote};}
  table{max-width:100%;}
  pre{white-space:pre-wrap;}
  .pelton-vt{display:inline-block;margin-inline-start:3px;font-family:${readVar('--font-mono') || 'monospace'};font-weight:700;font-size:0.85em;cursor:default;}
  .pelton-vt-clean{color:${c.success};}
  .pelton-vt-flagged{color:${c.danger};}
  .pelton-vt-unknown{color:${c.quote};}
  .pelton-vt-error{color:${c.warning};}
  .pelton-phish{display:inline-block;margin-inline-start:3px;font-weight:700;font-size:0.85em;color:${c.danger};cursor:default;}
  .${markClass}{background:color-mix(in srgb, ${accent} 32%, ${c.page});color:inherit;border-radius:2px;}
  .${dateClass}{position:relative;border-radius:3px;cursor:pointer;outline:none;${still ? '' : 'transition:background-color 120ms ease-out,box-shadow 120ms ease-out;'}}
  .${dateClass}.pelton-date-on{background:color-mix(in srgb, ${accent} 14%, ${c.page});box-shadow:0 0 0 2px color-mix(in srgb, ${accent} 14%, ${c.page});}
  .${dateClass}:focus-visible{box-shadow:0 0 0 2px ${accent};}
  .${dateClass}.pelton-date-on[tabindex]::before{content:"";position:absolute;inset-inline-start:-0.3em;top:-0.95em;width:0.95em;height:0.95em;border-radius:3px;background:${c.page} url("${calendarGlyph(accent)}") center/78% no-repeat;box-shadow:0 0 0 1px color-mix(in srgb, ${accent} 40%, ${c.page}),0 1px 3px rgba(0,0,0,0.16);pointer-events:none;}`
    const imgSrc = allowRemote ? 'data: https: http:' : 'data:'
    // media rides on the same choice as images: a message that embeds a video
    // can only reach out once the reader has allowed remote content for it.
    // The sanitizer has already removed remote media before that point, so this
    // is the second of the two locks rather than the only one.
    const mediaSrc = allowRemote ? 'data: https: http:' : 'data:'
    const csp = `default-src 'none'; img-src ${imgSrc}; media-src ${mediaSrc}; style-src 'unsafe-inline'; font-src data:; script-src 'nonce-${nonce}'`
    const cspMeta = `<meta http-equiv="Content-Security-Policy" content="${csp}">`
    const open = '<sty' + 'le>'
    const close = '</sty' + 'le>'
    // relays clicked links to the parent instead of trying to navigate this
    // sandboxed iframe (which has no allow-top-navigation and would silently
    // do nothing). runs inside the iframe's own document so it's a normal,
    // same-document click listener - no cross-frame event delivery involved.
    //
    // the contextmenu half is only wired up when there is a menu to show:
    // suppressing the default one to show nothing would just look broken.
    const contextHandler = offerMenu
      ? 'document.addEventListener("contextmenu",function(e){var a=e.target&&e.target.closest("a");if(!a)return;var href=(a.getAttribute("href")||"").trim();if(!/^https?:/i.test(href))return;e.preventDefault();window.parent.postMessage({peltonContextLink:href,x:e.clientX,y:e.clientY},"*")});'
      : ''
    const hoverHandler =
      'var lastHov="";function hov(h){if(h===lastHov)return;lastHov=h;window.parent.postMessage({peltonHoverLink:h},"*")}' +
      'document.addEventListener("mouseover",function(e){var a=e.target&&e.target.closest&&e.target.closest("a");hov(a?(a.getAttribute("href")||"").trim():"")});' +
      'document.documentElement.addEventListener("mouseleave",function(){hov("")});'
    // dates marked in the body (see markDates): hovering or focusing one lights
    // up all of its pieces and tells the parent which it is, a click or Enter
    // asks for its preview to open and stay. Only the mark's number crosses.
    const dateHandler =
      `function pdEl(e){var n=e.target;return n&&n.closest?n.closest("[${dateAttr}]"):null}` +
      `function pdLit(id,on){var l=document.querySelectorAll('[${dateAttr}="'+id+'"]');for(var i=0;i<l.length;i++){l[i].classList.toggle("pelton-date-on",on)}}` +
      'var pdCur="";function pdHov(el,how){var id=el?el.getAttribute("' + dateAttr + '"):"";if(id===pdCur)return;if(pdCur)pdLit(pdCur,false);pdCur=id;if(id)pdLit(id,true);window.parent.postMessage({peltonDate:id,how:how},"*")}' +
      'document.addEventListener("mouseover",function(e){pdHov(pdEl(e),"hover")});' +
      'document.documentElement.addEventListener("mouseleave",function(){pdHov(null,"hover")});' +
      'document.addEventListener("focusin",function(e){var d=pdEl(e);if(d)pdHov(d,"focus")});' +
      'document.addEventListener("focusout",function(e){if(pdEl(e))pdHov(null,"focus")});' +
      'document.addEventListener("click",function(e){var d=pdEl(e);if(!d)return;e.preventDefault();window.parent.postMessage({peltonDateOpen:d.getAttribute("' + dateAttr + '")},"*")});' +
      'document.addEventListener("keydown",function(e){var d=pdEl(e);if(!d)return;if(e.key==="Enter"||e.key===" "){e.preventDefault();e.stopImmediatePropagation();window.parent.postMessage({peltonDateOpen:d.getAttribute("' + dateAttr + '")},"*")}else if(e.key==="Escape"&&pdCur){e.stopImmediatePropagation();pdHov(null,"focus")}});'
    const script =
      '<scr' +
      'ipt nonce="' +
      nonce +
      '">document.addEventListener("click",function(e){var a=e.target&&e.target.closest("a");if(!a)return;var href=(a.getAttribute("href")||"").trim();if(!href)return;e.preventDefault();if(/^(https?:|mailto:)/i.test(href)){window.parent.postMessage({peltonOpenUrl:href},"*")}});' +
      contextHandler +
      hoverHandler +
      dateHandler +
      frameKeyRelay +
      '</scr' +
      'ipt>'
    // data-pelton-ready marks the body as belonging to our own srcdoc, not
    // the iframe's initial blank placeholder document: a fresh iframe already
    // has an empty <body> before any srcdoc has loaded, so a readiness check
    // that only looks for "a body" would resolve instantly against that
    // placeholder instead of waiting for the real content.
    // dir="auto" reads the direction off the message's own first strong
    // character (#356), so Hebrew and Arabic mail lays out right to left with
    // its numbers and punctuation in the right order. It is only a default: a
    // message that states its own direction, which the sanitizer now keeps,
    // overrides this on the element that says so.
    //
    // It goes on the document rather than on the reader's side of the pane
    // because the direction belongs to the mail, not to the interface: an
    // Arabic interface must not turn an English message round.
    return `<!doctype html><html dir="auto"><head><meta charset="utf-8">${cspMeta}${open}${css}${close}</head><body dir="auto" data-pelton-ready="1">${html}${script}</body></html>`
  }

  // the calendar glyph a hovered date shows, as an svg data url in a color.
  function calendarGlyph(accent: string): string {
    return `data:image/svg+xml,${encodeURIComponent(calendarIcon.replace('fill="currentColor"', `fill="${accent}"`))}`
  }

  // nonce is regenerated per message so a stale nonce from a previous render
  // can never be replayed against a new one.
  function makeNonce(): string {
    return crypto.randomUUID().replace(/-/g, '')
  }

  // one nonce per message: drawing a fresh one on every update made the
  // document a new string each time, and the frame reloaded when a flag was
  // toggled or the pane re-rendered for any other reason.
  let nonce = makeNonce()
  let nonceFor = -1
  $: if (detail.id !== nonceFor) {
    nonceFor = detail.id
    nonce = makeNonce()
  }

  // --- colours in a dark theme -------------------------------------------------

  /** The colours the frame's own document is drawn in. */
  interface FramePalette {
    page: string
    text: string
    link: string
    rule: string
    quote: string
    success: string
    danger: string
    warning: string
  }

  // the fixed light page html mail is written for.
  const lightPage: FramePalette = {
    page: '#ffffff',
    text: '#1a1a1a',
    link: '#1a56db',
    rule: '#94a3b8',
    quote: '#55606c',
    success: '#1a7f4b',
    danger: '#c0392b',
    warning: '#9a6700',
  }

  // the theme's own colours, read again whenever the theme or accent changes.
  function themePalette(_dark: boolean, _themeId: string, _accent: string): FramePalette {
    return {
      page: readVar('--surface-raised'),
      text: readVar('--text-primary'),
      link: readVar('--link'),
      rule: readVar('--border-strong'),
      quote: readVar('--text-secondary'),
      success: readVar('--success'),
      danger: readVar('--danger'),
      warning: readVar('--warning'),
    }
  }

  let htmlStyle: MailStyle = 'plain'
  let senderColors: '' | ColorMode = ''
  let messageColor: '' | ColorMode = ''
  $: htmlStyle = adaptive && detail.isHtml ? mailStyle(detail.bodyHtmlSafe) : 'plain'
  $: senderColors = adaptive ? choiceFor($senderReading, detail.fromAddress).colors : ''
  $: messageColor = adaptive ? ($messageColors.get(detail.id) ?? '') : ''
  $: colorBase = { dark: $darkTheme, style: htmlStyle, pref: $prefs.htmlColors }
  $: mode = adaptive && detail.isHtml ? colorMode({ ...colorBase, sender: senderColors, message: messageColor }) : 'original'
  $: adapted = mode === 'adapt'
  // a dark theme showing mail as sent puts it on a light sheet of its own.
  $: sheet = adaptive && detail.isHtml && $darkTheme && !adapted
  $: frameHtml = adapted ? adaptHtml(shownHtml) : shownHtml
  $: framePalette = adapted ? themePalette($darkTheme, $prefs.themeId, $prefs.accent) : null
  $: textSize = fontSize || $prefs.messageFontSize
  $: leading = roomy ? readVar('--reading-leading') || '1.7' : '1.5'

  // the toggle flips this message; a flip that differs from what the sender
  // would get otherwise can then be kept for the sender, and a kept choice
  // forgotten again.
  $: senderOrDefault = senderColors || defaultMode(colorBase)
  $: canKeep = messageColor !== '' && messageColor !== senderOrDefault
  $: canForget = !canKeep && messageColor === '' && senderColors !== ''

  // a long html mail in a dark theme is where the colors button matters most.
  const longMail = 6000
  $: if (adaptive && detail.isHtml && $darkTheme && detail.bodyHtmlSafe.length > longMail) {
    void tick().then(() => noteTipContext('dark-mail'))
  }

  function flipColors(): void {
    setMessageColors(detail.id, adapted ? 'original' : 'adapt')
  }

  function keepForSender(): void {
    const keep = messageColor
    if (keep === '') {
      return
    }
    void rememberColors(detail.fromAddress, keep === defaultMode(colorBase) ? '' : keep)
    setMessageColors(detail.id, '')
  }

  function forgetForSender(): void {
    void rememberColors(detail.fromAddress, '')
  }

  $: srcdoc = buildSrcdoc(frameHtml, remoteLoaded, textSize, $prefs.bodyFont, nonce, offerMenu, $prefs.reduceMotion, framePalette, leading)

  // plain-text bodies render in a <pre>, not the sandboxed iframe, so bare
  // urls need their own linkification: nothing upstream turns them into real
  // links the way html mail's own <a> tags already are.
  $: plainSegments = detail.isHtml ? [] : linkifySegments(shownPlain)

  // the iframe is sized to its content height so the reading pane has a single
  // scrollbar instead of a nested one (which the interface zoom made worse).
  // allow-same-origin lets us measure the content height from the parent.
  let frame: HTMLIFrameElement
  let frameHeight = 320
  let resizeObserver: ResizeObserver | null = null

  // measuring documentElement/body.scrollHeight is unreliable here: per spec
  // scrollHeight can never be smaller than the viewport (the iframe's own
  // current height), so once the iframe grows it can never be measured back
  // down, and the height only ratchets upward, leaving a growing gap below
  // short emails. a ResizeObserver on the body's own border box reports its
  // true content size independent of the iframe's current height.
  function measure(): void {
    const body = readyBody()
    if (!body) {
      return
    }
    const h = Math.max(40, Math.ceil(body.getBoundingClientRect().height))
    // hysteresis: on fractional display scaling (125%/150%, common on Fedora's
    // WebKitGTK) the applied iframe height rounds to a device pixel differently
    // from how getBoundingClientRect measures it back, so writing every reading
    // to frameHeight makes the body flip between two adjacent pixel heights
    // forever, which reads as continuous shaking. only apply a change that moves
    // the height by more than a pixel, which settles the loop while still
    // tracking real content growth and shrink.
    if (Math.abs(h - frameHeight) <= 1) {
      return
    }
    frameHeight = h
  }

  // readyBody returns the iframe's body only once it's our own rendered
  // srcdoc, not the iframe's transient initial blank document.
  function readyBody(): HTMLElement | null {
    const body = frame?.contentDocument?.body
    return body?.dataset.peltonReady ? body : null
  }

  // readyPollHandle cancels the fallback readiness poll started by
  // scheduleAttach, so a later reload (a new message) doesn't leave a stale
  // poll running against a document that's already been replaced.
  let readyPollHandle = 0

  // attachInteractivity wires up sizing for the iframe's current document.
  // Link clicks are handled separately, by the nonce-scoped script inside the
  // srcdoc itself (see buildSrcdoc) relaying to onWindowMessage below - not by
  // a listener attached here from the parent, which proved unreliable. This
  // is idempotent per document: calling it twice for the same load is
  // harmless since a fresh resizeObserver just replaces the previous one.
  function attachInteractivity(): void {
    resizeObserver?.disconnect()
    resizeObserver = null
    const body = readyBody()
    if (!body) {
      return
    }
    measure()
    resizeObserver = new ResizeObserver(() => measure())
    resizeObserver.observe(body)
    // a fresh srcdoc replaces the document, so any badges from the previous
    // render are gone and have to be put back.
    decorateLinks(get(linkVerdicts), suspectLinks)
    markMatches(markTerms)
    void markDates()
  }

  // --- dates named in the mail ------------------------------------------------

  $: datesOn = $prefs.mailDates && $prefs.design !== 'classic' && remote === null
  $: timeFormat = $prefs.timeFormat as TimeFormat

  // the dates found in the shown body, by mark number, and the run they were
  // found in, so a reply that arrives after the body changed is dropped.
  let mentions: DateMention[] = []
  let dateRun = 0

  function sentAt(): Date {
    const d = detail.date ? new Date(detail.date) : new Date()
    return Number.isNaN(d.getTime()) ? new Date() : d
  }

  function describe(m: DateMention): string {
    return formatWhen(m.start, m.end, m.allDay, timeFormat, $locale)
  }

  // markDates finds the dates in the frame's text, off the main thread, then
  // wraps them in marks over the same text nodes. Taking the marks out first
  // makes it safe to run again for the same document.
  let datesBody: HTMLElement | null = null

  async function markDates(again = false): Promise<void> {
    const doc = frame?.contentDocument
    const body = readyBody()
    if (!doc || !body || (body === datesBody && !again)) {
      return
    }
    datesBody = body
    const run = ++dateRun
    unmarkDates(doc)
    mentions = []
    closePeek(false)
    if (!datesOn) {
      return
    }
    const before = collectText(body)
    let found: DateMention[]
    try {
      found = await scanDates(before.text, sentAt(), $locale)
    } catch {
      return
    }
    if (run !== dateRun || readyBody() !== body || found.length === 0) {
      return
    }
    const now = collectText(body)
    if (now.text !== before.text) {
      return
    }
    mentions = found
    markRanges(
      now,
      found.map((m, id) => ({ id, index: m.index, length: m.length })),
      (id) => $t('dateHint.mark').replace('{when}', describe(found[id])),
    )
  }

  let datesWere: boolean | null = null
  $: if (frame && detail.isHtml && datesOn !== datesWere) {
    if (datesWere !== null) {
      void markDates(true)
    }
    datesWere = datesOn
  }

  // the plain-text body: the same dates, cut into its segments by offset.
  let plainMentions: DateMention[] = []
  let plainFor = ''
  $: if (!detail.isHtml) {
    void scanPlain(shownPlain, datesOn)
  }

  async function scanPlain(text: string, on: boolean): Promise<void> {
    const key = `${detail.id}|${on}|${text.length}`
    if (key === plainFor) {
      return
    }
    plainFor = key
    plainMentions = []
    mentions = []
    closePeek(false)
    if (!on) {
      return
    }
    const run = ++dateRun
    try {
      const found = await scanDates(text, sentAt(), $locale)
      if (run === dateRun && plainFor === key) {
        plainMentions = found
        mentions = found
      }
    } catch {
      plainMentions = []
    }
  }

  /** A piece of a plain segment, with the mark number of the date it is part of. */
  interface PlainPart {
    text: string
    date: number
    first: boolean
  }

  // splits each segment of the plain body at the dates found in it. Links keep
  // their text whole and are not marked.
  function plainParts(segments: typeof plainSegments, found: DateMention[]): PlainPart[][] {
    let offset = 0
    return segments.map((segment) => {
      const from = offset
      offset += segment.text.length
      if (segment.href || found.length === 0) {
        return [{ text: segment.text, date: -1, first: false }]
      }
      const parts: PlainPart[] = []
      let at = 0
      found.forEach((m, id) => {
        const start = Math.max(m.index - from, at)
        const end = Math.min(m.index + m.length - from, segment.text.length)
        if (end <= start) {
          return
        }
        if (start > at) {
          parts.push({ text: segment.text.slice(at, start), date: -1, first: false })
        }
        parts.push({ text: segment.text.slice(start, end), date: id, first: m.index >= from })
        at = end
      })
      if (at < segment.text.length) {
        parts.push({ text: segment.text.slice(at), date: -1, first: false })
      }
      return parts
    })
  }

  $: plainSplit = detail.isHtml ? [] : plainParts(plainSegments, plainMentions)

  // --- the preview a date opens ---------------------------------------------

  const showDelay = 450
  const hideDelay = 220

  let peek: { id: number; anchor: { left: number; top: number; right: number; bottom: number }; pinned: boolean } | null = null
  let showTimer: ReturnType<typeof setTimeout> | undefined
  let hideTimer: ReturnType<typeof setTimeout> | undefined
  let plainBox: HTMLPreElement | undefined

  $: peekMention = peek ? mentions[peek.id] ?? null : null

  // dateEls returns the marks of one date: in the frame's document, or in the
  // plain body.
  function dateEls(id: number): HTMLElement[] {
    const root: ParentNode | null | undefined = detail.isHtml ? frame?.contentDocument : plainBox
    return root ? Array.from(root.querySelectorAll<HTMLElement>(`[${dateAttr}="${id}"]`)) : []
  }

  // anchorOf is the box around a date's marks in the layout space a fixed
  // element uses: a frame's own pixels are scaled by how it is drawn, and the
  // interface zoom is divided out.
  function anchorOf(id: number): { left: number; top: number; right: number; bottom: number } | null {
    const els = dateEls(id)
    if (els.length === 0) {
      return null
    }
    const rects = els.map((el) => el.getBoundingClientRect())
    let left = Math.min(...rects.map((r) => r.left))
    let right = Math.max(...rects.map((r) => r.right))
    let top = Math.min(...rects.map((r) => r.top))
    let bottom = Math.max(...rects.map((r) => r.bottom))
    if (detail.isHtml && frame) {
      const box = frame.getBoundingClientRect()
      const inner = frame.contentDocument?.documentElement.clientWidth ?? 0
      const ratio = inner > 0 ? box.width / inner : 1
      left = box.left + left * ratio
      right = box.left + right * ratio
      top = box.top + top * ratio
      bottom = box.top + bottom * ratio
    }
    const scale = currentUIScale()
    return { left: left / scale, right: right / scale, top: top / scale, bottom: bottom / scale }
  }

  function openPeek(id: number, pinned: boolean): void {
    clearTimeout(showTimer)
    clearTimeout(hideTimer)
    const anchor = mentions[id] ? anchorOf(id) : null
    peek = anchor ? { id, anchor, pinned: pinned || (peek?.id === id && peek.pinned) } : null
  }

  // the date the focus is being handed back to, which must not open again.
  let returning: number | null = null

  function closePeek(refocus: boolean): void {
    clearTimeout(showTimer)
    clearTimeout(hideTimer)
    const was = peek
    peek = null
    if (refocus && was?.pinned) {
      returning = was.id
      dateEls(was.id)[0]?.focus()
    }
  }

  // hoverDate is the pointer or the focus reaching a date (id) or leaving it
  // (null). A preview opens after a short rest, or at once for the keyboard,
  // and closes a moment after the pointer leaves unless it went into the
  // preview itself. A pinned preview stays.
  function hoverDate(id: number | null, how: 'hover' | 'focus'): void {
    if (how === 'focus' && id !== null && id === returning) {
      returning = null
      return
    }
    returning = null
    if (peek?.pinned) {
      return
    }
    clearTimeout(showTimer)
    if (id === null) {
      if (peek) {
        clearTimeout(hideTimer)
        hideTimer = setTimeout(() => closePeek(false), hideDelay)
      }
      return
    }
    clearTimeout(hideTimer)
    if (peek?.id === id) {
      return
    }
    if (how === 'focus') {
      openPeek(id, false)
    } else {
      showTimer = setTimeout(() => openPeek(id, false), peek ? 0 : showDelay)
    }
  }

  function holdPeek(): void {
    clearTimeout(hideTimer)
  }

  function releasePeek(): void {
    if (peek && !peek.pinned) {
      clearTimeout(hideTimer)
      hideTimer = setTimeout(() => closePeek(false), hideDelay)
    }
  }

  function onPlainDateKey(event: KeyboardEvent, id: number): void {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      openPeek(id, true)
    }
  }

  // the words of the search this message was opened from, marked in the body.
  $: markTerms = [
    ...($searchOpenedId === detail.id ? $highlightTerms : []),
    ...($promiseHighlight?.messageId === detail.id ? promiseTerms($promiseHighlight.quote) : []),
  ]

  // markMatches wraps the search words in mark elements inside the iframe's
  // own document, the same way decorateLinks adds its badges: built with
  // createElement over text nodes, so nothing from the mail is parsed again.
  function markMatches(terms: readonly string[]): void {
    const doc = frame?.contentDocument
    if (readyBody() && doc) {
      markDocument(doc, terms)
    }
  }

  $: if (frame && detail.isHtml) {
    markMatches(markTerms)
  }

  // onFrameLoad fires when the iframe's srcdoc finishes loading. It is the
  // fast path; scheduleAttach below also runs unconditionally as a fallback
  // in case 'load' fires before contentDocument.body is actually populated
  // (observed to be unreliable timing on some webview engines).
  function onFrameLoad(): void {
    hoverUrl = ''
    attachInteractivity()
  }

  // scheduleAttach polls (briefly, via rAF) until the iframe's document has a
  // body, then attaches interactivity. This exists because relying solely on
  // the 'load' event has proven unreliable: a listener registered a beat too
  // late leaves clicks silently doing nothing, which is worse than the small
  // cost of polling for under a second.
  function scheduleAttach(): void {
    cancelAnimationFrame(readyPollHandle)
    let attempts = 0
    const tick = (): void => {
      if (readyBody()) {
        attachInteractivity()
        return
      }
      attempts += 1
      if (attempts < 120) {
        readyPollHandle = requestAnimationFrame(tick)
      }
    }
    readyPollHandle = requestAnimationFrame(tick)
  }

  $: if (frame && srcdoc) {
    scheduleAttach()
  }

  let hoverUrl = ''
  let hoverCleaned = false

  // openLink opens a link in the system browser, cleaned when that is on.
  function openLink(href: string): void {
    openURL(resolveLink(href, cleanOn).url)
  }

  // showHover updates the status overlay with the address a hovered link goes
  // to, or hides it for an empty href: the cleaned address, marked as such,
  // when cleaning changed it. mailto links show just the address. The overlay
  // is anchored to the message pane, outside its scroll area.
  function showHover(href: string): void {
    const link = /^https?:/i.test(href) ? resolveLink(href, cleanOn) : null
    const shown = /^mailto:/i.test(href) ? href.slice(7).split('?')[0] : (link?.url ?? '')
    const cleaned = link?.changed ?? false
    if (shown === hoverUrl && cleaned === hoverCleaned) return
    hoverUrl = shown
    hoverCleaned = cleaned
    hoverSlide = 0
    if (shown) void measureHover()
  }

  let hoverBox: HTMLDivElement | undefined
  let hoverText: HTMLSpanElement | undefined
  let hoverSlide = 0

  // an address wider than the bar slides back and forth instead of being cut.
  async function measureHover(): Promise<void> {
    await tick()
    if (!hoverBox || !hoverText) return
    hoverSlide = Math.max(hoverText.offsetWidth - hoverBox.clientWidth, 0)
  }

  // onWindowMessage receives the url from the click handler running inside
  // the iframe's own document (see the injected script in buildSrcdoc) and
  // opens it in the external browser. The source check makes sure this only
  // ever acts on messages from our own mail iframe, not anything else that
  // might postMessage into this window.
  function onWindowMessage(event: MessageEvent): void {
    if (event.source !== frame?.contentWindow) {
      return
    }
    const key = relayedKey(event.data)
    if (key && frame) {
      replayKey(frame, key)
      return
    }
    const data = event.data as {
      peltonOpenUrl?: unknown
      peltonContextLink?: unknown
      peltonHoverLink?: unknown
      peltonDate?: unknown
      peltonDateOpen?: unknown
      how?: unknown
      x?: unknown
      y?: unknown
    } | null
    if (typeof data?.peltonDate === 'string') {
      const id = data.peltonDate === '' ? null : Number(data.peltonDate)
      if (id === null || (Number.isInteger(id) && id >= 0)) {
        hoverDate(id, data.how === 'focus' ? 'focus' : 'hover')
      }
      return
    }
    if (typeof data?.peltonDateOpen === 'string') {
      const id = Number(data.peltonDateOpen)
      if (Number.isInteger(id) && id >= 0) {
        openPeek(id, true)
      }
      return
    }
    if (typeof data?.peltonHoverLink === 'string') {
      showHover(data.peltonHoverLink)
      return
    }
    const href = data?.peltonOpenUrl
    if (typeof href === 'string' && /^(https?:|mailto:)/i.test(href)) {
      openLink(href)
      return
    }
    const link = data?.peltonContextLink
    if (typeof link === 'string' && typeof data?.x === 'number' && typeof data?.y === 'number') {
      openLinkMenu(link, data.x, data.y)
    }
  }

  // openLinkMenu shows the scan action for a link the user right-clicked inside
  // the iframe. the coordinates arrive in the iframe's own space, which the
  // interface zoom scales relative to the parent, so they are converted using
  // the measured ratio between the element's rendered and internal widths
  // rather than by reasoning about the zoom factor.
  function openLinkMenu(url: string, frameX: number, frameY: number): void {
    if (!offerMenu || !frame) {
      return
    }
    const rect = frame.getBoundingClientRect()
    const inner = frame.contentDocument?.documentElement.clientWidth ?? 0
    const scale = inner > 0 ? rect.width / inner : 1
    openContextMenu(rect.left + frameX * scale, rect.top + frameY * scale, linkMenuItems(url))
  }

  // linkMenuItems is the menu of a link: copy it as it would open, open the
  // original when cleaning changed it, and scan it when that is set up.
  function linkMenuItems(url: string): MenuItem[] {
    const link = resolveLink(url, cleanOn)
    const items: MenuItem[] = []
    if ($prefs.design !== 'classic') {
      items.push({ label: $t('links.copy'), icon: IconCopy, action: () => void copyLink(link.url) })
      if (link.changed) {
        items.push({ label: $t('links.openOriginal'), icon: IconExternalLink, action: () => openURL(url) })
      }
    }
    if (canScan) {
      items.push({ label: $t('virustotal.scanLink'), icon: IconShieldSearch, action: () => void scanLink(url) })
    }
    return items
  }

  async function copyLink(url: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(url)
      toastSuccess($t('links.copied'))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // the plain-text body is rendered by svelte rather than in the iframe, so its
  // links get the same menu directly.
  function onPlainContext(event: MouseEvent, url: string): void {
    if (!offerMenu || url === '') {
      return
    }
    event.preventDefault()
    openContextMenu(event.clientX, event.clientY, linkMenuItems(url))
  }

  async function scanLink(url: string): Promise<void> {
    try {
      putLinkVerdict(detail.id, url, await scanUrl(url))
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // glyph and class for a verdict badge rendered inside the iframe, where the
  // svelte badge component cannot reach.
  function verdictGlyph(v: Verdict): { text: string; cls: string; label: string } {
    if (v.error !== '') {
      const label = v.error === 'rate_limited' ? $t('virustotal.error.rateLimited') : v.error === 'unauthorized' ? $t('virustotal.error.unauthorized') : v.error
      return { text: '!', cls: 'pelton-vt-error', label }
    }
    if (v.status === 'clean') {
      return { text: '✓', cls: 'pelton-vt-clean', label: `0/${v.total} ${$t('virustotal.verdict.enginesFlagged')}` }
    }
    if (v.status === 'flagged') {
      return {
        text: '✗',
        cls: 'pelton-vt-flagged',
        label: `${v.malicious + v.suspicious}/${v.total} ${$t('virustotal.verdict.enginesFlagged')}`,
      }
    }
    return { text: '?', cls: 'pelton-vt-unknown', label: $t('virustotal.verdict.unknown') }
  }

  // suspectLinks are the urls the phishing checks flagged in this message: a
  // link whose text names another site, a punycode host, a shortener, or a
  // sign-in page somewhere unrelated to the sender (#206). Marking them where
  // they sit is the only place the warning is any use, since the banner cannot
  // point at a link halfway down a newsletter.
  $: suspectLinks = new Set(detail.phishing?.links ?? [])

  // decorateLinks pins a badge to every anchor whose target has a verdict.
  // Badges are added to the iframe's own document, which the parent can reach
  // because the sandbox allows same-origin; they are our own nodes, never
  // anything the sender supplied, and are rebuilt from scratch on every pass so
  // a re-scan cannot leave a stale marker behind.
  function decorateLinks(verdicts: Map<string, Verdict>, suspects: Set<string>): void {
    const doc = frame?.contentDocument
    if (!readyBody() || !doc) {
      return
    }
    for (const stale of Array.from(doc.querySelectorAll('.pelton-vt, .pelton-phish'))) {
      stale.remove()
    }
    for (const anchor of Array.from(doc.querySelectorAll('a[href]'))) {
      const href = (anchor.getAttribute('href') ?? '').trim()
      if (suspects.has(href)) {
        const mark = doc.createElement('span')
        mark.className = 'pelton-phish'
        mark.textContent = '⚠'
        mark.title = $t('phishing.linkMarker')
        anchor.after(mark)
      }
      const verdict = verdicts.get(href)
      if (!verdict) {
        continue
      }
      const { text, cls, label } = verdictGlyph(verdict)
      const badge = doc.createElement('span')
      badge.className = `pelton-vt ${cls}`
      badge.textContent = text
      badge.title = label
      anchor.after(badge)
    }
  }

  $: if (frame && detail.isHtml) {
    decorateLinks($linkVerdicts, suspectLinks)
  }

  window.addEventListener('message', onWindowMessage)

  onDestroy(() => {
    clearTimeout(showTimer)
    clearTimeout(hideTimer)
    dateRun += 1
    resizeObserver?.disconnect()
    cancelAnimationFrame(readyPollHandle)
    window.removeEventListener('message', onWindowMessage)
  })

  // tracking pixels (#205). they are only reported when the setting is on, so
  // an empty list means either a clean message or detection turned off.
  $: trackers = detail.trackingPixels ?? []
  // pixelsBlocked flips once remote content has been loaded with the pixels
  // held back, so the reader is told what was withheld instead of it happening
  // quietly. Detection is a heuristic: the wording says "look like" and the
  // menu on each button loads them anyway.
  let pixelsBlocked = false
  // which load button has its "load them too" menu open, by index.
  let pixelMenu: number | null = null
  // whether the per-pixel breakdown under the banner is expanded.
  let pixelDetails = false

  // trackerHosts is the deduplicated list of who the pixels would report to.
  $: trackerHosts = [...new Set(trackers.map((p) => p.host))]

  // reasonText turns the backend's signal names into the sentence shown under
  // a pixel. An unknown signal falls back to its raw name rather than vanishing.
  function reasonText(reason: string, tFn: (key: string) => string): string {
    const key = `detail.mailBody.pixelReason.${reason}`
    const text = tFn(key)
    return text === key ? reason : text
  }

  async function loadRemote(includeTrackers = false): Promise<void> {
    try {
      const id = detail.id
      if (remote) {
        await remote.load(includeTrackers)
      } else {
        const html = await getMessageHtml(id, true, includeTrackers)
        setBodyHtml(id, html)
        dispatch('html', { id, html })
      }
      // the reader may have moved on while the html was fetched
      if (detail.id !== id) {
        return
      }
      remoteLoaded = true
      pixelsBlocked = !includeTrackers && trackers.length > 0
      pixelMenu = null
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // trust the sender permanently, then show this message's remote content now.
  async function trustSender(includeTrackers = false): Promise<void> {
    try {
      await trustSenderImages(detail.id)
      toastSuccess($t('detail.mailBody.imagesTrusted').replace('{who}', senderLabel))
      await loadRemote(includeTrackers)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // trust the whole sender domain permanently, then show remote content now.
  async function trustDomain(includeTrackers = false): Promise<void> {
    try {
      await allowDomainImages(detail.id)
      toastSuccess($t('detail.mailBody.imagesTrusted').replace('{who}', senderDomain ?? ''))
      await loadRemote(includeTrackers)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // allow remote content for this one message only (persists), then show it now.
  // nothing else from the sender or domain is trusted.
  async function trustThisEmail(includeTrackers = false): Promise<void> {
    try {
      await allowRemoteForMessage(detail.id)
      await loadRemote(includeTrackers)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // trust whatever the remote source offers to trust, then show it now.
  async function trustSource(includeTrackers = false): Promise<void> {
    try {
      await remote?.always?.run()
      await loadRemote(includeTrackers)
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  // the load buttons, built as data so the split control that carries the
  // "load the pixels too" menu is written once rather than four times.
  $: loadActions = remote
    ? [
        {
          key: 'once',
          icon: IconPhoto,
          label: $t('detail.mailBody.loadOnce'),
          title: '',
          run: (withPixels: boolean) => loadRemote(withPixels),
        },
        ...(remote.always
          ? [
              {
                key: 'always',
                icon: IconWorldCheck,
                label: remote.always.label,
                title: remote.always.title,
                run: (withPixels: boolean) => trustSource(withPixels),
              },
            ]
          : []),
      ]
    : mailLoadActions

  $: mailLoadActions = [
    {
      key: 'once',
      icon: IconPhoto,
      label: $t('detail.mailBody.loadOnce'),
      title: '',
      run: (withPixels: boolean) => loadRemote(withPixels),
    },
    {
      key: 'email',
      icon: IconMailCheck,
      label: $t('detail.mailBody.thisEmail'),
      title: $t('detail.mailBody.thisEmailTitle'),
      run: (withPixels: boolean) => trustThisEmail(withPixels),
    },
    {
      key: 'sender',
      icon: IconUserCheck,
      label: $t('detail.mailBody.thisSender'),
      title: $t('detail.mailBody.alwaysLoadFrom').replace('{who}', senderLabel),
      run: (withPixels: boolean) => trustSender(withPixels),
    },
    ...(senderDomain
      ? [
          {
            key: 'domain',
            icon: IconWorldCheck,
            label: $t('detail.mailBody.thisDomain'),
            title: $t('detail.mailBody.alwaysLoadFrom').replace('{who}', senderDomain),
            run: (withPixels: boolean) => trustDomain(withPixels),
          },
        ]
      : []),
  ]
</script>

{#if detail.hasRemoteContent && !remoteLoaded}
  <div class="remote-bar">
    <div class="remote-info">
      <span class="remote-text">{$t('detail.mailBody.remoteBlocked')}</span>
      {#if detail.remoteHosts && detail.remoteHosts.length > 0}
        <span class="remote-hosts" title={detail.remoteHosts.join(', ')}>
          {$t('detail.mailBody.from')} {detail.remoteHosts.slice(0, 3).join(', ')}{detail.remoteHosts.length > 3 ? ` ${$t('detail.mailBody.more').replace('{count}', String(detail.remoteHosts.length - 3))}` : ''}
        </span>
      {/if}
    </div>
    <div class="remote-actions">
      {#each loadActions as action, i (action.key)}
        <span class="split">
          <button type="button" class="remote-btn" class:has-caret={trackers.length > 0} title={action.title} on:click={() => action.run(false)}>
            <svelte:component this={action.icon} size={14} stroke={1.6} />
            {action.label}
          </button>
          {#if trackers.length > 0}
            <button
              type="button"
              class="remote-caret"
              aria-label={$t('detail.mailBody.pixelMenuLabel')}
              aria-expanded={pixelMenu === i}
              on:click={() => (pixelMenu = pixelMenu === i ? null : i)}
            >
              <IconChevronDown size={12} stroke={2} />
            </button>
            {#if pixelMenu === i}
              <div class="pixel-menu">
                <button type="button" on:click={() => action.run(true)}>
                  {$t('detail.mailBody.loadWithPixels')}
                </button>
              </div>
            {/if}
          {/if}
        </span>
      {/each}
    </div>
  </div>
{/if}

{#if detail.hasRemoteContent && !remoteLoaded && trackers.length > 0}
  <div class="pixel-bar">
    <button type="button" class="pixel-summary" aria-expanded={pixelDetails} on:click={() => (pixelDetails = !pixelDetails)}>
      <IconEyeOff size={14} stroke={1.6} />
      {$t('detail.mailBody.pixelSummary').replace('{count}', String(trackers.length))}
      <IconChevronDown size={12} stroke={2} class={pixelDetails ? 'flip' : ''} />
    </button>
    {#if pixelDetails}
      <ul class="pixel-list">
        {#each trackers as pixel (pixel.url)}
          <li>
            <span class="pixel-host">{pixel.host}</span>
            <span class="pixel-why">{pixel.reasons.map((r) => reasonText(r, $t)).join(', ')}</span>
          </li>
        {/each}
      </ul>
      <p class="pixel-note">{$t('detail.mailBody.pixelNote')}</p>
    {/if}
  </div>
{/if}

{#if remoteLoaded && pixelsBlocked}
  <div class="pixel-bar done">
    <span class="pixel-done-text">
      <IconEyeOff size={14} stroke={1.6} />
      {$t('detail.mailBody.pixelsKeptBlocked').replace('{count}', String(trackers.length))}
      <span class="pixel-hosts" title={trackerHosts.join(', ')}>{trackerHosts.slice(0, 2).join(', ')}</span>
    </span>
    <button type="button" class="remote-btn" on:click={() => loadRemote(true)}>
      {$t('detail.mailBody.loadWithPixels')}
    </button>
  </div>
{/if}

{#if adaptive && detail.isHtml && $darkTheme}
  <div class="colors-bar">
    <button type="button" class="colors-btn" data-tip-anchor="mail-colors" on:click={flipColors}>
      <Ph svg={paletteIcon} size={13} />
      {adapted ? $t('reader.colors.original') : $t('reader.colors.adapt')}
    </button>
    {#if canKeep}
      <button type="button" class="colors-link" on:click={keepForSender}>
        {$t('reader.colors.keep').replace('{who}', senderLabel)}
      </button>
    {:else if canForget}
      <button type="button" class="colors-link" on:click={forgetForSender}>
        {$t('reader.colors.forget').replace('{who}', senderLabel)}
      </button>
    {/if}
  </div>
{/if}

{#if detail.isHtml}
  <iframe
    class="body-frame"
    class:adapted
    class:sheet
    title={$t('detail.mailBody.iframeTitle')}
    sandbox="allow-same-origin allow-scripts allow-popups allow-top-navigation-by-user-activation"
    bind:this={frame}
    on:load={onFrameLoad}
    style={`height:${frameHeight}px`}
    {srcdoc}
  ></iframe>
{:else}
  <!-- dir="auto" for the same reason as the html document above: a plain-text
       message written right to left reads that way, whatever the interface is
       set to. -->
  <!-- the date marks are spans around the text they hold, so what the body
       says is the same with or without them. -->
  <!-- svelte-ignore a11y-no-static-element-interactions a11y-mouse-events-have-key-events -->
  <pre dir="auto" class="body-plain mono selectable" class:roomy style={`font-size:${textSize}px;--date-glyph:url("${calendarGlyph('currentColor')}")`} bind:this={plainBox}>{#each plainSegments as segment, si}{#if segment.href}<a
        class="plain-link"
        href={segment.href}
        on:click|preventDefault={() => openLink(segment.href ?? '')}
        on:contextmenu={(e) => onPlainContext(e, segment.href ?? '')}
        on:mouseenter={() => showHover(segment.href ?? '')}
        on:mouseleave={() => showHover('')}
      >{segment.text}</a>{#if $linkVerdicts.has(segment.href ?? '')}<VerdictBadge verdict={$linkVerdicts.get(segment.href ?? '')!} size={12} />{/if}{:else}{#each plainSplit[si] ?? [{ text: segment.text, date: -1, first: false }] as part}{#if part.date >= 0}{#if part.first}<span
              class="date-mark"
              class:on={peek?.id === part.date}
              data-pelton-date={part.date}
              role="button"
              tabindex="0"
              aria-haspopup="dialog"
              aria-label={mentions[part.date] ? $t('dateHint.mark').replace('{when}', describe(mentions[part.date])) : undefined}
              on:mouseenter={() => hoverDate(part.date, 'hover')}
              on:mouseleave={() => hoverDate(null, 'hover')}
              on:focus={() => hoverDate(part.date, 'focus')}
              on:blur={() => hoverDate(null, 'focus')}
              on:click={() => openPeek(part.date, true)}
              on:keydown={(e) => onPlainDateKey(e, part.date)}
            ><Highlight text={part.text} terms={markTerms} /></span>{:else}<span
              class="date-mark"
              class:on={peek?.id === part.date}
              data-pelton-date={part.date}
              on:mouseenter={() => hoverDate(part.date, 'hover')}
              on:mouseleave={() => hoverDate(null, 'hover')}
            ><Highlight text={part.text} terms={markTerms} /></span>{/if}{:else}<Highlight text={part.text} terms={markTerms} />{/if}{/each}{/if}{/each}</pre>
{/if}

{#if quoteSplit?.hasQuote}
  <button
    type="button"
    class="quote-toggle"
    aria-expanded={showQuote}
    aria-label={showQuote ? $t('reader.quote.hide') : $t('reader.quote.show')}
    title={showQuote ? $t('reader.quote.hide') : $t('reader.quote.show')}
    on:click={() => (showQuote = !showQuote)}
  >
    <Ph svg={dotsIcon} size={16} />
  </button>
{/if}

{#if peek && peekMention}
  <DatePeek
    slot={{ start: peekMention.start, end: peekMention.end, allDay: peekMention.allDay }}
    anchor={peek.anchor}
    pinned={peek.pinned}
    messageId={detail.id}
    on:hold={holdPeek}
    on:release={releasePeek}
    on:close={(e) => closePeek(e.detail)}
  />
{/if}

{#if hoverUrl}
  <div class="link-hover mono" aria-hidden="true" bind:this={hoverBox}>
    <span
      class="link-hover-text"
      class:sliding={hoverSlide > 0}
      style={`--slide:-${hoverSlide}px;--slide-time:${Math.max(3, hoverSlide / 40)}s`}
      bind:this={hoverText}>{hoverUrl}</span
    >
    {#if hoverCleaned}
      <span class="link-hover-tag">{$t('links.cleaned')}</span>
    {/if}
  </div>
{/if}

<style>
  .quote-toggle {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    margin-top: var(--space-2);
    padding: 0 var(--space-2);
    height: var(--chip-height);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-sunken);
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      border-color var(--motion-fast) var(--ease-out);
  }

  .quote-toggle:hover {
    background: var(--surface-hover);
    border-color: var(--border-strong);
    color: var(--text-primary);
  }

  .quote-toggle[aria-expanded='true'] {
    background: var(--surface-hover);
  }

  .link-hover {
    position: absolute;
    right: 0;
    bottom: 0;
    z-index: 30;
    max-width: calc(100% - var(--space-6));
    border: var(--hairline) solid var(--border-default);
    border-right: none;
    border-bottom: none;
    border-top-left-radius: var(--radius-control);
    background: var(--surface-overlay);
    color: var(--text-secondary);
    font-size: calc(var(--fz-meta) - 1px);
    overflow: hidden;
    white-space: nowrap;
    pointer-events: none;
  }

  .link-hover-text {
    display: inline-block;
    padding: var(--space-1) var(--space-2);
  }

  .link-hover-tag {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    padding: 0 var(--space-2) 0 var(--space-3);
    background: var(--surface-overlay);
    color: var(--text-tertiary);
    font-family: var(--font-ui);
  }

  .link-hover-text.sliding {
    animation: link-slide var(--slide-time) ease-in-out 0.6s infinite alternate;
  }

  @keyframes link-slide {
    0%,
    15% {
      transform: translateX(0);
    }
    85%,
    100% {
      transform: translateX(var(--slide));
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .link-hover-text.sliding {
      animation: none;
    }
  }

  /* a warning-tinted banner, since blocked remote content is a privacy matter. */
  .remote-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    flex-wrap: wrap;
    margin-bottom: var(--space-3);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--warning);
    border-radius: var(--radius-control);
    background: var(--warning-bg, var(--surface-sunken));
  }

  .remote-info {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }

  .remote-text {
    font-size: var(--fz-label);
    color: var(--text-primary);
  }

  .remote-hosts {
    font-size: var(--fz-meta);
    color: var(--text-secondary);
    font-family: var(--font-mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .remote-actions {
    display: inline-flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }

  .remote-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-1) var(--space-3);
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-primary);
    font-size: var(--fz-label);
    cursor: var(--cursor-action);
  }

  .remote-btn:hover {
    background: var(--surface-hover);
  }

  /* a split control: the button does the safe thing, the caret next to it
     offers to load the tracking pixels as well. */
  .split {
    position: relative;
    display: inline-flex;
  }

  .remote-btn.has-caret {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
    border-inline-end: none;
  }

  .remote-caret {
    display: inline-flex;
    align-items: center;
    padding: 0 var(--space-1);
    border: var(--hairline) solid var(--border-default);
    border-top-right-radius: var(--radius-control);
    border-bottom-right-radius: var(--radius-control);
    background: var(--surface-raised);
    color: var(--text-secondary);
    cursor: pointer;
  }

  .remote-caret:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .pixel-menu {
    position: absolute;
    top: calc(100% + var(--space-1));
    /* anchored to the end of its button, so it opens on the other side in a
       right-to-left interface rather than off the edge of it. */
    inset-inline-end: 0;
    z-index: 30;
    border: var(--hairline) solid var(--border-default);
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow: var(--shadow-overlay);
    white-space: nowrap;
  }

  .pixel-menu button {
    display: block;
    width: 100%;
    padding: var(--space-2) var(--space-3);
    border: none;
    background: transparent;
    color: var(--text-primary);
    font-size: var(--fz-label);
    text-align: start;
    cursor: pointer;
  }

  .pixel-menu button:hover {
    background: var(--surface-hover);
  }

  /* the tracking-pixel line sits under the blocked-content banner, and after
     loading it replaces it with what stayed behind. */
  .pixel-bar {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-2) var(--space-3);
    border: var(--hairline) solid var(--border-subtle);
    border-radius: var(--radius-control);
    margin-bottom: var(--space-2);
  }

  .pixel-bar.done {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    flex-wrap: wrap;
  }

  .pixel-summary,
  .pixel-done-text {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    border: none;
    background: transparent;
    padding: 0;
    color: var(--text-secondary);
    font-size: var(--fz-meta);
    text-align: start;
  }

  .pixel-summary {
    cursor: pointer;
  }

  .pixel-summary:hover {
    color: var(--text-primary);
  }

  .pixel-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }

  .pixel-list li {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
    font-size: var(--fz-meta);
  }

  .pixel-host {
    font-family: var(--font-mono);
    color: var(--text-primary);
    word-break: break-all;
  }

  .pixel-why,
  .pixel-hosts {
    color: var(--text-tertiary);
  }

  .pixel-note {
    margin: 0;
    font-size: var(--fz-meta);
    color: var(--text-tertiary);
  }

  /* height is set inline from the measured content height so the pane has a single
     scrollbar. a min-height avoids a flash of collapse before the first measure.
     the iframe is a nested browsing context, but its rendering still gets pulled
     along by the ancestor's CSS zoom (the app-wide interface scale, applied on
     <html> in theme.ts) since zoom is not a normal non-inherited property for
     replaced elements. that means email content ends up zoomed twice: once by
     its own messageFontSize, and again by whatever interface scale the user
     picked. dividing back out by --ui-scale here cancels the ancestor's zoom so
     the reading pane always renders at true size regardless of interface zoom. */
  .body-frame {
    display: block;
    width: 100%;
    min-height: 120px;
    border: none;
    /* fixed, not theme-derived: matches the fixed light background the
       srcdoc itself renders on (see buildSrcdoc), so there is no dark flash
       around/before the html mail content in dark mode. */
    background: #ffffff;
    zoom: calc(1 / var(--ui-scale, 1));
  }

  /* adapted mail is drawn on the theme's own surface. */
  .body-frame.adapted {
    background: var(--surface-raised);
  }

  /* designed mail in a dark theme keeps its colours on a light sheet. */
  .body-frame.sheet {
    border-radius: var(--radius-control);
  }

  .body-plain.roomy {
    line-height: var(--reading-leading);
  }

  .colors-bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-end;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }

  .colors-btn,
  .colors-link {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    height: var(--chip-height);
    padding: 0 var(--space-2);
    border-radius: var(--radius-control);
    font: inherit;
    font-size: var(--fz-meta);
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      color var(--motion-fast) var(--ease-out);
  }

  .colors-btn {
    border: var(--hairline) solid var(--border-default);
    background: transparent;
    color: var(--text-secondary);
  }

  .colors-btn:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .colors-link {
    border: none;
    background: transparent;
    color: var(--text-tertiary);
    text-decoration: underline;
    text-underline-offset: 2px;
  }

  .colors-link:hover {
    color: var(--text-primary);
  }

  .body-plain {
    margin: 0;
    font-size: var(--fz-body);
    line-height: 1.55;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-word;
  }

  .plain-link {
    color: var(--accent);
    text-decoration: underline;
    cursor: var(--cursor-action);
  }

  /* a date the mail names: quiet until the pointer or the focus reaches it. */
  .date-mark {
    position: relative;
    border-radius: var(--radius-control);
    outline: none;
    cursor: var(--cursor-action);
    transition:
      background-color var(--motion-fast) var(--ease-out),
      box-shadow var(--motion-fast) var(--ease-out);
  }

  .date-mark:hover,
  .date-mark.on,
  .date-mark:focus-visible {
    background: color-mix(in srgb, var(--accent) 14%, transparent);
    box-shadow: 0 0 0 var(--space-1) color-mix(in srgb, var(--accent) 14%, transparent);
  }

  .date-mark:focus-visible {
    box-shadow: 0 0 0 calc(var(--hairline) * 2) var(--accent);
  }

  .date-mark[tabindex]:is(:hover, .on, :focus-visible)::before {
    content: '';
    position: absolute;
    inset-inline-start: -0.3em;
    top: -0.95em;
    width: 0.95em;
    height: 0.95em;
    border-radius: var(--radius-control);
    background: var(--surface-overlay);
    box-shadow:
      0 0 0 var(--hairline) var(--border-default),
      var(--shadow-overlay);
    pointer-events: none;
  }

  .date-mark[tabindex]:is(:hover, .on, :focus-visible)::after {
    content: '';
    position: absolute;
    inset-inline-start: -0.2em;
    top: -0.85em;
    width: 0.75em;
    height: 0.75em;
    background: var(--accent);
    -webkit-mask: var(--date-glyph) center / contain no-repeat;
    mask: var(--date-glyph) center / contain no-repeat;
    pointer-events: none;
  }

  :global(:root[data-reduce-motion]) .date-mark {
    transition: none;
  }
</style>
