import { marked } from 'marked'

/** Render compose Markdown with email line breaks, consistently across preview and sending. */
export function renderMarkdown(source: string): string {
  return marked.parse(source, { async: false, breaks: true })
}
