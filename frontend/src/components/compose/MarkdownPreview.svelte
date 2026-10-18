<script lang="ts">
  // the markdown preview of the compose pane. a reply quotes the sender's text,
  // and marked passes raw html through, so the rendered result is untrusted: it
  // goes into a frame with an empty sandbox (no scripts, no same-origin) and a
  // CSP that forbids script, never into the app's own document.
  import { renderMarkdown } from '../../lib/markdown'
  import { onMount } from 'svelte'
  import { t } from '../../lib/i18n'

  // markdown is the compose body to render.
  export let markdown: string
  // font is the css font-family the editor uses, empty for the ui font.
  export let font: string

  // The iframe has its own document and color scheme. Snapshot the active
  // tokens and refresh them when preferences or an injected custom theme change.
  function readTheme(): Record<string, string> {
    const root = document.documentElement
    const computed = getComputedStyle(root)
    const names = ['--font-ui', '--font-mono', '--space-2', '--space-3', '--space-4',
      '--surface-overlay', '--surface-sunken', '--text-primary', '--text-secondary',
      '--fz-body', '--list-line-height', '--link', '--radius-control', '--hairline', '--border-strong']
    return Object.fromEntries([
      ...names.map((name) => [name, computed.getPropertyValue(name).trim()]),
      ['color-scheme', root.dataset.theme === 'dark' ? 'dark' : 'light'],
    ])
  }

  let theme = readTheme()
  onMount(() => {
    const observer = new MutationObserver(() => { theme = readTheme() })
    observer.observe(document.documentElement, { attributes: true })
    observer.observe(document.head, { childList: true, subtree: true, characterData: true })
    return () => observer.disconnect()
  })

  // withoutTargets strips target and formtarget from the rendered html: a
  // quoted link or form naming its own target outranks the frame's base
  // target and could navigate the preview to a remote page. A template parses
  // without loading images or running handlers, so the untrusted markup never
  // comes alive in the app's document.
  function withoutTargets(html: string): string {
    const template = document.createElement('template')
    template.innerHTML = html
    for (const el of template.content.querySelectorAll('[target], [formtarget]')) {
      el.removeAttribute('target')
      el.removeAttribute('formtarget')
    }
    return template.innerHTML
  }

  function buildSrcdoc(source: string, fontFamily: string, theme: Record<string, string>): string {
    const readVar = (name: string): string => theme[name]
    const html = withoutTargets(renderMarkdown(source))
    const family = fontFamily || readVar('--font-ui')
    const space2 = readVar('--space-2')
    const space3 = readVar('--space-3')
    // the style tag name is concatenated so svelte's parser does not take the
    // literal for a component style block.
    const open = '<sty' + 'le>'
    const close = '</sty' + 'le>'
    const css = `
  html{color-scheme:${theme['color-scheme']};}
  html,body{margin:0;background:${readVar('--surface-overlay')};color:${readVar('--text-primary')};font-family:${family};font-size:${readVar('--fz-body')};line-height:${readVar('--list-line-height')};}
  body{padding:${space3} ${readVar('--space-4')};overflow-wrap:break-word;}
  h1,h2,h3{margin:${space3} 0 ${space2};}
  a{color:${readVar('--link')};}
  pre,code{font-family:${readVar('--font-mono')};background:${readVar('--surface-sunken')};border-radius:${readVar('--radius-control')};}
  pre{padding:${space3};overflow-x:auto;}
  blockquote{margin:0 0 ${space2};padding-inline-start:${space3};border-inline-start:${readVar('--hairline')} solid ${readVar('--border-strong')};color:${readVar('--text-secondary')};}
  img{max-width:100%;height:auto;}`
    const csp = `default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; script-src 'none'`
    // the sandbox has no allow-popups, so target=_blank makes a click on a
    // quoted link do nothing instead of loading a remote page inside the
    // compose pane.
    return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${csp}"><base target="_blank">${open}${css}${close}</head><body>${html}</body></html>`
  }

  $: srcdoc = buildSrcdoc(markdown, font, theme)
</script>

<iframe class="preview" title={$t('compose.toolbar.preview')} sandbox="" {srcdoc}></iframe>

<style>
  .preview {
    flex: 1;
    width: 100%;
    min-height: 0;
    border: 0;
    background: var(--surface-overlay);
  }
</style>
