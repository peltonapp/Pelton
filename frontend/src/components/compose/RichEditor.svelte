<script lang="ts">
  // the rich (wysiwyg) compose editor, built on tiptap/prosemirror. it is loaded
  // lazily from Compose so prosemirror stays out of the main bundle and only ships
  // when someone actually composes in rich mode. it emits html on every change so
  // the session body stays the html part the backend sends; a toolbar drives the
  // usual formatting commands and reflects the active marks at the caret.
  import { onMount, onDestroy, createEventDispatcher } from 'svelte'
  import { Editor, createDocument, getHTMLFromFragment } from '@tiptap/core'
  import StarterKit from '@tiptap/starter-kit'
  import Link from '@tiptap/extension-link'
  import {
    IconBold,
    IconItalic,
    IconCode,
    IconLink,
    IconList,
    IconListNumbers,
    IconQuote,
    IconHeading,
  } from '@tabler/icons-svelte'
  import { t } from '../../lib/i18n'
  import { caretMark } from '../../lib/richtext'
  import { smoothCaret } from '../../lib/smoothcaret'
  import { prefs } from '../../stores/prefs'
  import { looksLikeMarkdown, renderMarkdown } from '../../lib/markdown'

  // the initial html to seed the editor with (a draft or reply body).
  export let content = ''
  // caretContent, when set, makes the editor take focus on mount. it is the
  // content with caretMark standing where the caret belongs; without the mark
  // the caret starts at the top.
  export let caretContent: string | null = null
  // bare leaves out the built-in toolbar, for a composer that draws its own
  // and drives the editor through run() and isActive().
  export let bare = false
  // inputRules turns Markdown typed into the editor into formatting as it is
  // written. Off, typing ** or # does nothing special. Read on mount.
  export let inputRules = true
  // pasteMarkdown turns pasted plain text that reads as Markdown into
  // formatting. Read on mount.
  export let pasteMarkdown = false
  // placeholder is shown while the editor is empty.
  export let placeholder = ''

  const dispatch = createEventDispatcher<{ change: string; transaction: number }>()

  let element: HTMLDivElement
  let editor: Editor | null = null
  // a counter bumped on every editor transaction so the toolbar's active-state
  // checks re-run reactively (tiptap mutates in place, so svelte needs a nudge).
  let revision = 0
  // the content prop as last seen, so an outside change to it (a signature
  // picked from the footer menu, a reopened draft) is told apart from the
  // echo of the editor's own typing, which comes back equal to its html.
  let lastContent = content

  // empty drives the placeholder.
  let empty = false

  onMount(() => {
    editor = new Editor({
      element,
      extensions: [
        StarterKit.configure({ heading: { levels: [1, 2, 3] } }),
        Link.configure({ openOnClick: false, autolink: true }),
      ],
      content,
      enableInputRules: inputRules,
      enablePasteRules: inputRules,
      editorProps: {
        handlePaste: (_view, event) => pasteAsMarkdown(event),
      },
      onUpdate: ({ editor }) => {
        dispatch('change', editor.getHTML())
      },
      onTransaction: ({ editor }) => {
        revision += 1
        empty = editor.isEmpty
        dispatch('transaction', revision)
      },
    })
    empty = editor.isEmpty
    if (caretContent !== null) {
      editor.commands.focus(markPosition(caretContent, editor) ?? 'start')
    }
  })

  // the gliding caret of the new design, on while its setting is.
  let dropCaret: (() => void) | null = null
  $: glide = $prefs.smoothCaret && $prefs.design !== 'classic'
  $: if (editor) {
    dropCaret?.()
    dropCaret = glide ? smoothCaret(editor, element) : null
  }

  $: if (editor && content !== lastContent) {
    lastContent = content
    if (content !== editor.getHTML()) {
      editor.commands.setContent(content, { emitUpdate: false })
    }
  }

  // markPosition finds caretMark in html parsed the way the editor parses its
  // content, so the position holds in the editor's own document.
  function markPosition(html: string, editor: Editor): number | null {
    let found: number | null = null
    createDocument(html, editor.schema).descendants((node, pos) => {
      const at = node.isText ? (node.text ?? '').indexOf(caretMark) : -1
      if (found === null && at >= 0) {
        found = pos + at
      }
      return found === null
    })
    return found
  }

  // contentWithCaret is the current html with caretMark where the caret is.
  export function contentWithCaret(): string {
    if (!editor) {
      return content
    }
    const { state } = editor
    const doc = state.tr.insertText(caretMark, state.selection.head).doc
    return getHTMLFromFragment(doc.content, editor.schema)
  }

  onDestroy(() => {
    dropCaret?.()
    editor?.destroy()
  })

  // pasteAsMarkdown inserts pasted plain text that reads as Markdown as the
  // formatting it describes. Anything carrying html of its own pastes as usual.
  function pasteAsMarkdown(event: ClipboardEvent): boolean {
    const data = event.clipboardData
    if (!pasteMarkdown || !editor || !data || data.types.includes('text/html')) {
      return false
    }
    const text = data.getData('text/plain')
    if (!text || !looksLikeMarkdown(text)) {
      return false
    }
    editor.chain().focus().insertContent(renderMarkdown(text)).run()
    return true
  }

  /** A formatting command the composer's own toolbar can run. */
  type Command = 'bold' | 'italic' | 'underline' | 'strike' | 'bulletList' | 'orderedList' | 'blockquote' | 'code' | 'clear'

  /** run applies a formatting command to the selection. */
  export function run(command: Command): void {
    if (!editor) {
      return
    }
    const chain = editor.chain().focus()
    switch (command) {
      case 'bold':
        chain.toggleBold().run()
        break
      case 'italic':
        chain.toggleItalic().run()
        break
      case 'underline':
        chain.toggleUnderline().run()
        break
      case 'strike':
        chain.toggleStrike().run()
        break
      case 'bulletList':
        chain.toggleBulletList().run()
        break
      case 'orderedList':
        chain.toggleOrderedList().run()
        break
      case 'blockquote':
        chain.toggleBlockquote().run()
        break
      case 'code':
        chain.toggleCode().run()
        break
      case 'clear':
        chain.unsetAllMarks().clearNodes().run()
        break
    }
  }

  /** isActive reports whether a mark or node is on at the caret. */
  export function isActive(name: string): boolean {
    return editor ? editor.isActive(name) : false
  }

  /** linkHref is the address of the link at the caret, or ''. */
  export function linkHref(): string {
    return editor ? ((editor.getAttributes('link').href as string | undefined) ?? '') : ''
  }

  /** setLinkHref links the selection to href, or removes the link when href is ''. */
  export function setLinkHref(href: string): void {
    if (!editor) {
      return
    }
    if (href === '') {
      editor.chain().focus().extendMarkRange('link').unsetLink().run()
      return
    }
    editor.chain().focus().extendMarkRange('link').setLink({ href }).run()
  }

  /** focus puts the caret in the editor. */
  export function focus(): void {
    editor?.commands.focus()
  }

  // active() reports whether a mark/node is on at the caret. it reads `revision`
  // so the buttons recompute as the selection moves.
  function active(name: string, attrs?: Record<string, unknown>): boolean {
    void revision
    return editor ? editor.isActive(name, attrs) : false
  }

  function focusChain() {
    return editor!.chain().focus()
  }

  function toggleBold(): void {
    focusChain().toggleBold().run()
  }
  function toggleItalic(): void {
    focusChain().toggleItalic().run()
  }
  function toggleCode(): void {
    focusChain().toggleCode().run()
  }
  function toggleHeading(): void {
    focusChain().toggleHeading({ level: 2 }).run()
  }
  function toggleBullet(): void {
    focusChain().toggleBulletList().run()
  }
  function toggleOrdered(): void {
    focusChain().toggleOrderedList().run()
  }
  function toggleQuote(): void {
    focusChain().toggleBlockquote().run()
  }

  // setLink toggles a link on the selection. an empty prompt removes the link.
  function setLink(): void {
    if (!editor) {
      return
    }
    const prev = (editor.getAttributes('link').href as string) ?? ''
    const url = window.prompt($t('compose.toolbar.linkUrlPrompt'), prev)
    if (url === null) {
      return
    }
    if (url === '') {
      focusChain().unsetLink().run()
      return
    }
    focusChain().extendMarkRange('link').setLink({ href: url }).run()
  }
</script>

<div class="rich">
  {#if !bare}
  <div class="toolbar" role="toolbar" aria-label={$t('compose.toolbar.ariaLabel')}>
    <button type="button" class:on={active('bold')} title={$t('compose.toolbar.bold')} aria-label={$t('compose.toolbar.bold')} on:click={toggleBold}>
      <IconBold size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('italic')} title={$t('compose.toolbar.italic')} aria-label={$t('compose.toolbar.italic')} on:click={toggleItalic}>
      <IconItalic size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('code')} title={$t('compose.toolbar.inlineCode')} aria-label={$t('compose.toolbar.inlineCode')} on:click={toggleCode}>
      <IconCode size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('heading', { level: 2 })} title={$t('compose.toolbar.heading')} aria-label={$t('compose.toolbar.heading')} on:click={toggleHeading}>
      <IconHeading size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('link')} title={$t('compose.toolbar.link')} aria-label={$t('compose.toolbar.link')} on:click={setLink}>
      <IconLink size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('bulletList')} title={$t('compose.toolbar.bulletList')} aria-label={$t('compose.toolbar.bulletList')} on:click={toggleBullet}>
      <IconList size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('orderedList')} title={$t('compose.toolbar.numberedList')} aria-label={$t('compose.toolbar.numberedList')} on:click={toggleOrdered}>
      <IconListNumbers size={16} stroke={1.8} />
    </button>
    <button type="button" class:on={active('blockquote')} title={$t('compose.toolbar.quote')} aria-label={$t('compose.toolbar.quote')} on:click={toggleQuote}>
      <IconQuote size={16} stroke={1.8} />
    </button>
  </div>
  {/if}

  <!-- dir="auto" turns the editor round once the first strong character typed
       is right-to-left (#356), and the direction it settles on is what goes out
       with the message: see writingDirection in mailcompose.ts. -->
  <div
    dir="auto"
    class="surface selectable"
    class:empty={empty && placeholder !== ''}
    style:--placeholder={placeholder ? JSON.stringify(placeholder) : null}
    bind:this={element}
  ></div>
</div>

<style>
  .rich {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    padding: var(--space-1) var(--space-2);
    border-bottom: var(--hairline) solid var(--border-subtle);
    flex-wrap: wrap;
  }

  button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    color: var(--text-secondary);
    cursor: var(--cursor-action);
    padding: var(--space-2);
    border-radius: var(--radius-control);
  }

  button:hover {
    background: var(--surface-hover);
    color: var(--text-primary);
  }

  button.on {
    background: var(--selection-bg);
    color: var(--text-primary);
  }

  .surface {
    position: relative;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  /* the gliding caret (lib/smoothcaret.ts) stands in for the native one. */
  .surface:global(.smooth-caret-on) :global(.ProseMirror) {
    caret-color: transparent;
  }

  .surface :global(.smooth-caret) {
    position: absolute;
    top: 0;
    left: 0;
    width: 2px;
    margin-left: -1px;
    border-radius: 1px;
    background: var(--accent);
    opacity: 0;
    pointer-events: none;
    transition:
      transform var(--motion-fast) var(--ease-out),
      height var(--motion-fast) var(--ease-out);
  }

  .surface :global(.smooth-caret.shown) {
    opacity: 1;
  }

  .surface :global(.smooth-caret.jump) {
    transition: none;
  }

  .surface :global(.smooth-caret.blink) {
    animation: caret-blink 1.1s steps(1) infinite;
  }

  @keyframes -global-caret-blink {
    50% {
      opacity: 0;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .surface :global(.smooth-caret) {
      transition: none;
    }
  }

  /* the prosemirror editable area. tokens only; the editor injects .ProseMirror.
     --compose-font is the mail body font setting, set by Compose on the editor
     container (#64); unset, the ui font is inherited as before. */
  .surface :global(.ProseMirror) {
    min-height: 100%;
    outline: none;
    padding: var(--space-3) var(--space-4);
    font-family: var(--compose-font, inherit);
    font-size: var(--fz-body);
    line-height: 1.55;
    color: var(--text-primary);
  }

  .surface :global(.ProseMirror p) {
    margin: 0 0 var(--space-2);
  }

  .surface :global(.ProseMirror h1),
  .surface :global(.ProseMirror h2),
  .surface :global(.ProseMirror h3) {
    margin: var(--space-3) 0 var(--space-2);
    line-height: 1.3;
  }

  .surface :global(.ProseMirror a) {
    color: var(--link);
  }

  .surface :global(.ProseMirror code) {
    font-family: var(--font-mono);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
    padding: 0 4px;
  }

  .surface :global(.ProseMirror pre) {
    font-family: var(--font-mono);
    background: var(--surface-sunken);
    border-radius: var(--radius-control);
    padding: var(--space-3);
    overflow-x: auto;
  }

  .surface :global(.ProseMirror blockquote) {
    margin: 0 0 var(--space-2);
    padding-inline-start: var(--space-3);
    border-inline-start: 2px solid var(--border-strong);
    color: var(--text-secondary);
  }

  .surface.empty :global(.ProseMirror > p:first-child) {
    position: relative;
  }

  .surface.empty :global(.ProseMirror > p:first-child::before) {
    content: var(--placeholder);
    position: absolute;
    color: var(--text-tertiary);
    pointer-events: none;
  }

  /* placeholder-less empty state still needs a caret target height. */
  .surface :global(.ProseMirror:focus) {
    outline: none;
  }
</style>
