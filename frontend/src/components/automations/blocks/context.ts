// context.ts is what the block editor shares with the blocks it renders: the
// tree and catalog as stores, the problems and test results per block, and
// the editor's handful of commands. Nested blocks read it with getEditor()
// instead of passing a dozen props down every level.

import { getContext, setContext } from 'svelte'
import type { Readable, Writable } from 'svelte/store'
import type { BlockTree, ScriptCatalog } from '../../../lib/scripting'
import type { PaletteItem } from './palette'
import type { RunMark } from './runmarks'
import type { ScopeContext, ScopeEntry } from './scope'
import type { CondPath, Place } from './tree'
import type { Issue } from './validate'

/** Where something new goes: among blocks, or into a condition group. */
export type InsertTarget = { kind: 'block'; place: Place } | { kind: 'cond'; group: CondPath; index: number }

/** What is being dragged. */
export type DragPayload = { kind: 'palette'; item: PaletteItem } | { kind: 'block'; id: string } | { kind: 'cond'; cp: CondPath }

/** What the editor shares with every block. */
export interface EditorContext {
  tree: Readable<BlockTree>
  catalog: Readable<ScriptCatalog>
  scopeCtx: Readable<ScopeContext>
  /** Every value any block could refer to, for labelling chips. */
  allEntries: Readable<ScopeEntry[]>
  issues: Readable<Issue[]>
  marks: Readable<Map<string, RunMark>>
  /** The selected block. */
  selected: Writable<string | null>
  /** Where a drag would drop now. */
  dropAt: Readable<InsertTarget | null>
  dragging: Readable<DragPayload | null>
  /** Replaces the tree as one undoable edit, and selects a block if given. */
  apply(next: BlockTree, select?: string): void
  /** Replaces the tree with a moved block and announces where it landed. */
  move(next: BlockTree, id: string): void
  /** Starts dragging from a pointer press. */
  startDrag(event: PointerEvent, payload: DragPayload, label: string): void
  /** Opens the add menu at a target, hanging off an element. */
  openInsert(anchor: HTMLElement, target: InsertTarget): void
  /** The keyboard commands of a focused block. */
  blockKey(event: KeyboardEvent, id: string): void
}

const key = Symbol('block-editor')

/** Shares the editor with the blocks below. */
export function setEditor(ctx: EditorContext): void {
  setContext(key, ctx)
}

/** The editor the calling block sits in. */
export function getEditor(): EditorContext {
  return getContext<EditorContext>(key)
}
