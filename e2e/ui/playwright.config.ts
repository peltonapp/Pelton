import { defineConfig } from '@playwright/test'

const baseURL = process.env.PELTON_URL
if (!baseURL) {
  throw new Error('PELTON_URL is required (the Wails dev server, not the Vite URL)')
}

export default defineConfig({
  testDir: '.',
  timeout: 180_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL,
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
  },
  // inbox onboards and adds Alice and Bob; flows needs both accounts.
  projects: [
    { name: 'inbox', testMatch: 'inbox.spec.ts' },
    { name: 'flows', testMatch: 'flows.spec.ts', dependencies: ['inbox'] },
  ],
})
