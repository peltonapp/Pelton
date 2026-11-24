import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/svelte'
import ShellSkeleton from './ShellSkeleton.svelte'

describe('ShellSkeleton', () => {
  it('draws three placeholder entries by default, the first lit', () => {
    const { container } = render(ShellSkeleton)
    const rail = container.querySelector('.rail.shown') as HTMLElement
    expect(rail.querySelectorAll('i')).toHaveLength(3)
    expect(rail.querySelector('i.on')).toBe(rail.querySelector('i'))
    expect(container.querySelector('.scene')).toBeNull()
  })

  it('draws exactly the given spaces, lighting the scene and marking pending ones', () => {
    const { container } = render(ShellSkeleton, {
      props: {
        size: 'large',
        scene: 'notes',
        spaces: [{ key: 'mail', icon: '<svg></svg>' }, { key: 'notes', icon: '<svg></svg>' }, { key: 'chat', icon: '<svg></svg>', pending: true }],
      },
    })
    const rail = container.querySelector('.rail.shown') as HTMLElement
    const entries = rail.querySelectorAll('i')
    expect(entries).toHaveLength(3)
    expect(entries[1].classList.contains('on')).toBe(true)
    expect(entries[2].classList.contains('pending')).toBe(true)
    expect(entries[0].querySelector('svg')).not.toBeNull()
  })

  it('hints at a space of its own instead of the mail panes', () => {
    const { container } = render(ShellSkeleton, { props: { scene: 'calendar' } })
    expect(container.querySelector('.scene.calendar')).not.toBeNull()
    expect(container.querySelector('.surface.own')).not.toBeNull()
  })
})
