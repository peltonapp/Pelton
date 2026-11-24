import { describe, expect, it } from 'vitest'
import {
  defaultRowActions,
  maxRowActions,
  moveRowAction,
  parseRowActions,
  rowActionApplies,
  rowActionIds,
  rowActionView,
  serializeRowActions,
  toggleRowAction,
} from './rowactions'

const plain = { seen: true, flagged: false, pinned: false, muted: false, inSpam: false }

describe('parseRowActions', () => {
  it('reads the stored default', () => {
    expect(parseRowActions('archive,delete,done,snooze,pin')).toEqual([...defaultRowActions])
  })

  it('drops unknown ids, repeats and blanks', () => {
    expect(parseRowActions(' archive, bogus,,archive, flag ')).toEqual(['archive', 'flag'])
  })

  it('keeps at most five', () => {
    expect(parseRowActions(rowActionIds.join(','))).toHaveLength(maxRowActions)
  })

  it('reads an empty value as no buttons', () => {
    expect(parseRowActions('')).toEqual([])
  })

  it('round trips through serialize', () => {
    expect(serializeRowActions(['task', 'read', 'task'])).toBe('task,read')
  })
})

describe('toggleRowAction', () => {
  it('adds at the end and removes in place', () => {
    expect(toggleRowAction(['archive'], 'flag', true)).toEqual(['archive', 'flag'])
    expect(toggleRowAction(['archive', 'flag', 'pin'], 'flag', false)).toEqual(['archive', 'pin'])
  })

  it('refuses a sixth', () => {
    const full = [...defaultRowActions]
    expect(toggleRowAction(full, 'flag', true)).toEqual(full)
  })
})

describe('moveRowAction', () => {
  it('swaps with a neighbour and stays within bounds', () => {
    expect(moveRowAction(['archive', 'delete', 'done'], 1, -1)).toEqual(['delete', 'archive', 'done'])
    expect(moveRowAction(['archive', 'delete'], 0, -1)).toEqual(['archive', 'delete'])
    expect(moveRowAction(['archive', 'delete'], 1, 1)).toEqual(['archive', 'delete'])
  })
})

describe('rowActionView', () => {
  it('names a toggle by the way it would go', () => {
    expect(rowActionView('read', plain).labelKey).toBe('shortcut.markUnread')
    expect(rowActionView('read', { ...plain, seen: false }).labelKey).toBe('shortcut.markRead')
    expect(rowActionView('read', { ...plain, seen: false }).shortcut).toBe('mark-read')
    expect(rowActionView('pin', { ...plain, pinned: true }).labelKey).toBe('pin.unpin')
    expect(rowActionView('spam', { ...plain, inSpam: true }).labelKey).toBe('spam.notSpam')
  })

  it('carries the shortcut each action mirrors', () => {
    expect(rowActionView('delete', plain)).toMatchObject({ shortcut: 'delete-message', danger: true })
    expect(rowActionView('archive', plain)).toMatchObject({ shortcut: 'archive', danger: false })
    expect(rowActionView('project', plain).shortcut).toBe('add-to-project')
  })
})

describe('rowActionApplies', () => {
  it('offers mute only on a conversation and spam only with a spam folder', () => {
    expect(rowActionApplies('mute', { threadId: 0, spam: 'markable' })).toBe(false)
    expect(rowActionApplies('mute', { threadId: 4, spam: 'markable' })).toBe(true)
    expect(rowActionApplies('spam', { threadId: 4, spam: 'none' })).toBe(false)
    expect(rowActionApplies('archive', { threadId: 0, spam: 'none' })).toBe(true)
  })
})
