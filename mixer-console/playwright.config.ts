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
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: {
        browserName: 'chromium',
        permissions: ['camera'],
        launchOptions: {
          args: ['--no-sandbox', '--disable-dev-shm-usage', '--use-fake-ui-for-media-stream'],
        },
      },
    },
    {
      name: 'firefox',
      grep: /guests carousel stays visible below the mosaic video/,
      use: {
        browserName: 'firefox',
      },
    },
  ],
});
