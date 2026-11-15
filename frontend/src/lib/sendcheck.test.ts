import { describe, expect, it } from 'vitest'
import { hasFileAttached, mentionsAttachment, ownText, sendIssues, typedSubject, type SendCheckInput } from './sendcheck'
import { attachmentPhrases } from './locales/phrases/attachments'
import { locales } from './i18n'

describe('mentionsAttachment', () => {
  const mentions: [string, string][] = [
    ['en', "I've attached the invoice."],
    ['en', 'Please see the attachment for details'],
    ['en', 'The contract is enclosed.'],
    ['de', 'Anbei die Rechnung.'],
    ['de', 'Die Unterlagen findest du im Anhang.'],
    ['de', 'Ich habe die Datei angehängt'],
    ['fr', 'Veuillez trouver ci-joint le devis.'],
    ['fr', 'Voir la pièce jointe.'],
    ['nl', 'De offerte is bijgevoegd.'],
    ['nl', 'Zie de bijlage.'],
    ['es', 'Te adjunto el informe.'],
    ['es', 'Va el archivo adjunto'],
    ['it', 'In allegato trovi il preventivo.'],
    ['pl', 'Fakturę przesyłam w załączniku.'],
    ['pt', 'Segue em anexo o relatório.'],
    ['ru', 'Отчёт во вложении.'],
    ['tr', 'Fatura ekte.'],
    ['ar', 'الملف مرفق مع الرسالة'],
    ['zh-CN', '请查看附件。'],
  ]
  for (const [lang, text] of mentions) {
    it(`finds "${text}" (${lang})`, () => {
      expect(mentionsAttachment(text)).toBe(true)
    })
  }

  const quiet: [string, string][] = [
    ['en', 'Thanks for the meeting today.'],
    ['en', 'The file is not attached, I will send it later.'],
    ["en", "I'll send the attachment separately."],
    ['en', 'No attachment needed for this one.'],
    ['en', 'The attachment will follow.'],
    ['en', 'Attachments are not needed.'],
    ['en', 'He is very attachedness-free'],
    ['de', 'Ohne Anhang, nur zur Info.'],
    ['de', 'Den Anhang schicke ich später.'],
    ['de', 'Die Anlagenbauer kommen morgen'],
    ['fr', 'Pas de pièce jointe cette fois.'],
    ['es', 'Lo adjunto más tarde.'],
    ['it', 'Nessun allegato, solo una domanda.'],
    ['ru', 'Без вложений, просто вопрос.'],
    ['tr', 'Şekli değişti.'],
    ['zh-CN', '没有附件。'],
  ]
  for (const [lang, text] of quiet) {
    it(`leaves "${text}" (${lang})`, () => {
      expect(mentionsAttachment(text)).toBe(false)
    })
  }

  it('reads each sentence on its own', () => {
    expect(mentionsAttachment('I will call you later. The plan is attached.')).toBe(true)
    expect(mentionsAttachment('Attached is nothing yet, I will send it later')).toBe(false)
  })

  it('checks every language whatever the interface is set to', () => {
    expect(mentionsAttachment('Hi Ana, te adjunto el plan. Regards')).toBe(true)
  })
})

describe('the phrase table', () => {
  it('covers every language the app ships', () => {
    for (const locale of locales) {
      expect(attachmentPhrases[locale]?.mentions.length, locale).toBeGreaterThan(0)
    }
  })

  it('holds expressions that compile', () => {
    for (const [lang, p] of Object.entries(attachmentPhrases)) {
      for (const fragment of [...p.mentions, ...p.negations]) {
        expect(() => new RegExp(fragment, 'iu'), `${lang}: ${fragment}`).not.toThrow()
      }
    }
  })
})

describe('ownText', () => {
  it('drops quoted lines and what follows the signature delimiter', () => {
    const body = 'Hi Bob\n\n> see attached\n\nCheers\n-- \nBob, attached to his desk'
    expect(ownText(body, 'plaintext')).toBe('Hi Bob\n\n\nCheers')
  })

  it('stops at the original of a reply or forward', () => {
    expect(ownText('Sure.\n\nOn Monday, Ada wrote:\nthe file is attached', 'plaintext')).toBe('Sure.\n')
    expect(ownText('FYI\n---------- Forwarded message ----------\nAttached: plan.pdf', 'plaintext')).toBe('FYI')
  })

  it('drops blockquotes and the signature from rich text', () => {
    const html = '<p>Thanks!</p><blockquote><p>I attached the plan</p></blockquote><p>-- </p><p>Sent with an attachment</p>'
    const text = ownText(html, 'wysiwyg')
    expect(text).toContain('Thanks!')
    expect(text).not.toContain('attach')
  })
})

describe('typedSubject', () => {
  it('keeps a subject of a new message', () => {
    expect(typedSubject('Invoice attached')).toBe('Invoice attached')
  })

  it('ignores the original subject under a reply or forward prefix', () => {
    expect(typedSubject('Re: Invoice attached', 'Invoice attached')).toBe('')
    expect(typedSubject('AW: Fwd: invoice attached', 'Invoice attached')).toBe('')
  })

  it('keeps a subject the user changed', () => {
    expect(typedSubject('Re: plan attached now', 'Plan')).toBe('Re: plan attached now')
  })
})

describe('hasFileAttached', () => {
  it('does not count inline images', () => {
    expect(hasFileAttached([{ inline: true }])).toBe(false)
    expect(hasFileAttached([{ inline: true }, { inline: false }])).toBe(true)
    expect(hasFileAttached([])).toBe(false)
  })
})

describe('sendIssues', () => {
  const base: SendCheckInput = { recipients: 1, subject: 'Plan', body: 'Hello', mode: 'plaintext', attachments: [] }
  const all = { missingAttachment: true, emptySubject: true }

  it('finds nothing in an ordinary message', () => {
    expect(sendIssues(base, all)).toEqual([])
  })

  it('lists every issue, most pressing first', () => {
    expect(sendIssues({ ...base, recipients: 0, subject: '', body: 'See attached.' }, all)).toEqual([
      'noRecipients',
      'missingAttachment',
      'emptySubject',
    ])
  })

  it('finds a mention in the subject', () => {
    expect(sendIssues({ ...base, subject: 'Invoice attached' }, all)).toEqual(['missingAttachment'])
  })

  it('does not warn about a subject the reply inherited', () => {
    expect(sendIssues({ ...base, subject: 'Re: Invoice attached', originalSubject: 'Invoice attached' }, all)).toEqual([])
  })

  it('is satisfied by a file, not by an inline image', () => {
    const body = 'The plan is attached.'
    expect(sendIssues({ ...base, body, attachments: [{ inline: false }] }, all)).toEqual([])
    expect(sendIssues({ ...base, body, attachments: [{ inline: true }] }, all)).toEqual(['missingAttachment'])
  })

  it('leaves out the checks that are off, never the missing recipients', () => {
    const input = { ...base, recipients: 0, subject: '', body: 'attached' }
    expect(sendIssues(input, { missingAttachment: false, emptySubject: false })).toEqual(['noRecipients'])
  })
})
