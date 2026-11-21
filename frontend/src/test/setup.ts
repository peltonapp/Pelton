// Loaded before every test file: jest-dom's matchers, and a DOM torn down
// between tests so one test's markup cannot be found by the next.
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/svelte'
import { afterEach } from 'vitest'

// jsdom lays nothing out, so it has no scrollIntoView. Components that keep a
// highlighted row in view call it, and an unimplemented method would surface
// as an unhandled rejection from the tick they call it on.
if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = () => {}
}

// Nor does it measure text. Both compose editors ask a range for its rects to
// scroll the caret into view when they take focus.
if (!Range.prototype.getClientRects) {
  const rect = { x: 0, y: 0, top: 0, left: 0, right: 0, bottom: 0, width: 0, height: 0 } as DOMRect
  Range.prototype.getClientRects = () => [rect] as unknown as DOMRectList
  Range.prototype.getBoundingClientRect = () => rect
}

afterEach(() => {
  cleanup()
})
