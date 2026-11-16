// chathtml.ts prepares a chat message's body for the page. The backend has
// already run formatted bodies through the app's sanitiser, cut down to the
// tags Matrix allows; this only shapes what is left for display: links are
// marked so a click goes to the system browser, links to people become
// mention pills, and images, which can only be mxc media on the user's own
// homeserver, lose their source until it is fetched through the backend.
// Plain bodies are escaped and their bare links made clickable. No link is
// ever fetched for a preview.

import { linkifySegments } from './format'

/** A chat body ready for {@html}, and the mxc images it waits on. */
export interface PreparedBody {
  html: string
  images: string[]
}

const userLink = /^https:\/\/matrix\.to\/#\/(@[^/?#]+)/

function escapeHTML(text: string): string {
  return text.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c] ?? c)
}

/** Escapes plain text and turns its bare links into links. */
export function plainToHTML(text: string): string {
  return linkifySegments(text)
    .map((s) => (s.href ? `<a href="${escapeHTML(s.href)}" data-chat-link="">${escapeHTML(s.text)}</a>` : escapeHTML(s.text)))
    .join('')
}

/** Shapes sanitised message html for display. */
export function prepareHTML(html: string): PreparedBody {
  const tpl = document.createElement('template')
  tpl.innerHTML = html
  const images: string[] = []

  for (const a of Array.from(tpl.content.querySelectorAll('a'))) {
    const href = a.getAttribute('href') ?? ''
    a.removeAttribute('target')
    const user = href.match(userLink)
    if (user) {
      a.setAttribute('data-chat-user', decodeURIComponent(user[1]))
      a.classList.add('pill')
      continue
    }
    if (/^(https?:|mailto:)/i.test(href)) {
      a.setAttribute('data-chat-link', '')
    } else {
      a.removeAttribute('href')
    }
  }

  for (const img of Array.from(tpl.content.querySelectorAll('img'))) {
    const src = img.getAttribute('src') ?? ''
    img.removeAttribute('src')
    if (src.startsWith('mxc://')) {
      img.setAttribute('data-mxc', src)
      img.classList.add(img.hasAttribute('data-mx-emoticon') ? 'emoticon' : 'inline-image')
      images.push(src)
    } else {
      img.remove()
    }
  }

  for (const span of Array.from(tpl.content.querySelectorAll('span[data-mx-spoiler]'))) {
    span.classList.add('spoiler')
    span.setAttribute('tabindex', '0')
  }

  for (const el of Array.from(tpl.content.querySelectorAll('[data-mx-color], [data-mx-bg-color]'))) {
    const fg = el.getAttribute('data-mx-color')
    const bg = el.getAttribute('data-mx-bg-color')
    // only hex survives the sanitiser; anything else is dropped here too.
    const style: string[] = []
    if (fg && /^#[0-9a-f]{6}$/i.test(fg)) {
      style.push(`color:${fg}`)
    }
    if (bg && /^#[0-9a-f]{6}$/i.test(bg)) {
      style.push(`background-color:${bg}`)
    }
    if (style.length) {
      el.setAttribute('style', style.join(';'))
    }
  }

  return { html: tpl.innerHTML, images }
}

/** True when a body is nothing but emoji, which shows larger. */
export function isEmojiOnly(text: string): boolean {
  const trimmed = text.trim()
  if (!trimmed || trimmed.length > 24) {
    return false
  }
  return /^(?:\p{Extended_Pictographic}|\p{Emoji_Component}|‍|️|\s)+$/u.test(trimmed) && /\p{Extended_Pictographic}/u.test(trimmed)
}
