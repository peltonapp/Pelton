// widgets.ts draws the parts of a note the live preview shows as something
// other than text: the properties block, checkboxes, bullets, rules, callout
// titles, tables, images and embedded notes. Everything is built with DOM
// calls and textContent, so nothing in a note is ever parsed as HTML.

import { WidgetType, type EditorView } from '@codemirror/view'
import { classifyLines, inlineTokens, isImagePath, parseFrontMatter, tableAligns, tableCells, type NoteProperty } from '../../../lib/notemd'
import { noteContext, type FollowedLink } from './context'

/** Follows the link a clicked element of a rendered note stands for. */
export function followElement(view: EditorView, el: HTMLElement, event: MouseEvent): boolean {
  const link = linkOf(el)
  if (!link) {
    return false
  }
  event.preventDefault()
  view.state.facet(noteContext).follow(link, event.metaKey || event.ctrlKey)
  return true
}

/** The link a rendered element carries, from its data attributes. */
export function linkOf(el: HTMLElement | null): FollowedLink | null {
  const host = el?.closest<HTMLElement>('[data-nt-kind]')
  if (!host) {
    return null
  }
  const kind = host.dataset.ntKind as FollowedLink['kind']
  return { kind, target: host.dataset.ntTarget ?? '', subpath: host.dataset.ntSubpath ?? '' }
}

function setLink(el: HTMLElement, link: FollowedLink): void {
  el.dataset.ntKind = link.kind
  el.dataset.ntTarget = link.target
  el.dataset.ntSubpath = link.subpath
}

/**
 * Renders a line of inline Markdown into parent: marks become classes and
 * disappear, links become clickable spans carrying their target.
 */
export function renderInline(parent: HTMLElement, text: string, resolve?: (target: string, markdown: boolean) => string): void {
  const tokens = inlineTokens(text)
  const hidden = new Uint8Array(text.length)
  const classes: string[][] = Array.from({ length: text.length }, () => [])
  const links: (FollowedLink | null)[] = new Array(text.length).fill(null)
  const replace = new Map<number, { to: number; label: string }>()
  for (const tok of tokens) {
    for (const [a, b] of tok.marks) {
      hidden.fill(1, a, b)
    }
    let cls = `cm-nt-${tok.kind}`
    let link: FollowedLink | null = null
    switch (tok.kind) {
      case 'wiki':
        cls = resolve && !resolve(tok.target ?? '', false) && tok.target ? 'cm-nt-link cm-nt-unresolved' : 'cm-nt-link'
        link = { kind: 'wiki', target: tok.target ?? '', subpath: tok.subpath ?? '' }
        break
      case 'link':
      case 'url':
        cls = 'cm-nt-link'
        link = { kind: /^[a-z][a-z0-9+.-]*:/i.test(tok.target ?? '') ? 'url' : 'markdown', target: tok.target ?? '', subpath: tok.subpath ?? '' }
        break
      case 'tag':
        link = { kind: 'tag', target: tok.target ?? '', subpath: '' }
        break
      case 'embed':
      case 'image':
        replace.set(tok.from, { to: tok.to, label: tok.label || tok.target || '' })
        break
      case 'comment':
        hidden.fill(1, tok.from, tok.to)
        break
    }
    for (let i = tok.from; i < tok.to; i++) {
      classes[i].push(cls)
      if (link) {
        links[i] = link
      }
    }
  }
  let i = 0
  while (i < text.length) {
    const r = replace.get(i)
    if (r) {
      const span = document.createElement('span')
      span.className = 'cm-nt-embed-label'
      span.textContent = r.label
      parent.append(span)
      i = r.to
      continue
    }
    if (hidden[i]) {
      i++
      continue
    }
    let j = i + 1
    const key = classes[i].join(' ')
    while (j < text.length && !hidden[j] && !replace.has(j) && classes[j].join(' ') === key && links[j] === links[i]) {
      j++
    }
    const chunk = text.slice(i, j)
    if (!key && !links[i]) {
      parent.append(chunk)
    } else {
      const span = document.createElement('span')
      span.className = key
      span.textContent = chunk
      if (links[i]) {
        setLink(span, links[i]!)
      }
      parent.append(span)
    }
    i = j
  }
}

// placeCursor moves the selection into a rendered block so it shows as source.
function placeCursor(view: EditorView, pos: number): void {
  view.dispatch({ selection: { anchor: Math.min(pos, view.state.doc.length) } })
  view.focus()
}

/** The front matter, drawn as a compact list of properties. */
export class PropertiesWidget extends WidgetType {
  constructor(
    readonly source: string,
    readonly pos: number,
    readonly title: string,
  ) {
    super()
  }

  eq(other: PropertiesWidget): boolean {
    return other.source === this.source && other.pos === this.pos
  }

  toDOM(view: EditorView): HTMLElement {
    const props: NoteProperty[] = parseFrontMatter(this.source)?.props ?? []
    const box = document.createElement('div')
    box.className = 'cm-nt-props'
    box.setAttribute('role', 'group')
    box.setAttribute('aria-label', this.title)
    const head = document.createElement('div')
    head.className = 'cm-nt-props-title'
    head.textContent = this.title
    box.append(head)
    for (const p of props) {
      const row = document.createElement('div')
      row.className = 'cm-nt-prop'
      const key = document.createElement('span')
      key.className = 'cm-nt-prop-key'
      key.textContent = p.key
      const value = document.createElement('span')
      value.className = 'cm-nt-prop-value'
      if (p.list) {
        for (const item of p.list) {
          const chip = document.createElement('span')
          chip.className = 'cm-nt-chip'
          chip.textContent = item
          if (p.key.toLowerCase() === 'tags' || p.key.toLowerCase() === 'tag') {
            setLink(chip, { kind: 'tag', target: item.replace(/^#/, ''), subpath: '' })
          }
          value.append(chip)
        }
      } else {
        renderInline(value, p.value)
      }
      row.append(key, value)
      box.append(row)
    }
    box.addEventListener('mousedown', (e) => {
      const target = e.target as HTMLElement
      if (followElement(view, target, e)) {
        return
      }
      e.preventDefault()
      placeCursor(view, this.pos + 4)
    })
    return box
  }

  ignoreEvent(): boolean {
    return true
  }
}

/** A task checkbox that ticks the task in the text when clicked. */
export class CheckboxWidget extends WidgetType {
  constructor(
    readonly checked: boolean,
    readonly pos: number,
  ) {
    super()
  }

  eq(other: CheckboxWidget): boolean {
    return other.checked === this.checked && other.pos === this.pos
  }

  toDOM(view: EditorView): HTMLElement {
    const box = document.createElement('input')
    box.type = 'checkbox'
    box.className = 'cm-nt-check'
    box.checked = this.checked
    box.addEventListener('mousedown', (e) => e.preventDefault())
    box.addEventListener('click', (e) => {
      e.preventDefault()
      if (view.state.readOnly) {
        return
      }
      const at = this.pos + 1
      const current = view.state.sliceDoc(at, at + 1)
      view.dispatch({ changes: { from: at, to: at + 1, insert: current === ' ' ? 'x' : ' ' }, userEvent: 'input.toggle' })
    })
    return box
  }

  ignoreEvent(): boolean {
    return true
  }
}

/** A plain widget drawing one element with a class and optional text. */
export class MarkerWidget extends WidgetType {
  constructor(
    readonly cls: string,
    readonly text = '',
  ) {
    super()
  }

  eq(other: MarkerWidget): boolean {
    return other.cls === this.cls && other.text === this.text
  }

  toDOM(): HTMLElement {
    const el = document.createElement('span')
    el.className = this.cls
    el.textContent = this.text
    el.setAttribute('aria-hidden', 'true')
    return el
  }
}

/** A table, drawn when the cursor is outside it; a click puts the cursor in the source. */
export class TableWidget extends WidgetType {
  constructor(
    readonly source: string,
    readonly pos: number,
  ) {
    super()
  }

  eq(other: TableWidget): boolean {
    return other.source === this.source && other.pos === this.pos
  }

  toDOM(view: EditorView): HTMLElement {
    const ctx = view.state.facet(noteContext)
    const rows = this.source.split('\n').map((r) => r.replace(/\r$/, ''))
    const aligns = rows[1] ? tableAligns(rows[1]) : []
    const wrap = document.createElement('div')
    wrap.className = 'cm-nt-table-wrap'
    const table = document.createElement('table')
    table.className = 'cm-nt-table'
    rows.forEach((row, r) => {
      if (r === 1) {
        return
      }
      const tr = document.createElement('tr')
      tableCells(row).forEach((cell, c) => {
        const td = document.createElement(r === 0 ? 'th' : 'td')
        if (aligns[c]) {
          td.style.textAlign = aligns[c]
        }
        renderInline(td, cell.replace(/\\\|/g, '|'), ctx.resolve)
        tr.append(td)
      })
      ;(r === 0 ? (table.createTHead()) : (table.tBodies[0] ?? table.createTBody())).append(tr)
    })
    wrap.append(table)
    wrap.addEventListener('mousedown', (e) => {
      if (followElement(view, e.target as HTMLElement, e)) {
        return
      }
      e.preventDefault()
      placeCursor(view, this.pos)
    })
    return wrap
  }

  ignoreEvent(): boolean {
    return true
  }
}

const imageCache = new Map<string, Promise<string>>()

/** Forgets cached image data, after files of the vault changed. */
export function clearImageCache(): void {
  imageCache.clear()
}

/** An image from the vault or, once asked for, from the web. */
export class ImageWidget extends WidgetType {
  constructor(
    readonly target: string,
    readonly alt: string,
    readonly remote: boolean,
    readonly width: number,
    readonly pos: number,
  ) {
    super()
  }

  eq(other: ImageWidget): boolean {
    return other.target === this.target && other.alt === this.alt && other.width === this.width && other.pos === this.pos
  }

  toDOM(view: EditorView): HTMLElement {
    const ctx = view.state.facet(noteContext)
    const wrap = document.createElement('span')
    wrap.className = 'cm-nt-image'
    const img = document.createElement('img')
    img.alt = this.alt
    if (this.width > 0) {
      img.style.width = `${this.width}px`
    }
    if (this.remote) {
      const button = document.createElement('button')
      button.type = 'button'
      button.className = 'cm-nt-remote'
      button.textContent = ctx.t('notes.editor.loadRemote')
      button.title = this.target
      button.addEventListener('mousedown', (e) => e.preventDefault())
      button.addEventListener('click', (e) => {
        e.preventDefault()
        img.src = this.target
        button.replaceWith(img)
      })
      wrap.append(button)
      return wrap
    }
    const path = ctx.resolve(this.target, false) || this.target
    let data = imageCache.get(path)
    if (!data) {
      data = ctx.fileData(path)
      imageCache.set(path, data)
      data.catch(() => imageCache.delete(path))
    }
    data.then(
      (src) => {
        img.src = src
      },
      () => {
        wrap.classList.add('cm-nt-missing')
        wrap.textContent = this.alt || this.target
      },
    )
    wrap.append(img)
    wrap.addEventListener('mousedown', (e) => {
      e.preventDefault()
      placeCursor(view, this.pos)
    })
    return wrap
  }

  ignoreEvent(): boolean {
    return true
  }
}

/** Another note shown inside this one, or one of its sections. */
export class NoteEmbedWidget extends WidgetType {
  constructor(
    readonly target: string,
    readonly subpath: string,
    readonly pos: number,
  ) {
    super()
  }

  eq(other: NoteEmbedWidget): boolean {
    return other.target === this.target && other.subpath === this.subpath && other.pos === this.pos
  }

  toDOM(view: EditorView): HTMLElement {
    const ctx = view.state.facet(noteContext)
    const box = document.createElement('div')
    box.className = 'cm-nt-embed'
    const head = document.createElement('div')
    head.className = 'cm-nt-embed-title cm-nt-link'
    head.textContent = this.subpath ? `${this.target} > ${this.subpath}` : this.target
    setLink(head, { kind: 'wiki', target: this.target, subpath: this.subpath })
    const body = document.createElement('div')
    body.className = 'cm-nt-embed-body'
    box.append(head, body)
    const path = ctx.resolve(this.target, false)
    if (!path || path === ctx.path) {
      box.classList.add('cm-nt-missing')
      body.textContent = path === ctx.path ? '' : ctx.t('notes.editor.missingNote')
    } else if (isImagePath(path)) {
      return new ImageWidget(this.target, this.target, false, 0, this.pos).toDOM(view)
    } else {
      ctx.readNote(path).then(
        (text) => renderExcerpt(body, text, this.subpath, ctx.resolve),
        () => {
          box.classList.add('cm-nt-missing')
          body.textContent = ctx.t('notes.editor.missingNote')
        },
      )
    }
    box.addEventListener('mousedown', (e) => {
      if (followElement(view, e.target as HTMLElement, e)) {
        return
      }
      e.preventDefault()
      placeCursor(view, this.pos)
    })
    return box
  }

  ignoreEvent(): boolean {
    return true
  }
}

const excerptLines = 40

/**
 * Renders a note, or the section under one of its headings, as plain lines:
 * headings bold, list markers kept, code and front matter left out.
 */
export function renderExcerpt(parent: HTMLElement, text: string, subpath: string, resolve?: (target: string, markdown: boolean) => string): void {
  const lines = text.split('\n').map((l) => l.replace(/\r$/, ''))
  const kinds = classifyLines(lines)
  let start = 0
  let end = lines.length
  if (subpath) {
    const want = subpath.toLowerCase().replace(/^\^/, '')
    const at = kinds.findIndex((k, i) => k.type === 'heading' && lines[i].slice(k.markEnd).trim().toLowerCase() === want)
    if (at >= 0) {
      const level = (kinds[at] as { level: number }).level
      start = at
      end = kinds.findIndex((k, i) => i > at && k.type === 'heading' && k.level <= level)
      if (end < 0) {
        end = lines.length
      }
    }
  }
  let shown = 0
  for (let i = start; i < end && shown < excerptLines; i++) {
    const k = kinds[i]
    if (k.type === 'frontmatter' || k.type === 'fence' || k.type === 'code' || k.type === 'blank') {
      continue
    }
    const p = document.createElement('div')
    p.className = 'cm-nt-excerpt-line'
    let content = lines[i]
    if (k.type === 'heading') {
      p.classList.add('cm-nt-excerpt-heading')
      content = content.slice(k.markEnd)
    } else if (k.type === 'quote') {
      content = content.slice(k.markEnd)
    } else if (k.type === 'list' && !k.ordered) {
      content = `• ${content.slice(k.task ? k.task.from + 4 : k.markEnd)}`
    }
    renderInline(p, content, resolve)
    parent.append(p)
    shown++
  }
}
