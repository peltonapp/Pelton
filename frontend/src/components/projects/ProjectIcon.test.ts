import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import ProjectIcon from './ProjectIcon.svelte'
import { parseIconNodes } from '../../lib/projecticons'

const nodes = '[["path",{"d":"M5 4h4l3 3"}],["circle",{"cx":12,"cy":12,"r":2}]]'

describe('a project icon', () => {
  it('draws tabler geometry as an outline at the app stroke width', () => {
    const { container } = render(ProjectIcon, { props: { icon: 'folder', iconNodes: nodes, size: 16 } })
    const svg = container.querySelector('svg')
    expect(svg).toBeTruthy()
    expect(svg?.getAttribute('stroke')).toBe('currentColor')
    expect(svg?.getAttribute('stroke-width')).toBe('1.7')
    expect(container.querySelector('path')?.getAttribute('d')).toBe('M5 4h4l3 3')
    expect(container.querySelector('circle')?.getAttribute('r')).toBe('2')
    expect((screen.getByTestId('project-icon') as HTMLElement).style.width).toBe('16px')
  })

  it('takes parsed nodes as well as json', () => {
    const { container } = render(ProjectIcon, { props: { icon: 'folder', iconNodes: [['path', { d: 'M0 0' }]] } })
    expect(container.querySelector('path')?.getAttribute('d')).toBe('M0 0')
  })

  it('draws an older phosphor name without geometry', () => {
    const { container } = render(ProjectIcon, { props: { icon: 'rocket' } })
    expect(container.querySelector('.ph svg')).toBeTruthy()
    expect(screen.queryByTestId('project-icon')).toBeNull()
  })

  it('draws the default for an empty or unknown name', () => {
    const empty = render(ProjectIcon, { props: { icon: '' } })
    const unknown = render(ProjectIcon, { props: { icon: 'no-such-icon' } })
    expect(empty.container.querySelector('.ph svg')?.innerHTML).toBe(unknown.container.querySelector('.ph svg')?.innerHTML)
  })

  it('falls back to the name when the stored geometry is unusable', () => {
    for (const bad of ['', 'not json', '{}', '[]', '[["path"]]', '[[1,{}]]']) {
      const { container, unmount } = render(ProjectIcon, { props: { icon: 'rocket', iconNodes: bad } })
      expect(container.querySelector('.ph svg')).toBeTruthy()
      unmount()
    }
    expect(parseIconNodes(nodes)).toHaveLength(2)
  })
})
