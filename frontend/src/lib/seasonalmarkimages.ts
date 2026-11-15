// the picture that stands in for the Pelton logo on a few days of the year.

import aprilMark from '../assets/images/icons/seasonal/april-first.png'
import easterMark from '../assets/images/icons/seasonal/easter.png'
import halloweenMark from '../assets/images/icons/seasonal/halloween.png'
import christmasMark from '../assets/images/icons/seasonal/christmas.png'
import { seasonalMark, type SeasonalMark } from './seasonalmark'

export interface SeasonalImage {
  src: string
  /** True for art with its own dark background, shown as a rounded tile. */
  tile: boolean
}

const images: Record<SeasonalMark, SeasonalImage> = {
  'april-first': { src: aprilMark, tile: false },
  easter: { src: easterMark, tile: true },
  halloween: { src: halloweenMark, tile: true },
  christmas: { src: christmasMark, tile: true },
}

/** The stand-in for a day, or null on an ordinary one. */
export function seasonalImage(date: Date = new Date()): SeasonalImage | null {
  const mark = seasonalMark(date)
  return mark ? images[mark] : null
}
