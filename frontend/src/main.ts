// bundled ui and mono fonts via @fontsource (no runtime cdn). weights cover the
// regular/medium/semibold/bold the ui uses and the mono weights for mail bodies.
import '@fontsource/familjen-grotesk/400.css'
import '@fontsource/familjen-grotesk/500.css'
import '@fontsource/familjen-grotesk/600.css'
import '@fontsource/familjen-grotesk/700.css'

import '@fontsource/spline-sans-mono/300.css'
import '@fontsource/spline-sans-mono/400.css'
import '@fontsource/spline-sans-mono/500.css'
import '@fontsource/spline-sans-mono/600.css'
import '@fontsource/spline-sans-mono/700.css'

// theme tokens must load before the base styles that reference them.
import './theme/tokens.css'
import './style.css'

import { mount } from 'svelte'
import App from './App.svelte'
import MessageWindow from './MessageWindow.svelte'
import { isDemoMode, windowMaterialActive } from './lib/api'
import { setDemoActive } from './lib/demo'

const target = document.getElementById('app')
if (!target) {
  throw new Error('pelton: #app mount point missing')
}

// the cosmetic demo mode (--potatoes-are-nice) has to be known before anything
// renders. Asking for it from the root component's onMount was too late: a
// child mounts before its parent does, so the sidebar and the message list had
// already asked the backend for real mail, and the samples only appeared once
// something triggered a reload.
//
// The root component still asks as well. This runs earlier than any binding
// call the app has made before, so if the backend is not reachable this early
// on some platform, the later answer is what it always was rather than nothing.
await isDemoMode()
  .then(setDemoActive)
  .catch(() => {})

// a window created over a native material (Liquid Glass, Mica) shows it
// through the chrome (style.css), so the class has to be on before the first
// paint.
await windowMaterialActive()
  .then((on) => document.documentElement.classList.toggle('window-material', on))
  .catch(() => {})

// a message opened in a window of its own loads the same page with
// ?window=message&id=N and gets the reading pane alone instead of the app.
const params = new URLSearchParams(window.location.search)
const messageId = Number(params.get('id'))
const app =
  params.get('window') === 'message' && messageId > 0
    ? mount(MessageWindow, { target, props: { messageId } })
    : mount(App, { target })

// remove the pre-mount splash now that svelte has taken over.
document.getElementById('splash')?.remove()

export default app
