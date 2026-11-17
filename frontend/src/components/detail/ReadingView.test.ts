import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import userEvent from '@testing-library/user-event'
import { get } from 'svelte/store'
import ReadingView from './ReadingView.svelte'
import { readingView } from '../../stores/readingview'

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
  readingView.set(true)
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('ReadingView', () => {
  it('is a labelled layer over the window', () => {
    render(ReadingView, { props: { label: 'Reading view: Trip plans' } })
    expect(screen.getByRole('dialog', { name: 'Reading view: Trip plans' })).toBeInTheDocument()
  })

  it('leaves on Escape', async () => {
    render(ReadingView, { props: { label: 'Reading view' } })
    await userEvent.keyboard('{Escape}')
    expect(get(readingView)).toBe(false)
  })

  it('lets a dialog opened over it take Escape first', async () => {
    render(ReadingView, { props: { label: 'Reading view' } })
    const dialog = document.createElement('div')
    dialog.setAttribute('aria-modal', 'true')
    document.body.appendChild(dialog)
    await userEvent.keyboard('{Escape}')
    expect(get(readingView)).toBe(true)
  })

  it('gives the focus back when it closes', async () => {
    const before = document.createElement('button')
    document.body.appendChild(before)
    before.focus()
    const { unmount } = render(ReadingView, { props: { label: 'Reading view' } })
    unmount()
    expect(document.activeElement).toBe(before)
  })
})
