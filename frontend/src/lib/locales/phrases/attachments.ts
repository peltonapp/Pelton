// attachments.ts lists, per language, how people say a message has a file
// attached, and how they say it does not. The composer checks the text a
// user wrote against every language here, not only the interface language,
// since people often write in a language other than the one the app is set
// to (lib/sendcheck.ts).
//
// Each entry is a regular expression fragment, matched without regard to
// case. In a language whose words are split by spaces, a fragment has to
// stand as a whole word; end it with \p{L}* to let it be the start of a
// longer one, as a word stem. Languages that do not split words that way
// (Arabic, which joins its articles and prepositions to the word, and
// Chinese) match their fragments anywhere.
//
// A sentence that matches a negation is skipped whole, so "the attachment
// follows later" and "no attachment needed" do not count as a mention.
//
// Translators: add the phrases your language uses to its own block.

/** The phrases of one language. */
export interface AttachmentPhrases {
  /** Ways of saying something is attached. */
  mentions: string[]
  /** Ways of saying nothing is attached, or it comes later. */
  negations: string[]
  /** Whether a fragment has to stand as a whole word. */
  wordBounded: boolean
}

export const attachmentPhrases: Record<string, AttachmentPhrases> = {
  en: {
    mentions: ['attach(ed|ing|ment|ments)?', 'enclosed', 'encl'],
    negations: [
      'not attached',
      "(isn't|aren't|wasn't) attached",
      'no attachments?',
      'without (an |any )?attachments?',
      'attachments? (is|are) not (needed|necessary|required)',
      'no need (for|to) attach\\p{L}*',
      '(send|sending|forward) .{0,40}(later|separately|tomorrow)',
      'will follow',
      'forgot to attach',
    ],
    wordBounded: true,
  },
  de: {
    mentions: ['anbei', 'anhang', 'anhänge', 'im anhang', 'angehängt', 'anlage', 'beigefügt', 'beiliegend', 'hänge .{0,30}an'],
    negations: [
      'kein(en|e)? anh(a|ä)ng\\p{L}*',
      'ohne anh(a|ä)ng\\p{L}*',
      'nicht (angehängt|beigefügt)',
      '(schicke|sende|reiche) .{0,40}(später|nach|separat)',
      'folgt (später|separat)',
    ],
    wordBounded: true,
  },
  fr: {
    mentions: ['ci-joint\\p{L}*', 'ci joint\\p{L}*', 'pi(è|e)ces? jointes?', 'je joins', 'vous trouverez .{0,30}joint\\p{L}*'],
    negations: ['pas de pi(è|e)ce jointe', 'sans pi(è|e)ce jointe', '(pas|non) joint\\p{L}*', 'plus tard', 's(é|e)par(é|e)ment'],
    wordBounded: true,
  },
  nl: {
    mentions: ['bijgevoegd', 'bijlage(n)?', 'bijgaand', 'als bijlage', 'in de bijlage'],
    negations: ['geen bijlage(n)?', 'zonder bijlage(n)?', 'niet bijgevoegd', 'stuur .{0,40}(later|apart|na)'],
    wordBounded: true,
  },
  es: {
    mentions: ['adjunt\\p{L}*', 'anexo', 'archivo adjunto'],
    negations: ['sin adjunt\\p{L}*', 'no (he |va )?adjunt\\p{L}*', 'ning(ú|u)n adjunto', 'm(á|a)s tarde', 'por separado'],
    wordBounded: true,
  },
  it: {
    mentions: ['in allegato', 'allegat\\p{L}*', 'allego', 'alleghiamo'],
    negations: ['senza allegat\\p{L}*', 'nessun allegato', 'non allegat\\p{L}*', 'pi(ù|u) tardi', 'separatamente'],
    wordBounded: true,
  },
  pl: {
    mentions: ['w załącz\\p{L}*', 'załącznik\\p{L}*', 'załączam', 'załączon\\p{L}*', 'dołączam'],
    negations: ['bez załącznik\\p{L}*', 'nie (ma )?załącz\\p{L}*', 'później', 'osobno'],
    wordBounded: true,
  },
  pt: {
    mentions: ['em anexo', 'anex\\p{L}*', 'segue anexo'],
    negations: ['sem anexo\\p{L}*', 'n(ã|a)o (vai )?anex\\p{L}*', 'mais tarde', 'separadamente'],
    wordBounded: true,
  },
  ru: {
    mentions: ['во вложени\\p{L}*', 'вложени\\p{L}*', 'в приложени\\p{L}*', 'прикрепл\\p{L}*', 'прилагаю', 'приложенн\\p{L}*'],
    negations: ['без вложени\\p{L}*', 'нет вложени\\p{L}*', 'не прикрепл\\p{L}*', 'позже', 'отдельно'],
    wordBounded: true,
  },
  tr: {
    mentions: ['ekte', 'ekteki', 'ekli', 'ek olarak', 'ekte bulabilirsiniz', 'iliştir\\p{L}*', 'eklenti\\p{L}*'],
    negations: ['ek yok', 'eksiz', 'eklenti yok', 'sonra gönder\\p{L}*', 'ayrıca gönder\\p{L}*'],
    wordBounded: true,
  },
  ar: {
    mentions: ['مرفق', 'مرفقة', 'مرفقات', 'أرفقت', 'ارفقت', 'المرفق'],
    negations: ['بدون مرفق', 'لا يوجد مرفق', 'لا مرفقات', 'لاحقا', 'لاحقًا'],
    wordBounded: false,
  },
  'zh-CN': {
    mentions: ['附件', '附上', '随附', '附带', '见附'],
    negations: ['没有附件', '无附件', '不需要附件', '稍后', '之后再发', '另发'],
    wordBounded: false,
  },
}
