<script lang="ts">
  // the Notes space of the new design: a folder of Markdown files shown in
  // three panes like mail. the sidebar holds the folder tree, tags, starred
  // and recent notes and the daily notes calendar; the list shows the notes of
  // what the sidebar chose; the editor shows open notes in tabs, with a panel
  // beside it for backlinks, the outline, outgoing links and the local graph.
  // the graph of the whole folder takes the editor's place when asked for.
  // panes fold in a narrow window: the panel first slides over the editor,
  // then the list does, then the sidebar becomes a drawer. it is code-split
  // like the other spaces and only loads when opened.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import xIcon from '@phosphor-icons/core/assets/regular/x.svg?raw'
  import caretLeft from '@phosphor-icons/core/assets/regular/caret-left.svg?raw'
  import caretRight from '@phosphor-icons/core/assets/regular/caret-right.svg?raw'
  import starIcon from '@phosphor-icons/core/assets/duotone/star-duotone.svg?raw'
  import panelIcon from '@phosphor-icons/core/assets/duotone/sidebar-duotone.svg?raw'
  import listIcon from '@phosphor-icons/core/assets/duotone/list-bullets-duotone.svg?raw'
  import moreIcon from '@phosphor-icons/core/assets/regular/dots-three.svg?raw'
  import notesIcon from '@phosphor-icons/core/assets/duotone/notebook-duotone.svg?raw'
  import '../calendar/controls.css'
  import Ph from '../common/Ph.svelte'
  import Modal from '../common/Modal.svelte'
  import PieProgress from '../common/PieProgress.svelte'
  import Select from '../common/Select.svelte'
  import Segmented from '../settings2/ui/Segmented.svelte'
  import NoteSidebar from './NoteSidebar.svelte'
  import NoteList from './NoteList.svelte'
  import NoteEditor from './NoteEditor.svelte'
  import NotePanel from './NotePanel.svelte'
  import NoteGraph from './NoteGraph.svelte'
  import NoteSwitcher from './NoteSwitcher.svelte'
  import type { CompletionItem, FollowedLink, NoteContext, PreviewMode } from './editor/context'
  import {
    activeNote,
    closeNote,
    createFolder,
    createFromTemplate,
    createNote,
    deleteNote,
    ensureVault,
    moveNote,
    noteInsert,
    noteLine,
    noteRequest,
    noteResolver,
    noteTags,
    notesByPath,
    openDaily,
    openNote,
    openNotes,
    recentNotes,
    startNotes,
    starredNotes,
    templateNotes,
    toggleStar,
    vaultIndex,
    vaultInfo,
    vaultSwitched,
    vaultNotes,
  } from '../../stores/notes'
  import { getSetting, setSetting, SettingKeys, vaultChooseFolder, vaultFileData, vaultRead, vaultRescan, vaultSaveAttachment, vaultSearch } from '../../lib/api'
  import { onVaultChanged } from '../../lib/events'
  import { fuzzyMatch } from '../../lib/fuzzy'
  import { buildGraph, defaultGraphOptions, type GraphOptions } from '../../lib/notegraph'
  import { notesInScope, sortNotes, noteSorts, type NoteScope, type NoteSort } from '../../lib/notelist'
  import { isImagePath, joinPath, noteFolder, noteHeadings, noteTitle, safeNoteName } from '../../lib/notemd'
  import { linkText, resolveLink, resolveMarkdownLink } from '../../lib/noteresolve'
  import { dailyNoteDate } from '../../lib/notetemplate'
  import { openPeltonUrl } from '../../lib/peltonopen'
  import { addDays, resolveWeekStart } from '../../lib/caldate'
  import { openURL } from '../../lib/browser'
  import { focusSpaceSearch, registerSpaceSearch } from '../../lib/spacesearch'
  import { popupIsOpen } from '../../lib/popups'
  import { registerSpaceActions } from '../../stores/space'
  import { calendarSettings, loadCalendarSettings } from '../../stores/calendar'
  import { openContextMenu } from '../../stores/contextmenu'
  import { askConfirm } from '../../stores/confirm'
  import { prefs, setNotesEditorMode, setNotesVaultPref } from '../../stores/prefs'
  import { errorMessage, toastError } from '../../stores/toast'
  import { dur, motion } from '../../lib/motion'
  import { t } from '../../lib/i18n'
  import { bodyFontStack } from '../../lib/fonts'
  import type { NoteEditorMode } from '../../lib/types'

  const narrowPanel = 1280
  const narrowList = 1000
  const narrowSide = 740

  let width = 0
  let sideOpen = false
  let listOpen = false
  let panelOpen = true
  let panelTab: 'backlinks' | 'outline' | 'links' | 'graph' = 'backlinks'
  let graphOpen = false
  let graphOptions: GraphOptions = { ...defaultGraphOptions }
  let scope: NoteScope = { kind: 'all' }
  let query = ''
  let sort: NoteSort = 'modified'
  let searchHits: string[] | null = null
  let contexts = new Map<string, string>()
  let searching = false
  let day = new Date()
  let editor: NoteEditor
  let list: NoteList
  let switcher: 'open' | 'template' | null = null
  let prompt: { kind: 'folder' | 'rename'; path: string; value: string } | null = null
  let titleDraft = ''
  let headings: { level: number; text: string; line: number }[] = []
  let headingsTimer: ReturnType<typeof setTimeout> | null = null
  let revision = 0
  let modeOverride: PreviewMode | null = null
  let loading = true

  let unregister: (() => void) | undefined
  let stopSearch: (() => void) | undefined
  let stopNotes: (() => void) | undefined
  let stopChanges: (() => void) | undefined

  $: foldPanel = width > 0 && width < narrowPanel
  $: foldList = width > 0 && width < narrowList
  $: foldSide = width > 0 && width < narrowSide
  $: if (!foldSide) {
    sideOpen = false
  }
  $: if (!foldList) {
    listOpen = false
  }
  $: mode = (modeOverride ?? $prefs.notesEditorMode) as PreviewMode
  $: weekStart = resolveWeekStart($calendarSettings.weekStart, navigator.language)
  $: active = $activeNote
  $: activeInfo = active ? $notesByPath.get(active) : undefined
  $: if (active !== lastActive) {
    lastActive = active
    titleDraft = active ? noteTitle(active) : ''
    headings = activeInfo ? activeInfo.headings : []
  }
  let lastActive: string | null = null
  $: dailyDate = active ? dailyNoteDate(active, $prefs.notesDailyFolder, $prefs.notesDailyFormat) : null
  $: scoped = notesInScope($vaultNotes, scope, $starredNotes)
  $: shown = searchHits ? orderByHits(scoped, searchHits) : sortNotes(scoped, sort)
  $: listTitle = scopeTitle(scope)
  $: graph = graphOpen ? buildGraph($vaultNotes, graphOptions) : { nodes: [], edges: [] }
  $: noteFont = bodyFontStack($prefs.bodyFont) ?? 'var(--font-ui)'
  $: noteWidth = $prefs.notesLineWidth === 'narrow' ? 'var(--reader-width-narrow)' : $prefs.notesLineWidth === 'medium' ? 'var(--reader-width-medium)' : '100%'
  $: rtl = typeof document !== 'undefined' && document.documentElement.dir === 'rtl'
  $: void runSearch(query)
  $: folderItems = [{ value: '', label: $t('notes.graph.allFolders') }, ...($vaultIndex?.folders ?? []).map((f) => ({ value: f, label: f }))]
  $: tagItems = [{ value: '', label: $t('notes.graph.allTags') }, ...$noteTags.map((x) => ({ value: x.tag, label: `#${x.tag}` }))]
  $: modeOptions = (['live', 'source', 'reading'] as const).map((key) => ({ key, label: $t(`notes.mode.${key}`) }))

  function orderByHits<T extends { path: string }>(notes: T[], hits: string[]): T[] {
    const map = new Map(notes.map((n) => [n.path, n]))
    return hits.map((p) => map.get(p)).filter((n): n is T => !!n)
  }

  function scopeTitle(s: NoteScope): string {
    switch (s.kind) {
      case 'all':
        return $t('notes.allNotes')
      case 'starred':
        return $t('notes.starred')
      case 'tag':
        return `#${s.tag}`
      case 'folder':
        return s.path.split('/').pop() || $t('notes.allNotes')
    }
  }

  let searchToken = 0
  async function runSearch(q: string): Promise<void> {
    const mine = ++searchToken
    if (!q.trim()) {
      searchHits = null
      contexts = new Map()
      searching = false
      return
    }
    searching = true
    await new Promise((r) => setTimeout(r, 180))
    if (mine !== searchToken) {
      return
    }
    try {
      const hits = await vaultSearch(q)
      if (mine !== searchToken) {
        return
      }
      const lower = q.trim().toLowerCase()
      const titleHits = $vaultNotes.filter((n) => n.title.toLowerCase().includes(lower) || n.aliases.some((a) => a.toLowerCase().includes(lower))).map((n) => n.path)
      searchHits = [...new Set([...titleHits, ...hits.map((h) => h.path)])]
      contexts = new Map(hits.filter((h) => h.context).map((h) => [h.path, h.context]))
    } catch (err) {
      toastError(errorMessage(err))
    } finally {
      if (mine === searchToken) {
        searching = false
      }
    }
  }

  onMount(() => {
    stopNotes = startNotes()
    void loadCalendarSettings()
    void (async () => {
      try {
        await ensureVault()
        const s = await getSetting(SettingKeys.notesSort)
        if (s.found && noteSorts.includes(s.value as NoteSort)) {
          sort = s.value as NoteSort
        }
      } catch (err) {
        toastError(errorMessage(err))
      } finally {
        loading = false
      }
    })()
    stopChanges = onVaultChanged(() => {
      revision += 1
    })
    unregister = registerSpaceActions(
      'notes',
      {
        compose: () => whenFree(() => void newNote(currentFolder())),
        search: () => {
          if (!focusSpaceSearch()) {
            list?.focusFilter()
          }
        },
        sync: () => void vaultRescan(),
        'close-tab': () => whenFree(() => active && void closeActive()),
        'notes-new': () => whenFree(() => void newNote(currentFolder())),
        'notes-today': () => whenFree(() => void today()),
        'notes-switcher': () => whenFree(() => (switcher = 'open')),
        'notes-toggle-mode': () => whenFree(toggleMode),
        'notes-graph': () => whenFree(() => (graphOpen = !graphOpen)),
      },
      { compose: 'notes.newNote' },
    )
    stopSearch = registerSpaceSearch('notes', {
      placeholderKey: 'notes.searchPlaceholder',
      emptyKey: 'notes.noMatches',
      search: async (q) => {
        const lower = q.toLowerCase()
        const titles = $vaultNotes
          .filter((n) => n.title.toLowerCase().includes(lower) || n.aliases.some((a) => a.toLowerCase().includes(lower)))
          .slice(0, 8)
          .map((n) => ({ id: n.path, title: n.title, detail: n.snippet, aside: noteFolder(n.path) }))
        const seen = new Set(titles.map((x) => x.id))
        let text: { id: string; title: string; detail: string; aside: string }[] = []
        try {
          text = (await vaultSearch(q))
            .filter((h) => !seen.has(h.path))
            .slice(0, 14)
            .map((h) => ({ id: h.path, title: h.title, detail: h.context, aside: noteFolder(h.path) }))
        } catch {
          text = []
        }
        return [...titles, ...text]
      },
      open: (result) => openNote(result.id),
    })
  })

  onDestroy(() => {
    unregister?.()
    stopSearch?.()
    stopNotes?.()
    stopChanges?.()
    if (headingsTimer) {
      clearTimeout(headingsTimer)
    }
  })

  function whenFree(run: () => void): void {
    if (!popupIsOpen() && !switcher && !prompt) {
      run()
    }
  }

  // --- requests from other spaces ----------------------------------------------------

  $: if ($noteRequest && !loading) {
    const r = $noteRequest
    noteRequest.set(null)
    void tick().then(() => {
      switch (r.kind) {
        case 'new':
          void newNote(currentFolder())
          break
        case 'today':
          void today()
          break
        case 'switcher':
          switcher = 'open'
          break
        case 'template':
          switcher = 'template'
          break
        case 'graph':
          graphOpen = true
          break
        case 'toggle-mode':
          toggleMode()
          break
      }
    })
  }

  // --- actions -----------------------------------------------------------------------

  function currentFolder(): string {
    if (scope.kind === 'folder') {
      return scope.path
    }
    return ''
  }

  async function newNote(folder: string, title = $t('notes.untitled')): Promise<void> {
    graphOpen = false
    const path = await createNote(folder, title)
    if (path) {
      afterOpen()
      await tick()
      focusTitle()
    }
  }

  function focusTitle(): void {
    const el = document.querySelector<HTMLInputElement>('.notes-title-input')
    el?.focus()
    el?.select()
  }

  async function today(): Promise<void> {
    graphOpen = false
    day = new Date()
    await openDaily(day)
    afterOpen()
  }

  async function goDay(d: Date): Promise<void> {
    graphOpen = false
    day = d
    await openDaily(d)
    afterOpen()
  }

  function afterOpen(): void {
    if (foldList) {
      listOpen = false
    }
    if (foldSide) {
      sideOpen = false
    }
  }

  function open(path: string, newTab = false, line?: number): void {
    graphOpen = false
    if (!/\.md$/i.test(path)) {
      return
    }
    openNote(path, { newTab, line })
    afterOpen()
  }

  async function closeActive(): Promise<void> {
    if (!active) {
      return
    }
    await editor?.flush()
    closeNote(active)
  }

  function toggleMode(): void {
    const next: NoteEditorMode = mode === 'reading' ? 'live' : mode === 'live' ? 'reading' : 'live'
    modeOverride = next
  }

  function setMode(next: PreviewMode): void {
    modeOverride = null
    setNotesEditorMode(next)
  }

  async function renameActive(): Promise<void> {
    if (!active) {
      return
    }
    const name = safeNoteName(titleDraft)
    if (!titleDraft.trim() || name === noteTitle(active)) {
      titleDraft = noteTitle(active)
      return
    }
    await editor?.flush()
    const ok = await moveNote(active, joinPath(noteFolder(active), `${name}.md`))
    if (!ok) {
      titleDraft = noteTitle(active)
    }
  }

  async function chooseVault(): Promise<void> {
    try {
      const info = await vaultChooseFolder()
      if (!info.folder && !info.error) {
        return
      }
      if (info.folder) {
        setNotesVaultPref(info.folder)
      }
      await vaultSwitched(info)
      scope = { kind: 'all' }
    } catch (err) {
      toastError(errorMessage(err))
    }
  }

  function noteMenu(path: string, x: number, y: number): void {
    const starred = $starredNotes.includes(path)
    openContextMenu(x, y, [
      { label: $t('notes.menu.open'), action: () => open(path) },
      { label: $t('notes.menu.openTab'), action: () => open(path, true) },
      { label: starred ? $t('notes.menu.unstar') : $t('notes.menu.star'), action: () => toggleStar(path) },
      { label: $t('notes.menu.rename'), action: () => (prompt = { kind: 'rename', path, value: noteTitle(path) }) },
      { label: $t('notes.menu.copyLink'), action: () => void navigator.clipboard?.writeText(`[[${linkText($noteResolver, path)}]]`) },
      'separator',
      { label: $t('notes.menu.delete'), action: () => void deleteNote(path), danger: true },
    ])
  }

  function folderMenu(path: string, x: number, y: number): void {
    openContextMenu(x, y, [
      { label: $t('notes.newNote'), action: () => void newNote(path) },
      { label: $t('notes.newFolder'), action: () => (prompt = { kind: 'folder', path, value: '' }) },
      { label: $t('notes.menu.rename'), action: () => (prompt = { kind: 'rename', path, value: path.split('/').pop() ?? '' }) },
      'separator',
      { label: $t('notes.menu.deleteFolder'), action: () => void removeFolder(path), danger: true },
    ])
  }

  async function removeFolder(path: string): Promise<void> {
    const count = $vaultNotes.filter((n) => n.path.startsWith(`${path}/`)).length
    const ok = await askConfirm({
      title: $t('notes.deleteFolder.title').replace('{name}', path.split('/').pop() ?? path),
      body: count ? $t('notes.deleteFolder.body').replace('{n}', String(count)) : '',
      confirmLabel: $t('notes.menu.deleteFolder'),
      danger: true,
    })
    if (ok && (await deleteNote(path)) && scope.kind === 'folder' && (scope.path === path || scope.path.startsWith(`${path}/`))) {
      scope = { kind: 'all' }
    }
  }

  function moreMenu(e: MouseEvent): void {
    if (!active) {
      return
    }
    const path = active
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    openContextMenu(r.left, r.bottom, [
      { label: $t('notes.menu.copyLink'), action: () => void navigator.clipboard?.writeText(`[[${linkText($noteResolver, path)}]]`) },
      { label: $t('notes.menu.showInList'), action: () => (scope = { kind: 'folder', path: noteFolder(path) }) },
      { label: $t('notes.menu.rescan'), action: () => void vaultRescan() },
      'separator',
      { label: $t('notes.menu.delete'), action: () => void deleteNote(path), danger: true },
    ])
  }

  async function submitPrompt(): Promise<void> {
    if (!prompt) {
      return
    }
    const p = prompt
    const value = p.value.trim()
    prompt = null
    if (!value) {
      return
    }
    if (p.kind === 'folder') {
      const made = await createFolder(p.path, value)
      if (made) {
        scope = { kind: 'folder', path: made }
      }
      return
    }
    const isNote = /\.md$/i.test(p.path)
    const target = joinPath(noteFolder(p.path), isNote ? `${safeNoteName(value)}.md` : safeNoteName(value))
    if (p.path === active) {
      await editor?.flush()
    }
    const ok = await moveNote(p.path, target)
    if (ok && !isNote && scope.kind === 'folder' && scope.path === p.path) {
      scope = { kind: 'folder', path: target }
    }
  }

  async function dropOnFolder(from: string, folder: string): Promise<void> {
    const name = from.split('/').pop() ?? from
    const to = joinPath(folder, name)
    if (to === from || folder === from || folder.startsWith(`${from}/`) || noteFolder(from) === folder) {
      return
    }
    if (from === active) {
      await editor?.flush()
    }
    await moveNote(from, to)
  }

  function setSort(next: NoteSort): void {
    sort = next
    void setSetting(SettingKeys.notesSort, next)
  }

  async function pickTemplate(path: string): Promise<void> {
    switcher = null
    graphOpen = false
    const made = await createFromTemplate(path, currentFolder(), $t('notes.untitled'))
    if (made) {
      afterOpen()
      await tick()
      focusTitle()
    }
  }

  function headingsChanged(): void {
    if (headingsTimer) {
      clearTimeout(headingsTimer)
    }
    headingsTimer = setTimeout(() => {
      headingsTimer = null
      headings = editor ? noteHeadings(editor.currentText()) : []
    }, 250)
  }

  // --- the editor's view of the notes space --------------------------------------------

  function headingLine(path: string, subpath: string): number | undefined {
    if (!subpath) {
      return undefined
    }
    const want = subpath.toLowerCase()
    return $notesByPath.get(path)?.headings.find((h) => h.text.toLowerCase() === want)?.line
  }

  async function follow(link: FollowedLink, newTab: boolean): Promise<void> {
    if (link.kind === 'tag') {
      scope = { kind: 'tag', tag: link.target }
      query = ''
      if (foldList) {
        listOpen = true
      }
      return
    }
    if (link.kind === 'url') {
      if (!(await openPeltonUrl(link.target)) && /^(https?|mailto):/i.test(link.target)) {
        openURL(link.target)
      }
      return
    }
    const from = active ?? ''
    const target = link.kind === 'markdown' ? resolveMarkdownLink($noteResolver, link.target, from) : resolveLink($noteResolver, link.target, from)
    if (target) {
      if (/\.md$/i.test(target)) {
        open(target, newTab, headingLine(target, link.subpath))
      }
      return
    }
    if (link.kind === 'wiki' && link.target.trim()) {
      // a link to a note that does not exist yet makes it, like Obsidian.
      const name = link.target.trim().split('/')
      const title = name.pop() ?? link.target
      const folder = name.join('/')
      const made = await createNote(folder, title, '', false)
      if (made) {
        open(made, newTab)
      }
    }
  }

  function completeNotes(q: string): CompletionItem[] {
    const text = q.trim()
    const r = $noteResolver
    const items: { item: CompletionItem; score: number }[] = []
    for (const n of $vaultNotes) {
      if (n.path === active) {
        continue
      }
      const name = linkText(r, n.path)
      if (!text) {
        items.push({ item: { label: n.title, detail: noteFolder(n.path), insert: name }, score: n.mtime })
        continue
      }
      const s = fuzzyMatch(text, n.title)?.score
      if (s !== undefined) {
        items.push({ item: { label: n.title, detail: noteFolder(n.path), insert: name }, score: s * 1e13 })
      }
      for (const a of n.aliases) {
        const sa = fuzzyMatch(text, a)?.score
        if (sa !== undefined) {
          items.push({ item: { label: a, detail: n.title, insert: `${name}|${a}` }, score: sa * 1e13 - 1 })
        }
      }
    }
    for (const f of $vaultIndex?.files ?? []) {
      if (!isImagePath(f)) {
        continue
      }
      const base = f.split('/').pop() ?? f
      const s = text ? fuzzyMatch(text, base)?.score : 0
      if (s !== undefined) {
        items.push({ item: { label: base, detail: noteFolder(f), insert: linkText(r, f) }, score: text ? s * 1e13 - 2 : -1 })
      }
    }
    const out = items.sort((a, b) => b.score - a.score).slice(0, 12).map((x) => x.item)
    if (text && !$vaultNotes.some((n) => n.title.toLowerCase() === text.toLowerCase())) {
      out.push({ label: text, detail: $t('notes.switcher.create'), insert: text, create: true })
    }
    return out
  }

  function completeHeadings(note: string, q: string): CompletionItem[] {
    const path = note.trim() ? resolveLink($noteResolver, note, active ?? '') : active
    if (!path) {
      return []
    }
    const list = path === active ? headings : ($notesByPath.get(path)?.headings ?? [])
    const lower = q.toLowerCase()
    return list
      .filter((h) => h.text && h.text.toLowerCase().includes(lower))
      .slice(0, 12)
      .map((h) => ({ label: h.text, detail: '#'.repeat(h.level), insert: h.text }))
  }

  function completeTags(q: string): CompletionItem[] {
    const lower = q.toLowerCase()
    return $noteTags
      .filter((x) => x.tag.toLowerCase().startsWith(lower) && x.tag.toLowerCase() !== lower)
      .slice(0, 10)
      .map((x) => ({ label: `#${x.tag}`, detail: String(x.count), insert: x.tag }))
  }

  $: context = {
    resolve: (target: string, markdown: boolean) => (markdown ? resolveMarkdownLink($noteResolver, target, active ?? '') : resolveLink($noteResolver, target, active ?? '')),
    fileData: (path: string) => vaultFileData(path),
    readNote: (path: string) => vaultRead(path).then((f) => f.content),
    follow: (link: FollowedLink, newTab: boolean) => void follow(link, newTab),
    completeNotes,
    completeHeadings,
    completeTags,
    saveAttachment: (name: string, base64: string) => vaultSaveAttachment(name, base64),
    t: (key: string) => $t(key),
  } satisfies Omit<NoteContext, 'path'>

  function graphOpenNode(id: string, kind: string, newTab: boolean): void {
    if (kind === 'note') {
      open(id, newTab)
    } else if (kind === 'tag') {
      scope = { kind: 'tag', tag: id.replace(/^#/, '') }
    } else if (kind === 'object') {
      void openPeltonUrl(id)
    } else if (kind === 'missing') {
      void follow({ kind: 'wiki', target: id.replace(/^missing:/, ''), subpath: '' }, newTab)
    }
  }

  // links to mail dropped on the Notes entry go in at the cursor.
  $: if ($noteInsert && editor && active) {
    const text = $noteInsert
    noteInsert.set(null)
    editor.insertText(text)
  }

  // a line asked for from elsewhere (a backlink, the outline, a search hit).
  $: if ($noteLine && editor && $noteLine.path === active) {
    const l = $noteLine.line
    noteLine.set(null)
    void tick().then(() => editor?.goToLine(l))
  }

  function onKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape' && !popupIsOpen()) {
      if (sideOpen) {
        sideOpen = false
      } else if (listOpen) {
        listOpen = false
      } else if (graphOpen) {
        graphOpen = false
      } else {
        return
      }
      e.preventDefault()
    }
  }
</script>

<!-- svelte-ignore a11y-no-noninteractive-element-interactions -->
<section
  class="screen"
  aria-label={$t('space.notes')}
  bind:clientWidth={width}
  on:keydown={onKeydown}
  style={`--note-font:${noteFont};--note-width:${noteWidth}`}
>
  {#if !foldSide || sideOpen}
    <div class="side" class:drawer={foldSide} transition:fly={{ x: foldSide ? (rtl ? 24 : -24) : 0, duration: dur(foldSide ? motion.base : 0) }}>
      <NoteSidebar
        {scope}
        {day}
        {active}
        {graphOpen}
        {weekStart}
        on:scope={(e) => {
          scope = e.detail
          query = ''
          graphOpen = false
          if (foldList) listOpen = true
          if (foldSide) sideOpen = false
        }}
        on:open={(e) => open(e.detail)}
        on:newNote={(e) => void newNote(e.detail)}
        on:newFolder={(e) => (prompt = { kind: 'folder', path: e.detail, value: '' })}
        on:today={() => void today()}
        on:day={(e) => void goDay(e.detail)}
        on:template={() => (switcher = 'template')}
        on:graph={() => (graphOpen = !graphOpen)}
        on:folderMenu={(e) => folderMenu(e.detail.path, e.detail.x, e.detail.y)}
        on:noteMenu={(e) => noteMenu(e.detail.path, e.detail.x, e.detail.y)}
        on:drop={(e) => void dropOnFolder(e.detail.from, e.detail.folder)}
      />
    </div>
  {/if}
  {#if (foldSide && sideOpen) || (foldList && listOpen)}
    <!-- svelte-ignore a11y-click-events-have-key-events a11y-no-static-element-interactions -->
    <div class="scrim" transition:fade={{ duration: dur(motion.fast) }} on:click={() => ((sideOpen = false), (listOpen = false))}></div>
  {/if}

  {#if !foldList || listOpen || !active}
    <div class="list-pane" class:drawer={foldList && active} transition:fly={{ x: foldList && active ? (rtl ? 24 : -24) : 0, duration: dur(foldList && active ? motion.base : 0) }}>
      <NoteList
        bind:this={list}
        notes={shown}
        title={listTitle}
        {active}
        {query}
        {sort}
        {contexts}
        {searching}
        showSidebarButton={foldSide}
        sidebarOpen={sideOpen}
        on:open={(e) => open(e.detail.path, e.detail.newTab)}
        on:menu={(e) => noteMenu(e.detail.path, e.detail.x, e.detail.y)}
        on:query={(e) => (query = e.detail)}
        on:sort={(e) => setSort(e.detail)}
        on:newNote={() => void newNote(currentFolder())}
        on:sidebar={() => (sideOpen = !sideOpen)}
      />
    </div>
  {/if}

  <div class="main" class:hidden={foldList && !active && !graphOpen}>
    {#if $openNotes.length > 0 && !graphOpen}
      <div class="tabs" role="tablist" aria-label={$t('notes.tabs')}>
        {#if foldList}
          <button type="button" class="cal-icon" aria-label={$t('notes.showList')} on:click={() => (listOpen = !listOpen)}>
            <Ph svg={listIcon} size={16} />
          </button>
        {/if}
        {#each $openNotes as p (p)}
          <div class="tab" class:on={p === active} role="presentation">
            <button
              type="button"
              role="tab"
              class="tab-name"
              aria-selected={p === active}
              title={p}
              on:click={() => openNote(p)}
              on:auxclick={(e) => e.button === 1 && closeNote(p)}
            >{noteTitle(p)}</button>
            <button type="button" class="tab-close" aria-label={$t('notes.closeTab').replace('{name}', noteTitle(p))} on:click={() => (p === active ? void closeActive() : closeNote(p))}>
              <Ph svg={xIcon} size={11} />
            </button>
          </div>
        {/each}
      </div>
    {/if}

    {#if graphOpen}
      <header class="toolbar graph-bar">
        {#if foldList}
          <button type="button" class="cal-icon" aria-label={$t('notes.showList')} on:click={() => (listOpen = !listOpen)}>
            <Ph svg={listIcon} size={16} />
          </button>
        {/if}
        <h2 class="graph-title">{$t('notes.graph')}</h2>
        <input class="cal-field graph-filter" type="search" placeholder={$t('notes.graph.filter')} aria-label={$t('notes.graph.filter')} bind:value={graphOptions.query} />
        <Select value={graphOptions.folder} items={folderItems} ariaLabel={$t('notes.graph.folder')} on:change={(e) => (graphOptions = { ...graphOptions, folder: e.detail })} />
        <Select value={graphOptions.tag} items={tagItems} ariaLabel={$t('notes.graph.tag')} on:change={(e) => (graphOptions = { ...graphOptions, tag: e.detail })} />
        <label class="check"><input type="checkbox" bind:checked={graphOptions.tags} /> {$t('notes.graph.tags')}</label>
        <label class="check"><input type="checkbox" bind:checked={graphOptions.objects} /> {$t('notes.graph.objects')}</label>
        <label class="check"><input type="checkbox" bind:checked={graphOptions.orphans} /> {$t('notes.graph.orphans')}</label>
        <span class="spacer"></span>
        <button type="button" class="cal-icon" aria-label={$t('notes.graph.close')} on:click={() => (graphOpen = false)}>
          <Ph svg={xIcon} size={14} />
        </button>
      </header>
      <div class="graph-host">
        <NoteGraph {graph} current={active ?? ''} label={$t('notes.graph')} on:open={(e) => graphOpenNode(e.detail.id, e.detail.kind, e.detail.newTab)} />
      </div>
    {:else if active}
      <header class="toolbar">
        <input
          class="notes-title-input title-input"
          aria-label={$t('notes.title')}
          bind:value={titleDraft}
          on:keydown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              void renameActive().then(() => editor?.focus())
            } else if (e.key === 'Escape') {
              titleDraft = active ? noteTitle(active) : ''
              editor?.focus()
            }
          }}
          on:blur={() => void renameActive()}
        />
        {#if dailyDate}
          <div class="day-nav">
            <button type="button" class="cal-icon" aria-label={$t('notes.daily.previous')} on:click={() => dailyDate && void goDay(addDays(dailyDate, -1))}>
              <Ph svg={caretLeft} size={14} />
            </button>
            <button type="button" class="cal-icon" aria-label={$t('notes.daily.next')} on:click={() => dailyDate && void goDay(addDays(dailyDate, 1))}>
              <Ph svg={caretRight} size={14} />
            </button>
          </div>
        {/if}
        <span class="spacer"></span>
        <Segmented value={mode} options={modeOptions} label={$t('notes.mode')} on:change={(e) => setMode(e.detail as PreviewMode)} />
        <button type="button" class="cal-icon" class:starred={$starredNotes.includes(active)} aria-pressed={$starredNotes.includes(active)} aria-label={$t('notes.menu.star')} on:click={() => active && toggleStar(active)}>
          <Ph svg={starIcon} size={16} />
        </button>
        <button type="button" class="cal-icon" aria-pressed={panelOpen} aria-label={$t('notes.panel.toggle')} on:click={() => (panelOpen = !panelOpen)}>
          <Ph svg={panelIcon} size={16} />
        </button>
        <button type="button" class="cal-icon" aria-label={$t('notes.more')} on:click={moreMenu}>
          <Ph svg={moreIcon} size={16} />
        </button>
      </header>
      <div class="editor-row">
        <div class="editor-host">
          {#key active}
            <NoteEditor bind:this={editor} path={active} {mode} {context} {revision} line={$noteLine?.path === active ? $noteLine.line : null} on:headings={headingsChanged} on:gone={() => active && closeNote(active)} />
          {/key}
        </div>
        {#if panelOpen}
          <div class="panel-pane" class:overlay={foldPanel} transition:fly={{ x: foldPanel ? (rtl ? -24 : 24) : 0, duration: dur(foldPanel ? motion.base : 0) }}>
            <NotePanel
              path={active}
              {headings}
              tab={panelTab}
              {revision}
              on:tab={(e) => (panelTab = e.detail)}
              on:close={() => (panelOpen = false)}
              on:open={(e) => open(e.detail.path, e.detail.newTab, e.detail.line)}
              on:line={(e) => editor?.goToLine(e.detail)}
              on:follow={(e) => void follow({ kind: e.detail.kind, target: e.detail.target, subpath: '' }, false)}
            />
          </div>
        {/if}
      </div>
    {:else}
      <div class="empty-state">
        {#if loading}
          <PieProgress label={$t('notes.loading')} />
        {:else if $vaultInfo?.error}
          <Ph svg={notesIcon} size={28} />
          <p class="empty-title">{$t('notes.vaultError')}</p>
          <p class="empty-body">{$vaultInfo.error}</p>
          <button type="button" class="cal-btn primary" on:click={() => void chooseVault()}>{$t('notes.chooseFolder')}</button>
        {:else}
          <Ph svg={notesIcon} size={28} />
          <p class="empty-title">{$vaultNotes.length ? $t('notes.empty.pick') : $t('notes.empty.first')}</p>
          <div class="empty-actions">
            <button type="button" class="cal-btn primary" on:click={() => void newNote(currentFolder())}>{$t('notes.newNote')}</button>
            <button type="button" class="cal-btn" on:click={() => void today()}>{$t('notes.today')}</button>
            <button type="button" class="cal-btn" on:click={() => (switcher = 'open')}>{$t('notes.switcher.open')}</button>
          </div>
          {#if $vaultInfo?.default}
            <p class="empty-hint">{$t('notes.defaultFolderHint')}</p>
            <button type="button" class="cal-btn" on:click={() => void chooseVault()}>{$t('notes.chooseFolder')}</button>
          {/if}
        {/if}
      </div>
    {/if}
  </div>
</section>

{#if switcher === 'open'}
  <NoteSwitcher
    notes={$vaultNotes}
    recent={$recentNotes}
    label={$t('notes.switcher.open')}
    placeholder={$t('notes.switcher.placeholder')}
    on:pick={(e) => {
      switcher = null
      open(e.detail.path, e.detail.newTab)
    }}
    on:create={(e) => {
      switcher = null
      void newNote(currentFolder(), e.detail)
    }}
    on:close={() => (switcher = null)}
  />
{:else if switcher === 'template'}
  <NoteSwitcher
    notes={$templateNotes}
    allowCreate={false}
    label={$t('notes.fromTemplate')}
    placeholder={$templateNotes.length ? $t('notes.template.placeholder') : $t('notes.template.none').replace('{folder}', $prefs.notesTemplatesFolder || '-')}
    on:pick={(e) => void pickTemplate(e.detail.path)}
    on:close={() => (switcher = null)}
  />
{/if}

{#if prompt}
  <Modal title={prompt.kind === 'folder' ? $t('notes.newFolder') : $t('notes.menu.rename')} size="small" on:close={() => (prompt = null)}>
    <form
      class="prompt"
      on:submit|preventDefault={() => void submitPrompt()}
    >
      <!-- svelte-ignore a11y-autofocus -->
      <input class="cal-field" bind:value={prompt.value} autofocus aria-label={$t('notes.name')} placeholder={$t('notes.name')} />
    </form>
    <svelte:fragment slot="footer">
      <div class="prompt-actions">
        <button type="button" class="cal-btn" on:click={() => (prompt = null)}>{$t('notes.cancel')}</button>
        <button type="button" class="cal-btn primary" on:click={() => void submitPrompt()}>{prompt.kind === 'folder' ? $t('notes.create') : $t('notes.menu.rename')}</button>
      </div>
    </svelte:fragment>
  </Modal>
{/if}

<style>
  .screen {
    position: absolute;
    inset: 0;
    display: flex;
    min-width: 0;
    background: var(--surface-raised);
    color: var(--text-primary);
    font-family: var(--font-ui);
    overflow: hidden;
  }

  .side {
    flex-shrink: 0;
    height: 100%;
    background: var(--surface-base);
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .side.drawer {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    z-index: 20;
    box-shadow: var(--shadow-overlay);
  }

  .scrim {
    position: absolute;
    inset: 0;
    z-index: 17;
    background: var(--scrim);
  }

  .list-pane {
    flex-shrink: 0;
    width: calc(var(--cal-sidebar-width) * 1.3);
    height: 100%;
    border-inline-end: var(--hairline) solid var(--border-default);
  }

  .list-pane.drawer {
    position: absolute;
    inset-block: 0;
    inset-inline-start: 0;
    z-index: 18;
    box-shadow: var(--shadow-overlay);
  }

  .main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }

  .main.hidden {
    display: none;
  }

  .tabs {
    display: flex;
    align-items: stretch;
    gap: var(--space-1);
    min-height: calc(var(--control-height) + var(--space-2));
    padding: var(--space-1) var(--space-2) 0;
    border-bottom: var(--hairline) solid var(--border-default);
    background: var(--surface-base);
    overflow-x: auto;
    scrollbar-width: none;
  }

  .tabs > .cal-icon {
    align-self: center;
  }

  .tab {
    display: flex;
    align-items: center;
    max-width: calc(var(--cal-sidebar-width) * 0.9);
    border: var(--hairline) solid transparent;
    border-bottom: none;
    border-radius: var(--radius-control) var(--radius-control) 0 0;
    color: var(--text-secondary);
  }

  .tab.on {
    background: var(--surface-raised);
    border-color: var(--border-default);
    color: var(--text-primary);
    margin-bottom: calc(var(--hairline) * -1);
  }

  .tab-name {
    min-width: 0;
    padding: 0 var(--space-2) 0 var(--space-3);
    border: none;
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: var(--fz-label);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    cursor: var(--cursor-action);
  }

  .tab-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: var(--space-5);
    height: var(--space-5);
    margin-inline-end: var(--space-1);
    border: none;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-tertiary);
    cursor: var(--cursor-action);
    opacity: 0;
  }

  .tab:hover .tab-close,
  .tab.on .tab-close,
  .tab-close:focus-visible {
    opacity: 1;
  }

  .tab-close:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-height: calc(var(--control-height) + 2 * var(--space-3));
    padding: var(--space-2) var(--space-3) var(--space-2) var(--space-4);
    border-bottom: var(--hairline) solid var(--border-subtle);
  }

  .graph-bar {
    flex-wrap: wrap;
    gap: var(--space-3);
  }

  .graph-title {
    margin: 0;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .graph-filter {
    width: calc(var(--cal-sidebar-width) * 0.8);
    height: calc(var(--control-height) - 2px);
    font-size: var(--fz-label);
  }

  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--fz-label);
    color: var(--text-secondary);
    white-space: nowrap;
  }

  .check input {
    accent-color: var(--accent);
  }

  .title-input {
    flex: 1;
    min-width: 0;
    height: var(--control-height);
    padding: 0 var(--space-2);
    border: var(--hairline) solid transparent;
    border-radius: var(--radius-control);
    background: transparent;
    color: var(--text-primary);
    font: inherit;
    font-size: var(--fz-heading);
    font-weight: var(--fw-semibold);
  }

  .title-input:hover {
    border-color: var(--border-subtle);
  }

  .title-input:focus {
    outline: none;
    border-color: var(--accent);
    background: var(--surface-raised);
  }

  .day-nav {
    display: flex;
    gap: var(--space-1);
  }

  .spacer {
    flex: 1;
  }

  .cal-icon.starred {
    color: var(--warning);
  }

  .cal-icon[aria-pressed='true']:not(.starred) {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .editor-row {
    position: relative;
    display: flex;
    flex: 1;
    min-height: 0;
  }

  .editor-host {
    flex: 1;
    min-width: 0;
    min-height: 0;
  }

  .panel-pane {
    flex-shrink: 0;
    width: var(--task-detail-width);
    height: 100%;
    border-inline-start: var(--hairline) solid var(--border-default);
  }

  .panel-pane.overlay {
    position: absolute;
    inset-block: 0;
    inset-inline-end: 0;
    z-index: 16;
    max-width: 100%;
    box-shadow: var(--shadow-overlay);
  }

  .graph-host {
    flex: 1;
    min-height: 0;
  }

  .empty-state {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-3);
    padding: var(--space-6);
    color: var(--text-tertiary);
    text-align: center;
  }

  .empty-title {
    margin: 0;
    font-size: var(--fz-heading);
    color: var(--text-secondary);
  }

  .empty-body,
  .empty-hint {
    margin: 0;
    max-width: var(--reader-width-narrow);
    font-size: var(--fz-label);
  }

  .empty-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: var(--space-2);
  }

  .empty-hint {
    margin-top: var(--space-4);
  }

  .prompt .cal-field {
    width: 100%;
  }

  .prompt-actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    width: 100%;
  }
</style>
