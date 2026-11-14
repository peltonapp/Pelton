// wailsruntime.ts stands in for @wailsio/runtime under vitest (see the alias in
// vite.config.ts). The real runtime wires itself to window on import and
// talks to a backend that does not exist in tests; tests that need a binding's
// result mock lib/api instead.

// @ts-expect-error: the package ships types only for its public entry points.
export * as Create from '../../node_modules/@wailsio/runtime/dist/create.js'

export class CancellablePromise<T> extends Promise<T> {
  cancel(): void {}
}

export const Call = {
  ByID(): Promise<never> {
    return Promise.reject(new Error('no backend in tests'))
  },
  ByName(): Promise<never> {
    return Promise.reject(new Error('no backend in tests'))
  },
}

export const Events = {
  On(): () => void {
    return () => {}
  },
  Emit(): Promise<void> {
    return Promise.resolve()
  },
}

export const Browser = {
  OpenURL(): Promise<void> {
    return Promise.resolve()
  },
}
