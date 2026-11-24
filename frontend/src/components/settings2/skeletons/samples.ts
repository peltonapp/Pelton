/** One made-up message the settings pictures draw. */
export interface SampleMessage {
  name: string
  email: string
  subject: string
  snippet: string
  minutes: number
  unread: boolean
  flagged: boolean
}

/** Three made-up messages shared by every picture that needs sample mail. */
export const sampleMessages: SampleMessage[] = [
  {
    name: 'Ada Lovelace',
    email: 'ada@analytical.example',
    subject: 'Notes on the Analytical Engine',
    snippet:
      'I have been thinking about how the machine might weave algebraic patterns, just as the loom weaves flowers and leaves. The notes run longer than the translation they accompany, and I would value your reading of the final section before they go to print.',
    minutes: 4,
    unread: true,
    flagged: true,
  },
  {
    name: 'Grace Hopper',
    email: 'grace@compiler.example',
    subject: 'Compiler release schedule',
    snippet:
      'The first release candidate is built and the regression suite is green on every target. Remaining work is the documentation pass and the migration notes for anyone still on the old calling convention, which I would like to finish this week.',
    minutes: 95,
    unread: false,
    flagged: false,
  },
  {
    name: 'Alan Turing',
    email: 'alan@bletchley.example',
    subject: 'Lunch on Friday?',
    snippet:
      'Are we still on for that place near the park? They have the good fries, and the table by the window is usually free until one. Let me know and I will book it.',
    minutes: 60 * 26,
    unread: false,
    flagged: true,
  },
]
