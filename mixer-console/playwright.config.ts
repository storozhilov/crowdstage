import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.ts',
  globalSetup: './tests/global-setup.ts',
  fullyParallel: false,
  workers: 1,
  timeout: 120_000,
  expect: {
    timeout: 30_000,
  },
  reporter: [['list']],
  use: {
    baseURL: process.env.MIXER_CONSOLE_URL,
    permissions: ['camera'],
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    launchOptions: {
      args: ['--no-sandbox', '--disable-dev-shm-usage', '--use-fake-ui-for-media-stream'],
    },
  },
});
