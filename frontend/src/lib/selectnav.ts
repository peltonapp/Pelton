// selectnav.ts is the keyboard behaviour of the custom select (#401), kept
// apart from the component so it can be reasoned about and tested on its own.
//
// Replacing a native <select> means re-implementing what the operating system
// used to do: arrow keys that skip disabled entries, Home and End, and the
// typeahead that jumps to an option by its first letters. Getting that wrong
// makes the control look right and feel broken, which is worse than the browser
// styling it replaces.

/** One chooseable entry. */
export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
  /** A color shown as a dot before the label, such as a calendar's. */
  color?: string
}

/** A labelled run of options, rendered as a heading that cannot be chosen. */
export interface SelectGroup {
  label: string
  options: SelectOption[]
}

/** What a caller passes: options, groups of options, or a mix of both. */
export type SelectItem = SelectOption | SelectGroup

/** True for a group rather than a single option. */
export function isGroup(item: SelectItem): item is SelectGroup {
  return (item as SelectGroup).options !== undefined
}

/** Every option in order, groups flattened away. Navigation works on this. */
export function flatten(items: SelectItem[]): SelectOption[] {
  const out: SelectOption[] = []
  for (const item of items) {
    if (isGroup(item)) {
      out.push(...item.options)
    } else {
      out.push(item)
    }
  }
  return out
}

/** The index of value, or -1 when nothing matches it. */
export function indexOfValue(options: SelectOption[], value: string): number {
  return options.findIndex((o) => o.value === value)
}

/**
 * The next selectable index in a direction, skipping disabled entries.
 *
 * Movement stops at the ends rather than wrapping. A native select does the
 * same, and wrapping in a long list (every folder in every account, say) moves
 * the highlight somewhere the user cannot see and did not ask for.
 *
 * from may be -1, meaning nothing is highlighted yet: moving down then lands on
 * the first option and moving up on the last.
 */
export function step(options: SelectOption[], from: number, delta: number): number {
  if (options.length === 0) {
    return -1
  }
  let i = from
  if (i < 0) {
    i = delta > 0 ? -1 : options.length
  }
  for (let next = i + delta; next >= 0 && next < options.length; next += delta) {
    if (!options[next].disabled) {
      return next
    }
  }
  // nothing selectable that way: stay where we are, unless we were nowhere.
  return from >= 0 && !options[from]?.disabled ? from : firstEnabled(options, delta > 0)
}

/** The first (or last) selectable index, or -1 when every option is disabled. */
export function firstEnabled(options: SelectOption[], forward = true): number {
  const order = forward ? options.map((_, i) => i) : options.map((_, i) => options.length - 1 - i)
  for (const i of order) {
    if (!options[i].disabled) {
      return i
    }
  }
  return -1
}

/**
 * The index a typeahead buffer points at, or -1 for no match.
 *
 * Matching is case-insensitive and starts just after the current highlight, so
 * pressing the same letter repeatedly cycles through the options beginning with
 * it, which is what a native select does. Disabled options are never matched.
 */
export function typeahead(options: SelectOption[], buffer: string, from: number): number {
  const needle = buffer.toLowerCase()
  if (needle === '') {
    return -1
  }
  // repeating one letter means "next one starting with it"; a longer buffer is
  // still being spelled out, so it re-matches from the current entry.
  const repeated = needle.length > 1 && [...needle].every((c) => c === needle[0])
  const term = repeated ? needle[0] : needle
  const start = repeated || needle.length === 1 ? from + 1 : from

  for (let n = 0; n < options.length; n++) {
    const i = (Math.max(0, start) + n) % options.length
    const option = options[i]
    if (!option.disabled && option.label.toLowerCase().startsWith(term)) {
      return i
    }
  }
  return -1
}

/** How long a run of keystrokes counts as one typeahead word. */
export const typeaheadResetMs = 800
