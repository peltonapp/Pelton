import { describe, expect, it } from 'vitest'
import { isEditableTarget } from './editable'

describe('isEditableTarget', () => {
  it('is true for form fields', () => {
    for (const tag of ['input', 'textarea', 'select']) {
      expect(isEditableTarget(document.createElement(tag))).toBe(true)
    }
  })

  it('is true for editable content', () => {
    const div = document.createElement('div')
    Object.defineProperty(div, 'isContentEditable', { value: true })
    expect(isEditableTarget(div)).toBe(true)
  })

  it('is false for other elements and no target', () => {
    expect(isEditableTarget(document.createElement('button'))).toBe(false)
    expect(isEditableTarget(document.createElement('div'))).toBe(false)
    expect(isEditableTarget(null)).toBe(false)
  })
})
