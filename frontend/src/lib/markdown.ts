import { marked } from 'marked'

/** Render compose Markdown with email line breaks, consistently across preview and sending. */
export function renderMarkdown(source: string): string {
  return marked.parse(source, { async: false, breaks: true })
}

// markdownSyntax matches what Markdown looks like and plain prose rarely does:
// a heading, list or quote marker opening a line, emphasis, inline code, a
// fenced block or a link.
const markdownSyntax = /^(#{1,6} |\s*[-*+] |\s*\d+\. |> |```)|\*\*[^*\n]+\*\*|__[^_\n]+__|`[^`\n]+`|\[[^\]\n]+\]\([^)\s]+\)/m

/** looksLikeMarkdown reports whether pasted plain text reads as Markdown. */
export function looksLikeMarkdown(text: string): boolean {
  return markdownSyntax.test(text)
}
