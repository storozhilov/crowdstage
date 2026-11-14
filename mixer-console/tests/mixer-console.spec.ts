import { expect, test, type Locator, type Page } from '@playwright/test';

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function baseURL(url: string): string {
  return url.replace(/\/+$/, '');
}

async function postAction(
  consoleURL: string,
  subsystem: 'media-mixer' | 'mosaic-composer',
  action: 'start' | 'stop',
): Promise<void> {
  const response = await fetch(
    `${baseURL(consoleURL)}/api/subsystems/${subsystem}/${action}`,
    { method: 'POST' },
  );
  if (!response.ok) {
    throw new Error(
      `POST ${subsystem}/${action} status = ${response.status}`,
    );
  }
}

async function stopAll(consoleURL: string): Promise<void> {
  await postAction(consoleURL, 'media-mixer', 'stop').catch(() => undefined);
  await postAction(consoleURL, 'mosaic-composer', 'stop').catch(() => undefined);
}

function panel(page: Page, id: string): Locator {
  return page.locator(`.subpanel[data-id="${id}"]`);
}

function switchButton(page: Page, id: string): Locator {
  return panel(page, id).locator('[data-role="switch"]');
}

function stateBadge(page: Page, id: string): Locator {
  return panel(page, id).locator('.badge.state');
}

function parameterValue(page: Page, id: string, name: string): Locator {
  return panel(page, id).locator('.parameter', { hasText: name }).locator('dd');
}

interface VideoSample {
  currentTime: number;
  width: number;
  height: number;
  readyState: number;
  range: number;
}

async function sampleVideo(page: Page): Promise<VideoSample> {
  return page.evaluate(() => {
    const video = document.querySelector<HTMLVideoElement>('#broadcast-video');
    if (!video) {
      throw new Error('broadcast video element is missing');
    }

    const canvas = document.createElement('canvas');
    canvas.width = 64;
    canvas.height = 36;

    const context = canvas.getContext('2d');
    if (!context) {
      throw new Error('2D canvas context is unavailable');
    }

    context.drawImage(video, 0, 0, canvas.width, canvas.height);
    const { data } = context.getImageData(0, 0, canvas.width, canvas.height);

    let minimum = 255;
    let maximum = 0;
    for (let offset = 0; offset < data.length; offset += 4) {
      const luma = (data[offset] + data[offset + 1] + data[offset + 2]) / 3;
      if (luma < minimum) {
        minimum = luma;
      }
      if (luma > maximum) {
        maximum = luma;
      }
    }

    return {
      currentTime: video.currentTime,
      width: video.videoWidth,
      height: video.videoHeight,
      readyState: video.readyState,
      range: maximum - minimum,
    };
  });
}

test('control panel switches each subsystem on and off independently', async ({ page }) => {
  const consoleURL = requireEnv('MIXER_CONSOLE_URL');
  await stopAll(consoleURL);

  try {
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'Control panel' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Media mixer' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Mosaic composer' })).toBeVisible();

    const mixerSwitch = switchButton(page, 'media-mixer');
    const mosaicSwitch = switchButton(page, 'mosaic-composer');

    await expect(mixerSwitch).toHaveAttribute('data-state', 'off', { timeout: 30_000 });
    await expect(mosaicSwitch).toHaveAttribute('data-state', 'off', { timeout: 30_000 });
    await expect(stateBadge(page, 'media-mixer')).toHaveText('idle');
    await expect(stateBadge(page, 'mosaic-composer')).toHaveText('idle');
    await expect(parameterValue(page, 'media-mixer', 'Outputs')).toHaveText('1');

    await mixerSwitch.click();
    await expect(mixerSwitch).toHaveAttribute('data-state', 'on', { timeout: 30_000 });
    await expect(stateBadge(page, 'media-mixer')).toHaveText('running');
    await expect(mosaicSwitch).toHaveAttribute('data-state', 'off');
    await expect(stateBadge(page, 'mosaic-composer')).toHaveText('idle');

    await mosaicSwitch.click();
    await expect(mosaicSwitch).toHaveAttribute('data-state', 'on', { timeout: 30_000 });
    await expect(stateBadge(page, 'mosaic-composer')).toHaveText('running');
    await expect(parameterValue(page, 'mosaic-composer', 'Grid')).toHaveText('4x3');
    await expect(parameterValue(page, 'mosaic-composer', 'Rotation timeout')).toHaveText('10s');
    await expect(mixerSwitch).toHaveAttribute('data-state', 'on');

    await mixerSwitch.click();
    await expect(mixerSwitch).toHaveAttribute('data-state', 'off', { timeout: 30_000 });
    await expect(stateBadge(page, 'media-mixer')).toHaveText('idle');
    await expect(mosaicSwitch).toHaveAttribute('data-state', 'on');
    await expect(stateBadge(page, 'mosaic-composer')).toHaveText('running');

    await mosaicSwitch.click();
    await expect(mosaicSwitch).toHaveAttribute('data-state', 'off', { timeout: 30_000 });
    await expect(stateBadge(page, 'mosaic-composer')).toHaveText('idle');
  } finally {
    await stopAll(consoleURL);
  }
});

test('broadcast panel displays the MediaMixer stream', async ({ page }) => {
  const consoleURL = requireEnv('MIXER_CONSOLE_URL');
  await stopAll(consoleURL);

  const status = page.locator('#broadcast-status');

  try {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Broadcast' })).toBeVisible();
    await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });

    await postAction(consoleURL, 'media-mixer', 'start');

    await expect(status).toHaveAttribute('data-state', 'live', { timeout: 30_000 });
    await expect
      .poll(async () => (await sampleVideo(page)).width, { timeout: 30_000 })
      .toBeGreaterThan(0);

    const first = await sampleVideo(page);
    await expect
      .poll(async () => (await sampleVideo(page)).currentTime, { timeout: 15_000 })
      .toBeGreaterThan(first.currentTime + 0.2);
    await expect
      .poll(async () => (await sampleVideo(page)).range, { timeout: 15_000 })
      .toBeGreaterThan(8);

    await postAction(consoleURL, 'media-mixer', 'stop');

    await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });
  } finally {
    await stopAll(consoleURL);
  }
});

test('a failed subsystem can still be stopped', async ({ page }) => {
  let mixerState = 'failed';
  let stopRequested = false;

  await page.route('**/api/subsystems/media-mixer/stop', async (route) => {
    stopRequested = true;
    mixerState = 'idle';
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'stopped',
        subsystem: {
          id: 'media-mixer',
          title: 'Media mixer',
          ok: true,
          response: { status: 'stopped' },
        },
      }),
    });
  });

  await page.route('**/api/subsystems', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.fallback();
      return;
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'degraded',
        subsystems: [
          {
            id: 'media-mixer',
            title: 'Media mixer',
            reachable: true,
            status: 'degraded',
            state: mixerState,
            parameters: [
              { name: 'Inputs', value: '0' },
              { name: 'Outputs', value: '1' },
            ],
          },
          {
            id: 'mosaic-composer',
            title: 'Mosaic composer',
            reachable: true,
            status: 'ok',
            state: 'idle',
            parameters: [],
          },
        ],
      }),
    });
  });

  await page.goto('/');

  const mixerSwitch = switchButton(page, 'media-mixer');
  await expect(mixerSwitch).toHaveAttribute('data-state', 'failed', { timeout: 30_000 });
  await expect(mixerSwitch).toBeEnabled();
  await expect(stateBadge(page, 'media-mixer')).toHaveText('failed');
  await expect(mixerSwitch.locator('[data-role="label"]')).toHaveText('Media mixer failed');

  await mixerSwitch.click();

  await expect.poll(() => stopRequested).toBe(true);
  await expect(mixerSwitch).toHaveAttribute('data-state', 'off', { timeout: 30_000 });
  await expect(stateBadge(page, 'media-mixer')).toHaveText('idle');
});
