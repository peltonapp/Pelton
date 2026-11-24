// platform.ts says which operating system the webview runs on. it imports
// nothing, so any module can read it while the others are still loading.

// isMac drives the modifier symbol and is used by shortcut matching.
export const isMac = /mac/i.test(navigator.userAgent)

// isWindows gates the few settings only Windows has (the Mica material).
export const isWindows = /windows/i.test(navigator.userAgent)
