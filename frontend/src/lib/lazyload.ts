// parts of the main window that are kept out of the startup bundle. The window
// loads them once it has settled, so they are ready before they are first used.

/** loadComposer loads the New design's composer. */
export const loadComposer = () => import('../components/compose/Composer2.svelte')

/** loadClassicCompose loads the Classic design's compose window. */
export const loadClassicCompose = () => import('../components/compose/Compose.svelte')

/** loadAttachmentsBrowser loads the New design's attachments browser. */
export const loadAttachmentsBrowser = () => import('../components/attachments/AttachmentsBrowser.svelte')

/** loadMenuBarEditor loads the in-place editor of the in-app menu bar. */
export const loadMenuBarEditor = () => import('../components/common/MenuBarEditor.svelte')

/** loadPromiseStrip loads the reader's strip of promises found in a message. */
export const loadPromiseStrip = () => import('../components/detail/PromiseStrip.svelte')

/** loadEncryptionIndicator loads the reader's line about an encrypted or signed message. */
export const loadEncryptionIndicator = () => import('../components/detail/EncryptionIndicator.svelte')

/** loadInviteCard loads the reader's card for a calendar invitation. */
export const loadInviteCard = () => import('../components/detail/InviteCard.svelte')

/** warmUp runs each loader once the window is idle, ignoring failures. */
export function warmUp(loaders: (() => Promise<unknown>)[]): void {
  const load = () => {
    for (const loader of loaders) {
      void loader().catch(() => undefined)
    }
  }
  const w = window as Window & { requestIdleCallback?: (cb: () => void, opts?: { timeout: number }) => number }
  if (w.requestIdleCallback) {
    w.requestIdleCallback(load, { timeout: 2000 })
  } else {
    setTimeout(load, 300)
  }
}
