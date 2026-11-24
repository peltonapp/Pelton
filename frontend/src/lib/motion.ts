// motion.ts gives Svelte transitions their durations. css animations take
// theirs from the motion tokens; a transition written in a component needs a
// number instead, and should stand still in the classic design and when the
// reader asked for reduced motion.

/** Durations in ms, matching --motion-fast, --motion-base and --motion-slow. */
export const motion = { fast: 110, base: 170, slow: 260 } as const

/**
 * Returns the duration of a transition both designs share: zero in the new
 * design when reduced motion is asked for, the given one otherwise.
 */
export function calm(ms: number): number {
  const root = document.documentElement
  return root.dataset.design === 'classic' ? ms : dur(ms)
}

/**
 * Returns the duration to animate with now: the given one in the new design,
 * zero in the classic design or with reduced motion on.
 */
export function dur(ms: number): number {
  const root = document.documentElement
  if (root.dataset.design === 'classic' || root.hasAttribute('data-reduce-motion')) {
    return 0
  }
  if (typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
    return 0
  }
  return ms
}
