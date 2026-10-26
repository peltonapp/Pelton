// signatureblock.ts places a header or footer signature into a compose body.

import type { EditorMode, Signature } from './types'
import { renderMarkdown } from './markdown'

// withSignatureBlock inserts a signature block into a body: headers go to the
// top, footers to the bottom after the standard "-- " delimiter. Given the
// quoted original of a reply, a footer goes above it instead (#512), as long
// as the quote is still in the body to find. The rich editor reads html, so
// there the block goes in as html.
export function withSignatureBlock(body: string, sig: Signature, mode: EditorMode, quote = ''): string {
  const quoteAt = sig.kind === 'footer' && quote ? body.lastIndexOf(quote) : -1
  const before = quoteAt >= 0 ? body.slice(0, quoteAt) : body
  const after = quoteAt >= 0 ? body.slice(quoteAt) : ''
  if (mode === 'wysiwyg') {
    const block = sig.format === 'html' ? sig.content : renderMarkdown(sig.content)
    return sig.kind === 'header' ? `${block}${body}` : `${before}<p>-- </p>${block}${after}`
  }
  if (sig.kind === 'header') {
    return `${sig.content}\n\n${body}`
  }
  const footer = `${before.replace(/\s*$/, '')}\n\n-- \n${sig.content}`
  return after ? `${footer}\n\n${after}` : footer
}
