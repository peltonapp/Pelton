// noteprojects.ts tells which project a note of the notes folder belongs to.
// The note's front matter says so: project-id holds the project's uid and
// decides; project holds its name, which alone is enough for a note typed by
// hand. The Notes space and the Projects space both read it from here, so
// neither store has to import the other.

import type { Project, VaultProp } from './types'

const projectKey = 'project'
const projectIdKey = 'project-id'

function prop(props: readonly VaultProp[], key: string): string {
  const p = props.find((x) => x.key.trim().toLowerCase() === key)
  if (!p) {
    return ''
  }
  return (p.list.length ? p.list[0] : p.value).trim()
}

/** The project a note's front matter names: its uid and name, either '' when missing. */
export function noteProjectRef(props: readonly VaultProp[] | undefined): { uid: string; name: string } {
  if (!props?.length) {
    return { uid: '', name: '' }
  }
  const name = prop(props, projectKey).replace(/^\[\[|\]\]$/g, '').trim()
  return { uid: prop(props, projectIdKey), name }
}

/** True when a note's front matter puts it in the project: by uid, or by name when it has no uid. */
export function inProject(props: readonly VaultProp[] | undefined, project: Pick<Project, 'uid' | 'name'>): boolean {
  const ref = noteProjectRef(props)
  if (ref.uid) {
    return ref.uid === project.uid
  }
  return !!ref.name && ref.name.toLowerCase() === project.name.trim().toLowerCase()
}

/** The project a note belongs to, among projects, or undefined. */
export function projectOfNote<P extends Pick<Project, 'uid' | 'name'>>(props: readonly VaultProp[] | undefined, projects: readonly P[]): P | undefined {
  const ref = noteProjectRef(props)
  if (!ref.uid && !ref.name) {
    return undefined
  }
  return projects.find((p) => inProject(props, p))
}
