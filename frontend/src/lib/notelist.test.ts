import { describe, expect, it } from 'vitest'
import { folderCounts, folderRows, hasTag, notesInScope, sortNotes } from './notelist'
import type { VaultNote } from './types'

function note(path: string, mtime: number, tags: string[] = []): VaultNote {
  return { path, title: path.replace(/^.*\//, '').replace(/\.md$/, ''), mtime, size: 0, tags, aliases: [], headings: [], snippet: '', links: [], props: [] }
}

const notes = [note('b.md', 3, ['Work/Plan']), note('a/x.md', 1), note('a/b/y.md', 2, ['other']), note('c 10.md', 5), note('c 9.md', 4)]

describe('notesInScope', () => {
  it('takes a folder with its subfolders, a tag with nested tags, and the starred order', () => {
    expect(notesInScope(notes, { kind: 'folder', path: 'a' }).map((n) => n.path)).toEqual(['a/x.md', 'a/b/y.md'])
    expect(notesInScope(notes, { kind: 'tag', tag: 'work' }).map((n) => n.path)).toEqual(['b.md'])
    expect(notesInScope(notes, { kind: 'starred' }, ['c 9.md', 'b.md']).map((n) => n.path)).toEqual(['c 9.md', 'b.md'])
    expect(notesInScope(notes, { kind: 'all' })).toHaveLength(5)
    expect(hasTag(notes[0], 'work/plan')).toBe(true)
    expect(hasTag(notes[0], 'wor')).toBe(false)
  })

  it('takes the notes whose front matter puts them in a project', () => {
    const inIt = { ...note('p/one.md', 1), props: [{ key: 'project-id', value: 'u1', list: [] }] }
    const byName = { ...note('two.md', 1), props: [{ key: 'Project', value: '[[Launch]]', list: [] }] }
    const other = { ...note('three.md', 1), props: [{ key: 'project-id', value: 'u2', list: [] }, { key: 'project', value: 'Launch', list: [] }] }
    expect(notesInScope([...notes, inIt, byName, other], { kind: 'project', uid: 'u1', name: 'launch' }).map((n) => n.path)).toEqual(['p/one.md', 'two.md'])
  })
})

describe('sortNotes', () => {
  it('orders by time and by name with numbers in order', () => {
    expect(sortNotes(notes, 'modified').map((n) => n.mtime)).toEqual([5, 4, 3, 2, 1])
    expect(sortNotes(notes, 'modifiedOld').map((n) => n.mtime)).toEqual([1, 2, 3, 4, 5])
    expect(sortNotes(notes, 'title').map((n) => n.title)).toEqual(['b', 'c 9', 'c 10', 'x', 'y'])
    expect(sortNotes(notes, 'titleDesc').map((n) => n.title)).toEqual(['y', 'x', 'c 10', 'c 9', 'b'])
  })
})

describe('folders', () => {
  it('lists folders with depth, hiding the children of collapsed ones', () => {
    const folders = ['a', 'a/b', 'a/b/c', 'z']
    expect(folderRows(folders, new Set()).map((r) => r.path)).toEqual(['a', 'z'])
    expect(folderRows(folders, new Set(['a'])).map((r) => [r.path, r.depth, r.hasChildren])).toEqual([
      ['a', 0, true],
      ['a/b', 1, true],
      ['z', 0, false],
    ])
  })

  it('counts the notes below each folder', () => {
    const counts = folderCounts(notes)
    expect(counts.get('a')).toBe(2)
    expect(counts.get('a/b')).toBe(1)
  })
})
