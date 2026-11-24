import { describe, expect, it } from 'vitest'
import { contrastRatio } from './accent'
import { BASE_TOKENS, SURFACE_TOKENS, TEXT_TOKENS, legibleTokens, legibleTone } from './legible'

interface Manifest {
  id: string
  base: 'light' | 'dark'
  tokens: Record<string, string>
}

const packs = Object.values(
  import.meta.glob<Manifest>('../../../internal/themepack/presets/*/manifest.json', { eager: true, import: 'default' }),
)

describe('text contrast in the new design', () => {
  it('finds the bundled theme packs', () => {
    expect(packs.length).toBeGreaterThanOrEqual(6)
  })

  for (const base of ['light', 'dark'] as const) {
    it(`keeps every text token of the built-in ${base} theme at 4.5:1 on every surface`, () => {
      const t = BASE_TOKENS[base]
      for (const fg of ['text-primary', ...TEXT_TOKENS]) {
        for (const s of SURFACE_TOKENS) {
          expect(contrastRatio(t[fg], t[s]), `${fg} on ${s}`).toBeGreaterThanOrEqual(4.5)
        }
      }
    })
  }

  for (const pack of packs) {
    it(`lifts the ${pack.id} pack to 4.5:1 on every surface`, () => {
      const lifted = legibleTokens(pack.base, pack.tokens)
      const surface = (name: string): string => pack.tokens[name] ?? BASE_TOKENS[pack.base][name]
      for (const fg of TEXT_TOKENS) {
        for (const s of SURFACE_TOKENS) {
          expect(contrastRatio(lifted[fg], surface(s)), `${fg} on ${s}`).toBeGreaterThanOrEqual(4.5)
        }
      }
    })
  }

  it('leaves a color that already reads unchanged', () => {
    expect(legibleTone('#1a1b1e', '#000000', ['#ffffff'])).toBe('#1a1b1e')
  })
})
