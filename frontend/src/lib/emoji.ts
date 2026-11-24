// emoji.ts is a small built-in set of emoji shortcodes for the chat
// composer: :thumbsup: and friends turn into the emoji as they are typed, and
// the composer suggests codes for a colon and a few letters. It is a short
// list of common ones, not a full picker.

/** Shortcodes and the emoji they stand for, most used first. */
export const shortcodes: Record<string, string> = {
  thumbsup: '👍',
  '+1': '👍',
  thumbsdown: '👎',
  '-1': '👎',
  heart: '❤️',
  joy: '😂',
  smile: '😄',
  slightly_smiling_face: '🙂',
  grin: '😁',
  laughing: '😆',
  sweat_smile: '😅',
  rofl: '🤣',
  wink: '😉',
  blush: '😊',
  innocent: '😇',
  heart_eyes: '😍',
  kissing_heart: '😘',
  thinking: '🤔',
  neutral_face: '😐',
  expressionless: '😑',
  roll_eyes: '🙄',
  smirk: '😏',
  relieved: '😌',
  sleeping: '😴',
  confused: '😕',
  upside_down: '🙃',
  open_mouth: '😮',
  astonished: '😲',
  flushed: '😳',
  pleading: '🥺',
  cry: '😢',
  sob: '😭',
  scream: '😱',
  angry: '😠',
  rage: '😡',
  sunglasses: '😎',
  nerd: '🤓',
  partying_face: '🥳',
  zipper_mouth: '🤐',
  shushing: '🤫',
  facepalm: '🤦',
  shrug: '🤷',
  ok_hand: '👌',
  clap: '👏',
  pray: '🙏',
  wave: '👋',
  raised_hands: '🙌',
  muscle: '💪',
  point_up: '☝️',
  point_right: '👉',
  point_left: '👈',
  v: '✌️',
  crossed_fingers: '🤞',
  handshake: '🤝',
  eyes: '👀',
  tada: '🎉',
  confetti_ball: '🎊',
  fire: '🔥',
  sparkles: '✨',
  star: '⭐',
  zap: '⚡',
  rocket: '🚀',
  '100': '💯',
  check: '✅',
  white_check_mark: '✅',
  heavy_check_mark: '✔️',
  x: '❌',
  warning: '⚠️',
  question: '❓',
  exclamation: '❗',
  bulb: '💡',
  memo: '📝',
  calendar: '📅',
  email: '📧',
  phone: '📞',
  computer: '💻',
  lock: '🔒',
  key: '🔑',
  bug: '🐛',
  wrench: '🔧',
  hourglass: '⌛',
  alarm_clock: '⏰',
  coffee: '☕',
  tea: '🍵',
  beer: '🍺',
  wine_glass: '🍷',
  pizza: '🍕',
  cake: '🍰',
  birthday: '🎂',
  gift: '🎁',
  balloon: '🎈',
  trophy: '🏆',
  medal: '🏅',
  sun: '☀️',
  cloud: '☁️',
  umbrella: '☔',
  snowflake: '❄️',
  rainbow: '🌈',
  earth: '🌍',
  seedling: '🌱',
  potato: '🥔',
  cat: '🐱',
  dog: '🐶',
  unicorn: '🦄',
  penguin: '🐧',
  ghost: '👻',
  robot: '🤖',
  skull: '💀',
  poop: '💩',
  see_no_evil: '🙈',
  hear_no_evil: '🙉',
  speak_no_evil: '🙊',
  broken_heart: '💔',
  two_hearts: '💕',
  orange_heart: '🧡',
  yellow_heart: '💛',
  green_heart: '💚',
  blue_heart: '💙',
  purple_heart: '💜',
  zzz: '💤',
  boom: '💥',
  dizzy: '💫',
  speech_balloon: '💬',
  thought_balloon: '💭',
  musical_note: '🎵',
  headphones: '🎧',
  camera: '📷',
  books: '📚',
  pushpin: '📌',
  paperclip: '📎',
  chart: '📈',
  money: '💰',
  house: '🏠',
  car: '🚗',
  airplane: '✈️',
  ship: '🚢',
  bike: '🚲',
}

/** The reactions offered one click away, in order. */
export const quickReactions = ['👍', '❤️', '😂', '🎉', '😮', '😢']

/**
 * Replaces every complete :code: the set knows with its emoji. Text inside
 * backticks is left alone, so code samples keep their colons.
 */
export function replaceShortcodes(text: string): string {
  return text
    .split(/(`[^`]*`)/)
    .map((part) =>
      part.startsWith('`') && part.endsWith('`') && part.length > 1
        ? part
        : part.replace(/:([a-z0-9_+-]+):/gi, (whole, code: string) => shortcodes[code.toLowerCase()] ?? whole),
    )
    .join('')
}

/** A shortcode being typed: where its colon is and what follows it. */
export interface ShortcodeQuery {
  start: number
  query: string
}

/**
 * Finds a shortcode being typed just before the caret: a colon at the start
 * or after a space, then at least two letters, with no closing colon yet.
 */
export function shortcodeQuery(text: string, caret: number): ShortcodeQuery | null {
  const before = text.slice(0, caret)
  const m = before.match(/(^|\s):([a-z0-9_+-]{2,})$/i)
  if (!m) {
    return null
  }
  return { start: caret - m[2].length - 1, query: m[2].toLowerCase() }
}

/** Shortcodes for a query, those starting with it first, at most limit. */
export function searchShortcodes(query: string, limit = 6): { code: string; emoji: string }[] {
  const q = query.toLowerCase()
  const seen = new Set<string>()
  const starts: { code: string; emoji: string }[] = []
  const contains: { code: string; emoji: string }[] = []
  for (const [code, emoji] of Object.entries(shortcodes)) {
    if (seen.has(emoji)) {
      continue
    }
    if (code.startsWith(q)) {
      starts.push({ code, emoji })
      seen.add(emoji)
    } else if (code.includes(q)) {
      contains.push({ code, emoji })
      seen.add(emoji)
    }
  }
  return [...starts, ...contains].slice(0, limit)
}
