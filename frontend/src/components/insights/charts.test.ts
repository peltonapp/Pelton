import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/svelte'
import BarChart from './BarChart.svelte'
import HeatGrid from './HeatGrid.svelte'
import CountUp from './CountUp.svelte'
import RankList from './RankList.svelte'

beforeEach(() => {
  document.documentElement.setAttribute('data-reduce-motion', '')
})

afterEach(() => {
  document.documentElement.removeAttribute('data-reduce-motion')
})

describe('BarChart', () => {
  const bars = [
    { label: 'Mon 2 Nov', received: 4, sent: 1 },
    { label: 'Tue 3 Nov', received: 0, sent: 0 },
    { label: 'Wed 4 Nov', received: 8, sent: 2 },
  ]

  it('draws one bar per bucket, scaled to the largest, with a labelled chart', () => {
    const { container } = render(BarChart, { bars, label: 'Mail over time', receivedLabel: 'received', sentLabel: 'sent' })
    expect(screen.getByRole('img', { name: 'Mail over time' })).toBeInTheDocument()
    expect(screen.getAllByTestId('bar')).toHaveLength(3)
    const received = container.querySelectorAll('rect.received')
    expect(received).toHaveLength(2)
    // the tallest stack fills the chart: 8 received and 2 sent of a max of 10.
    expect(Number(received[1].getAttribute('height'))).toBeCloseTo(80)
    expect(Number(container.querySelectorAll('rect.sent')[1].getAttribute('height'))).toBeCloseTo(20)
  })

  it('gives a screen reader the real numbers', () => {
    render(BarChart, { bars, label: 'Mail over time', receivedLabel: 'received', sentLabel: 'sent' })
    const row = screen.getByRole('rowheader', { name: 'Wed 4 Nov' }).closest('tr')!
    expect(row.textContent).toContain('8 received')
    expect(row.textContent).toContain('2 sent')
  })
})

describe('HeatGrid', () => {
  const days = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
  const grid = days.map(() => new Array<number>(24).fill(0))
  grid[1][9] = 10
  grid[1][10] = 1

  it('draws 7 by 24 cells, darker for more mail', () => {
    const { container } = render(HeatGrid, { grid, days, label: 'By hour', cellText: (d: string, h: number, n: number) => `${d} ${h}: ${n}` })
    expect(screen.getAllByTestId('cell')).toHaveLength(168)
    expect(container.querySelectorAll('rect.l5')).toHaveLength(1)
    expect(container.querySelectorAll('rect.l1')).toHaveLength(1)
    expect(container.querySelectorAll('rect.l0')).toHaveLength(166)
    expect(screen.getByRole('img', { name: 'By hour' })).toBeInTheDocument()
  })
})

describe('CountUp', () => {
  it('shows the final value at once with reduced motion', () => {
    const { container } = render(CountUp, { value: 1234 })
    expect(container.textContent).toContain((1234).toLocaleString())
  })
})

describe('RankList', () => {
  it('ranks entries with meters and says when there are none', () => {
    const { container } = render(RankList, {
      items: [
        { key: 'a', label: 'Ada', value: 10 },
        { key: 'b', label: 'Bob', value: 5 },
      ],
      empty: 'Nobody',
    })
    const meters = container.querySelectorAll<HTMLElement>('.meter span')
    expect(meters[0].style.width).toBe('100%')
    expect(meters[1].style.width).toBe('50%')
    render(RankList, { items: [], empty: 'Nobody' })
    expect(screen.getByText('Nobody')).toBeInTheDocument()
  })
})
