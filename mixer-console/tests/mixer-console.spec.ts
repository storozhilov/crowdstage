import { expect, test, type Locator, type Page } from '@playwright/test';
import { RoomServiceClient, TrackType } from 'livekit-server-sdk';

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

async function sampleVideo(
  page: Page,
  videoSelector: string,
  index = 0,
): Promise<VideoSample> {
  return page.evaluate(
    ({ selector, videoIndex }) => {
      const video = document.querySelectorAll<HTMLVideoElement>(selector)[videoIndex];
      if (!video) {
        throw new Error(`video element ${selector}[${videoIndex}] is missing`);
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
    },
    { selector: videoSelector, videoIndex: index },
  );
}

function studioCardRow(page: Page): Locator {
  return page.locator('#studio-card-row');
}

function studioSlots(page: Page): Locator {
  return studioCardRow(page).locator('.card-row-slot');
}

function activeStudioSlots(page: Page): Locator {
  return studioCardRow(page).locator('.card-row-slot:not([data-empty="true"])');
}

async function visibleStudioParticipants(page: Page): Promise<string[]> {
  return page.$$eval(
    '#studio-card-row .card-row-slot:not([data-empty="true"])',
    (slots) => slots.map((slot) => (slot as HTMLElement).dataset.participant ?? ''),
  );
}

function livekitHTTPURL(url: string): string {
  if (url.startsWith('ws://')) {
    return `http://${url.slice('ws://'.length)}`;
  }
  if (url.startsWith('wss://')) {
    return `https://${url.slice('wss://'.length)}`;
  }
  return url;
}

function studioRoomClient(): RoomServiceClient {
  return new RoomServiceClient(
    livekitHTTPURL(requireEnv('LIVEKIT_URL')),
    requireEnv('LIVEKIT_API_KEY'),
    requireEnv('LIVEKIT_API_SECRET'),
  );
}

async function waitForStudioShift(
  page: Page,
  before: string[],
): Promise<string[]> {
  await expect
    .poll(async () => (await visibleStudioParticipants(page)).join('|'))
    .not.toBe(before.join('|'));
  return visibleStudioParticipants(page);
}

async function waitForLiveStudioSlots(page: Page, count: number): Promise<void> {
  await expect(page.locator('#studio-status')).toHaveAttribute('data-state', 'live', {
    timeout: 30_000,
  });
  await expect
    .poll(async () => activeStudioSlots(page).count(), { timeout: 30_000 })
    .toBe(count);
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

test('control panel always shows all tracked parameters', async ({ page }) => {
  await page.goto('/');

  const controlPanel = page.locator('#control-panel');
  await expect(controlPanel).toBeVisible();
  await expect(controlPanel).not.toHaveAttribute('data-expanded');
  await expect(controlPanel).not.toHaveAttribute('aria-expanded');
  await expect(page.locator('.parameter-details')).toHaveCount(0);

  await expect(parameterValue(page, 'media-mixer', 'Studio LiveKit room')).toBeVisible();
  await expect(parameterValue(page, 'media-mixer', 'Broadcast LiveKit room')).toBeVisible();
  await expect(parameterValue(page, 'mosaic-composer', 'Mosaic LiveKit room')).toBeVisible();
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

test('studio panel displays studio streams in a three-card row', async ({ page }) => {
  const room = requireEnv('MIXER_CONSOLE_STUDIO_ROOM');
  const identityPrefix = requireEnv('STUDIO_PUBLISHER_IDENTITY');
  const client = studioRoomClient();

  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Studio' })).toBeVisible();
  await expect(studioSlots(page)).toHaveCount(3);
  await waitForLiveStudioSlots(page, 3);

  const publishers = (await client.listParticipants(room))
    .filter(
      (participant) =>
        participant.identity.startsWith(identityPrefix) &&
        participant.tracks.some((track) => track.type === TrackType.VIDEO),
    )
    .map((participant) => participant.identity)
    .sort();

  expect(await visibleStudioParticipants(page)).toEqual(publishers.slice(0, 3));

  for (let index = 0; index < 3; index += 1) {
    await expect
      .poll(async () => (await sampleVideo(page, '#studio-card-row .card-row-slot video', index)).width, {
        timeout: 30_000,
      })
      .toBeGreaterThan(0);

    const first = await sampleVideo(page, '#studio-card-row .card-row-slot video', index);
    await expect
      .poll(
        async () =>
          (await sampleVideo(page, '#studio-card-row .card-row-slot video', index)).currentTime,
        { timeout: 15_000 },
      )
      .toBeGreaterThan(first.currentTime + 0.2);
    await expect
      .poll(async () => (await sampleVideo(page, '#studio-card-row .card-row-slot video', index)).range, {
        timeout: 15_000,
      })
      .toBeGreaterThan(8);
  }
});

test('studio card row switches streams without looping', async ({ page }) => {
  await page.goto('/');
  await waitForLiveStudioSlots(page, 3);

  const previous = studioCardRow(page).locator('[data-role="previous"]');
  const next = studioCardRow(page).locator('[data-role="next"]');

  await expect(previous).toBeVisible();
  await expect(next).toBeVisible();
  await expect(previous).toBeDisabled();
  await expect(next).toBeEnabled();

  const initial = await visibleStudioParticipants(page);
  expect(initial).toHaveLength(3);

  await next.click();
  let current = await waitForStudioShift(page, initial);
  expect(current[0]).toBe(initial[1]);
  expect(current[1]).toBe(initial[2]);
  await expect(previous).toBeEnabled();

  for (let step = 0; step < 10 && (await next.isEnabled()); step += 1) {
    const before = current;
    await next.click();
    current = await waitForStudioShift(page, before);
    expect(current[0]).toBe(before[1]);
    expect(current[1]).toBe(before[2]);
  }
  await expect(next).toBeDisabled();

  while (await previous.isEnabled()) {
    const before = current;
    await previous.click();
    current = await waitForStudioShift(page, before);
    expect(current[1]).toBe(before[0]);
    expect(current[2]).toBe(before[1]);
  }
  await expect(previous).toBeDisabled();
  expect(current).toEqual(initial);
});

test('control panel and studio share the three-by-three grid bottom row', async ({ page }) => {
  await page.setViewportSize({ width: 1600, height: 1200 });
  await page.goto('/');

  const grid = await boundingBox(page, '#panel-grid');
  const control = await boundingBox(page, '#control-panel');
  const broadcast = await boundingBox(page, '#broadcast-panel');
  const guests = await boundingBox(page, '#guests-panel');
  const studio = await boundingBox(page, '#studio-panel');

  expect(grid.y).toBe(0);
  expect(grid.width).toBeGreaterThanOrEqual(1599);
  expect(grid.y + grid.height).toBeLessThanOrEqual(1200);

  expect(broadcast.x).toBeLessThan(guests.x);
  expect(broadcast.y).toBeLessThan(studio.y);
  expect(guests.x).toBeGreaterThan(broadcast.x + broadcast.width - 1);
  expect(studio.y).toBeGreaterThan(broadcast.y + broadcast.height - 1);

  expect(guests.height).toBeCloseTo(broadcast.height, 0);
  expect(studio.height).toBeCloseTo(control.height, 0);
  expect(studio.y).toBeCloseTo(control.y, 0);
  expect(control.x).toBeGreaterThan(studio.x + studio.width - 1);
  expect(control.x + control.width).toBeLessThanOrEqual(grid.x + grid.width);
  expect(control.y + control.height).toBeLessThanOrEqual(grid.y + grid.height);

  expect(broadcast.width).toBeGreaterThan(guests.width * 1.8);
  expect(broadcast.width).toBeLessThan(guests.width * 2.2);
  expect(studio.width).toBeGreaterThan(guests.width * 1.8);
  expect(studio.width).toBeLessThan(guests.width * 2.2);
  expect(control.width).toBeGreaterThan(guests.width * 0.8);
  expect(control.width).toBeLessThan(guests.width * 1.2);

  const mixer = await boundingBox(page, '.subpanel[data-id="media-mixer"]');
  const mosaic = await boundingBox(page, '.subpanel[data-id="mosaic-composer"]');
  expect(mixer.x).toBeLessThan(mosaic.x);

  const mixerSwitch = await boundingBox(page, '.subpanel[data-id="media-mixer"] [data-role="switch"]');
  const mixerParameters = await boundingBox(
    page,
    '.subpanel[data-id="media-mixer"] [data-role="parameters"]',
  );
  expect(mixerSwitch.y + mixerSwitch.height).toBeLessThanOrEqual(mixerParameters.y);

  const guestsVideo = await boundingBox(page, '#guests-video');
  expect(guestsVideo.y).toBeLessThan(guests.y + guests.height / 2);

  const studioCardRow = await boundingBox(page, '#studio-card-row');
  expect(studioCardRow.x).toBeLessThan(studio.x + studio.width / 2);
  expect(studioCardRow.x + studioCardRow.width).toBeLessThanOrEqual(
    studio.x + studio.width - 1,
  );

  const studioSlots = await page.locator('#studio-card-row .card-row-slot').all();
  const slotBoxes = await Promise.all(studioSlots.map((slot) => slot.boundingBox()));
  for (const slot of slotBoxes) {
    if (!slot) {
      throw new Error('studio card slot has no bounding box');
    }
  }
  const widths = slotBoxes.map((slot) => slot?.width ?? 0);
  expect(widths[0]).toBeCloseTo(widths[1], 0);
  expect(widths[1]).toBeCloseTo(widths[2], 0);
});

test('panels stack without horizontal overflow on a narrow viewport', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 900 });
  await page.goto('/');

  let previousBottom = 0;
  for (const selector of [
    '#broadcast-panel',
    '#guests-panel',
    '#studio-panel',
    '#control-panel',
  ]) {
    await expect(page.locator(selector)).toBeVisible();

    const box = await page.locator(selector).boundingBox();
    if (!box) {
      throw new Error(`panel ${selector} has no bounding box`);
    }

    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.y).toBeGreaterThanOrEqual(previousBottom - 1);
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

test('clicking a studio card switches the broadcasted studio stream', async ({ page }) => {
  const consoleURL = requireEnv('MIXER_CONSOLE_URL');
  await postAction(consoleURL, 'media-mixer', 'start');

  try {
    await page.goto('/');
    await waitForLiveStudioSlots(page, 3);
    await expect(page.locator('#broadcast-status')).toHaveAttribute('data-state', 'live', {
      timeout: 30_000,
    });

    const broadcastedSlots = page.locator(
      '#studio-card-row .card-row-slot[data-broadcasted="true"]',
    );
    await expect(broadcastedSlots).toHaveCount(1, { timeout: 30_000 });

    const current = await broadcastedSlots.getAttribute('data-participant');
    const visible = await visibleStudioParticipants(page);
    const target = visible.find((participant) => participant !== current);
    expect(target).toBeTruthy();

    const targetSlot = page.locator(
      `#studio-card-row .card-row-slot[data-participant="${target}"]`,
    );
    await targetSlot.click();

    await expect(targetSlot).toHaveAttribute('data-broadcasted', 'true', {
      timeout: 30_000,
    });
    await expect(broadcastedSlots).toHaveCount(1);
    await expect(page.locator('#broadcast-status')).toHaveAttribute('data-state', 'live', {
      timeout: 30_000,
    });

    const status = await (await fetch(`${baseURL(consoleURL)}/api/subsystems`)).json();
    const mixer = status.subsystems.find(
      (subsystem: { id: string }) => subsystem.id === 'media-mixer',
    );
    expect(mixer.studio.broadcastedParticipantIdentity).toBe(target);
  } finally {
    await stopAll(consoleURL);
  }
});

test('studio card row compacts when streams disappear', async ({ page }) => {
  const room = requireEnv('MIXER_CONSOLE_STUDIO_ROOM');
  const identityPrefix = requireEnv('STUDIO_PUBLISHER_IDENTITY');
  const client = studioRoomClient();

  await page.goto('/');
  await waitForLiveStudioSlots(page, 3);

  const initial = await visibleStudioParticipants(page);
  expect(initial).toHaveLength(3);

  const publishers = (await client.listParticipants(room)).filter(
    (participant) =>
      participant.identity.startsWith(identityPrefix) &&
      participant.tracks.some((track) => track.type === TrackType.VIDEO),
  );
  expect(publishers.length).toBeGreaterThanOrEqual(4);

  await client.removeParticipant(room, initial[0]);
  await expect.poll(async () => activeStudioSlots(page).count(), { timeout: 30_000 }).toBe(3);

  await client.removeParticipant(room, initial[1]);
  await expect.poll(async () => activeStudioSlots(page).count(), { timeout: 30_000 }).toBe(2);

  const emptySlots = await page.$$eval('#studio-card-row .card-row-slot', (slots) =>
    slots.map((slot) => (slot as HTMLElement).dataset.empty === 'true'),
  );
  expect(emptySlots).toEqual([false, false, true]);

  const remaining = (await client.listParticipants(room)).filter(
    (participant) =>
      participant.identity.startsWith(identityPrefix) &&
      participant.tracks.some((track) => track.type === TrackType.VIDEO),
  );
  for (const participant of remaining) {
    await client.removeParticipant(room, participant.identity);
  }

  await expect.poll(async () => activeStudioSlots(page).count(), { timeout: 30_000 }).toBe(0);
  await expect(page.locator('#studio-status')).toHaveAttribute('data-state', 'waiting', {
    timeout: 30_000,
  });
  await expect(studioCardRow(page).locator('[data-role="previous"]')).toBeHidden();
  await expect(studioCardRow(page).locator('[data-role="next"]')).toBeHidden();
});
