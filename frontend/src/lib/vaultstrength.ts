// vaultstrength.ts estimates how hard a passphrase is to guess, offline: a
// rough count of bits from its length and the kinds of characters it uses,
// less for runs and repeats. Nothing leaves the device. It is a hint, not a
// guarantee; a long phrase of ordinary words scores well and is.

/** How strong a passphrase looks. */
export type Strength = 'weak' | 'fair' | 'good' | 'strong'

/** Bits of guessing a passphrase takes, roughly. */
export function passphraseBits(p: string): number {
  const chars = [...p]
  if (chars.length === 0) {
    return 0
  }
  let pool = 0
  if (/[a-z]/.test(p)) pool += 26
  if (/[A-Z]/.test(p)) pool += 26
  if (/[0-9]/.test(p)) pool += 10
  if (/[^a-zA-Z0-9]/.test(p)) pool += 33
  let effective = 0
  for (let i = 0; i < chars.length; i++) {
    const repeat = i > 0 && chars[i] === chars[i - 1]
    const run = i > 0 && chars[i].codePointAt(0)! - chars[i - 1].codePointAt(0)! === 1
    effective += repeat || run ? 0.25 : 1
  }
  return Math.round(effective * Math.log2(Math.max(pool, 2)))
}

/** The strength band of a passphrase. */
export function passphraseStrength(p: string): Strength {
  const bits = passphraseBits(p)
  if (bits < 40) return 'weak'
  if (bits < 60) return 'fair'
  if (bits < 80) return 'good'
  return 'strong'
}
