// ai.ts holds the AI provider types and the rules for which permission a
// script needs to send data to a provider: "ai:<id>" for one that may leave
// this device, "ai_local:<id>" for one on this device or the local network.
// The backend makes the same call from the resolved address when a script
// runs; a provider shown as local here is one whose address says so outright.

/** A kind of provider that can be added. */
export interface AIKind {
  id: string
  /** English name, used when the catalog has no `ai.kind.<id>` string. */
  label: string
  /** Prefilled address, '' when the user enters one. */
  baseUrl: string
  /** Whether it takes an API key. */
  key: 'required' | 'optional' | 'none'
  /** Prefilled model, '' when the user picks one from the fetched list. */
  model: string
}

/** A provider the user set up. */
export interface AIProvider {
  id: string
  name: string
  kind: string
  baseUrl: string
  model: string
  models: string[]
  default: boolean
  hasKey: boolean
  /** The address names this device or a private network. */
  local: boolean
}

/** A provider being added (id '') or changed. */
export interface AIProviderInput {
  id: string
  name: string
  kind: string
  baseUrl: string
  model: string
  models: string[]
  /** A new key, '' to keep the stored one. */
  key: string
  clearKey: boolean
}

/** The outcome of a connection test. */
export interface AITestResult {
  ok: boolean
  message: string
  local: boolean
}

/** The AI switches of one script. */
export interface AIScriptOptions {
  logText: boolean
}

/** Base permission for a provider that may leave this device. */
export const permCloud = 'ai'
/** Base permission for a provider on this device or the local network. */
export const permLocal = 'ai_local'

/** The permission a script declares to use a provider. */
export function aiPermission(p: Pick<AIProvider, 'id' | 'local'>): string {
  return `${p.local ? permLocal : permCloud}:${p.id}`
}

/** The provider id an AI permission names, or '' for any other permission. */
export function aiTarget(perm: string): string {
  const [base, target] = perm.split(':', 2)
  return (base === permCloud || base === permLocal) && target ? target : ''
}

/** Whether a permission list mentions AI at all, targeted or not. */
export function usesAI(perms: string[]): boolean {
  return perms.some((p) => p === permCloud || p === permLocal || aiTarget(p) !== '')
}

/**
 * The permissions with access to a provider switched on or off. Either form
 * for that provider is replaced, so a provider whose address changed between
 * local and not is declared the way it is now.
 */
export function setAIAccess(perms: string[], p: Pick<AIProvider, 'id' | 'local'>, on: boolean): string[] {
  const rest = perms.filter((x) => aiTarget(x) !== p.id)
  return on ? [...rest, aiPermission(p)] : rest
}

/** A blank input for a new provider of a kind. */
export function newProviderInput(kind: AIKind): AIProviderInput {
  return { id: '', name: kind.label, kind: kind.id, baseUrl: kind.baseUrl, model: kind.model, models: [], key: '', clearKey: false }
}

/** The input that edits a saved provider, keeping its key. */
export function providerInput(p: AIProvider): AIProviderInput {
  return { id: p.id, name: p.name, kind: p.kind, baseUrl: p.baseUrl, model: p.model, models: [...p.models], key: '', clearKey: false }
}

/** Whether an address names this device or a private network outright. */
export function isLocalAddress(url: string): boolean {
  let host: string
  try {
    const u = new URL(url.trim())
    if (u.protocol !== 'http:' && u.protocol !== 'https:') {
      return false
    }
    host = u.hostname.toLowerCase().replace(/^\[|\]$/g, '')
  } catch {
    return false
  }
  if (host === 'localhost' || host.endsWith('.localhost') || host === '::1') {
    return true
  }
  const v4 = host.match(/^(\d+)\.(\d+)\.(\d+)\.(\d+)$/)
  if (v4) {
    const [a, b] = [Number(v4[1]), Number(v4[2])]
    return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254)
  }
  return /^f[cd][0-9a-f]{2}:/.test(host) || /^fe[89ab][0-9a-f]:/.test(host)
}
