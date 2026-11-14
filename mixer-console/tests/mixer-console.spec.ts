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

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function parameterValue(page: Page, id: string, name: string): Locator {
  return panel(page, id)
    .locator('.parameter')
    .filter({
      has: page.locator('dt', { hasText: new RegExp(`^${escapeRegExp(name)}$`) }),
    })
    .locator('dd');
}

async function detailsHeight(page: Page, id: string): Promise<number> {
  return page
    .locator(`.subpanel[data-id="${id}"] .parameter-details`)
    .evaluate((element) => element.getBoundingClientRect().height);
}

async function boundingBox(page: Page, selector: string) {
  const box = await page.locator(selector).boundingBox();
  if (!box) {
    throw new Error(`element ${selector} has no bounding box`);
  }
  return box;
}

interface VideoSample {
  currentTime: number;
  width: number;
  height: number;
  readyState: number;
  range: number;
}

async function sampleVideo(page: Page, videoId: string): Promise<VideoSample> {
  return page.evaluate((selector) => {
    const video = document.querySelector<HTMLVideoElement>(selector);
    if (!video) {
      throw new Error(`video element ${selector} is missing`);
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
  }, videoId);
}

test('control panel switches each subsystem on and off independently', async ({ page }) => {
  const consoleURL = requireEnv('MIXER_CONSOLE_URL');
  const studioRoom = requireEnv('MEDIA_MIXER_SOURCE_ROOM');
  const broadcastRoom = requireEnv('MEDIA_MIXER_TARGET_ROOM');
  const guestsRoom = requireEnv('MOSAIC_SOURCE_ROOM');
  const mosaicRoom = requireEnv('MOSAIC_TARGET_ROOM');
  await stopAll(consoleURL);

  try {
    await page.goto('/');

    await expect(page.getByRole('region', { name: 'Control panel' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Media mixer' })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Mosaic composer' })).toBeVisible();

    await expect(parameterValue(page, 'media-mixer', 'LiveKit connection')).toBeVisible();
    await expect(parameterValue(page, 'media-mixer', 'Inputs')).toBeVisible();
    await expect(parameterValue(page, 'media-mixer', 'Outputs')).toBeVisible();
    await expect(parameterValue(page, 'mosaic-composer', 'Guests')).toBeVisible();

    await expect(parameterValue(page, 'media-mixer', 'LiveKit connection')).toHaveText('connected');
    await expect(parameterValue(page, 'media-mixer', 'Studio LiveKit room')).toHaveText(studioRoom);
    await expect(parameterValue(page, 'media-mixer', 'Broadcast LiveKit room')).toHaveText(broadcastRoom);
    await expect(parameterValue(page, 'mosaic-composer', 'Guests LiveKit room')).toHaveText(guestsRoom);
    await expect(parameterValue(page, 'mosaic-composer', 'Mosaic LiveKit room')).toHaveText(mosaicRoom);

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

test('control panel expands to show all tracked parameters', async ({ page }) => {
  await page.goto('/');

  const controlPanel = page.locator('#control-panel');

  await expect(controlPanel).toHaveAttribute('data-expanded', 'false');
  await expect.poll(() => detailsHeight(page, 'media-mixer')).toBe(0);
  await expect.poll(() => detailsHeight(page, 'mosaic-composer')).toBe(0);
  await expect(page.getByText('Studio LiveKit room')).toBeHidden();
  await expect(page.getByText('Mosaic LiveKit room')).toBeHidden();

  await controlPanel.click({ position: { x: 6, y: 6 } });

  await expect(controlPanel).toHaveAttribute('data-expanded', 'true');
  await expect.poll(() => detailsHeight(page, 'media-mixer')).toBeGreaterThan(0);
  await expect.poll(() => detailsHeight(page, 'mosaic-composer')).toBeGreaterThan(0);
  await expect(page.getByText('Studio LiveKit room')).toBeVisible();
  await expect(page.getByText('Mosaic LiveKit room')).toBeVisible();

  await controlPanel.click({ position: { x: 6, y: 6 } });

  await expect(controlPanel).toHaveAttribute('data-expanded', 'false');
  await expect.poll(() => detailsHeight(page, 'media-mixer')).toBe(0);
  await expect.poll(() => detailsHeight(page, 'mosaic-composer')).toBe(0);
  await expect(page.getByText('Studio LiveKit room')).toBeHidden();
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
      .poll(async () => (await sampleVideo(page, '#broadcast-video')).width, { timeout: 30_000 })
      .toBeGreaterThan(0);

    const first = await sampleVideo(page, '#broadcast-video');
    await expect
      .poll(async () => (await sampleVideo(page, '#broadcast-video')).currentTime, { timeout: 15_000 })
      .toBeGreaterThan(first.currentTime + 0.2);
    await expect
      .poll(async () => (await sampleVideo(page, '#broadcast-video')).range, { timeout: 15_000 })
      .toBeGreaterThan(8);

    await postAction(consoleURL, 'media-mixer', 'stop');

    await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });
  } finally {
    await stopAll(consoleURL);
  }
});

test('guests panel displays the MosaicComposer stream', async ({ page }) => {
  const consoleURL = requireEnv('MIXER_CONSOLE_URL');
  await stopAll(consoleURL);

  const status = page.locator('#guests-status');

  try {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: 'Guests' })).toBeVisible();
    await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });

    await postAction(consoleURL, 'mosaic-composer', 'start');

    await expect(status).toHaveAttribute('data-state', 'live', { timeout: 30_000 });
    await expect
      .poll(async () => (await sampleVideo(page, '#guests-video')).width, { timeout: 30_000 })
      .toBeGreaterThan(0);

    await expect
      .poll(async () => (await sampleVideo(page, '#guests-video')).range, { timeout: 15_000 })
      .toBeGreaterThan(8);

    await postAction(consoleURL, 'mosaic-composer', 'stop');

    await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });
  } finally {
    await stopAll(consoleURL);
  }
});

test('studio panel displays the studio source stream', async ({ page }) => {
  const status = page.locator('#studio-status');

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Studio' })).toBeVisible();
  await expect(status).toHaveAttribute('data-state', 'live', { timeout: 30_000 });
  await expect
    .poll(async () => (await sampleVideo(page, '#studio-video')).width, { timeout: 30_000 })
    .toBeGreaterThan(0);

  const first = await sampleVideo(page, '#studio-video');
  await expect
    .poll(async () => (await sampleVideo(page, '#studio-video')).currentTime, { timeout: 15_000 })
    .toBeGreaterThan(first.currentTime + 0.2);
  await expect
    .poll(async () => (await sampleVideo(page, '#studio-video')).range, { timeout: 15_000 })
    .toBeGreaterThan(8);
});

test('panels follow the control stripe and three-by-three grid layout', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 1200 });
  await page.goto('/');

  const control = await boundingBox(page, '#control-panel');
  const grid = await boundingBox(page, '#panel-grid');
  const broadcast = await boundingBox(page, '#broadcast-panel');
  const guests = await boundingBox(page, '#guests-panel');
  const studio = await boundingBox(page, '#studio-panel');

  expect(control.y).toBe(0);
  expect(control.width).toBeGreaterThanOrEqual(1599);
  expect(grid.y).toBeGreaterThanOrEqual(control.y + control.height - 1);
  expect(grid.width).toBeGreaterThanOrEqual(1599);
  expect(grid.y + grid.height).toBeLessThanOrEqual(1200);

  expect(broadcast.x).toBeLessThan(guests.x);
  expect(broadcast.y).toBeLessThan(studio.y);
  expect(guests.x).toBeGreaterThan(broadcast.x + broadcast.width - 1);
  expect(studio.y).toBeGreaterThan(broadcast.y + broadcast.height - 1);

  expect(guests.height).toBeCloseTo(broadcast.height, 0);
  expect(studio.height).toBeLessThan(broadcast.height);
  expect(broadcast.width).toBeGreaterThan(guests.width * 1.8);
  expect(broadcast.width).toBeLessThan(guests.width * 2.2);
  expect(studio.width).toBeGreaterThan(guests.width * 2.8);
  expect(studio.width).toBeLessThan(guests.width * 3.2);

  const guestsVideo = await boundingBox(page, '#guests-video');
  expect(guestsVideo.y).toBeLessThan(guests.y + guests.height / 2);

  const studioVideo = await boundingBox(page, '#studio-video');
  expect(studioVideo.x).toBeLessThan(studio.x + studio.width / 2);
  expect(studioVideo.x + studioVideo.width).toBeLessThanOrEqual(studio.x + studio.width - 1);
});

test('panels stack without horizontal overflow on a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 900 });
  await page.goto('/');

  let previousBottom = 0;
  for (const selector of [
    '#control-panel',
    '#broadcast-panel',
    '#guests-panel',
    '#studio-panel',
  ]) {
    await expect(page.locator(selector)).toBeVisible();

    const box = await page.locator(selector).boundingBox();
    if (!box) {
      throw new Error(`panel ${selector} has no bounding box`);
    }

    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(previousBottom);
    previousBottom = box.y + box.height;
  }

  const hasHorizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > document.documentElement.clientWidth,
  );
  expect(hasHorizontalOverflow).toBe(false);
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
