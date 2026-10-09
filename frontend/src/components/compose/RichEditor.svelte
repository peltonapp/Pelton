<script lang="ts">
  // the rich (wysiwyg) compose editor, built on tiptap/prosemirror. it is loaded
  // lazily from Compose so prosemirror stays out of the main bundle and only ships
  // when someone actually composes in rich mode. it emits html on every change so
  // the session body stays the html part the backend sends; a toolbar drives the
  // usual formatting commands and reflects the active marks at the caret.
  import { onMount, onDestroy, createEventDispatcher } from 'svelte'
  import { Editor } from '@tiptap/core'
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

  // the initial html to seed the editor with (a draft or reply body).
  export let content = ''
  export let grabFocus = false

  const dispatch = createEventDispatcher<{ change: string }>()

  let element: HTMLDivElement
  let editor: Editor | null = null
  // a counter bumped on every editor transaction so the toolbar's active-state
  // checks re-run reactively (tiptap mutates in place, so svelte needs a nudge).
  let revision = 0

  onMount(() => {
    editor = new Editor({
      autofocus: grabFocus,
      element,
      extensions: [
        StarterKit.configure({ heading: { levels: [1, 2, 3] } }),
        Link.configure({ openOnClick: false, autolink: true }),
      ],
      content,
      onUpdate: ({ editor }) => {
        dispatch('change', editor.getHTML())
      },
      onTransaction: () => {
        revision += 1
      },
    })
  })

  onDestroy(() => {
    editor?.destroy()
  })

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

  <!-- dir="auto" turns the editor round once the first strong character typed
       is right-to-left (#356), and the direction it settles on is what goes out
       with the message: see writingDirection in mailcompose.ts. -->
  <div dir="auto" class="surface selectable" bind:this={element}></div>
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
    flex: 1;
    min-height: 0;
    overflow-y: auto;
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

  /* placeholder-less empty state still needs a caret target height. */
  .surface :global(.ProseMirror:focus) {
    outline: none;
  }
</style>
