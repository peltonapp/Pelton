/// <reference types="svelte" />
/// <reference types="vite/client" />

// A locale's strings split by area, from the string-chunks plugin in
// vite.config.ts: virtual:strings/<locale> exports core and areas,
// virtual:strings/<locale>/<area> one area's strings as its default.
declare module 'virtual:strings/*' {
  export const core: Record<string, string>
  export const areas: Record<string, () => Promise<{ default: Record<string, string> }>>
  const strings: Record<string, string>
  export default strings
}

// The Vite-aliased Tabler icon geometry (see vite.config.ts). Each icon name
// maps to a list of svg child nodes as [tag, attributes] pairs.
declare module 'tabler-nodes-outline' {
  type IconNode = [string, Record<string, string | number>]
  const nodes: Record<string, IconNode[]>
  export default nodes
}
