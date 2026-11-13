import { expect, test, type Page } from '@playwright/test';

interface VideoSample {
  currentTime: number;
  width: number;
  height: number;
  readyState: number;
  range: number;
}

const instance = requireEnv('BUS_MONITOR_INSTANCE');
const room = requireEnv('BUS_MONITOR_ROOM');
const controlURL = requireEnv('BUS_MONITOR_CONTROL_URL');

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

async function postPipeline(baseURL: string, path: '/start' | '/stop'): Promise<void> {
  const url = `${baseURL.replace(/\/+$/, '')}${path}`;
  const response = await fetch(url, { method: 'POST' });

  if (!response.ok) {
    throw new Error(`POST ${url} status = ${response.status}`);
  }

  const body = (await response.json()) as { status?: string };
  const expected = path === '/start' ? 'started' : 'stopped';
  if (body.status !== expected) {
    throw new Error(`POST ${url} status = ${String(body.status)}, want ${expected}`);
  }
}

async function sampleVideo(page: Page): Promise<VideoSample> {
  return page.evaluate(() => {
    const video = document.querySelector<HTMLVideoElement>('#video');
    if (!video) {
      throw new Error('video element is missing');
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

test(`${instance} bus monitor waits while its source is idle and plays after it starts`, async ({ page }) => {
  const status = page.locator('#status');

  await postPipeline(controlURL, '/stop');
  await page.goto('/');
  await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });
  await expect(status).toContainText(room);

  let sourceStarted = false;
  try {
    await postPipeline(controlURL, '/start');
    sourceStarted = true;

    await expect(status).toHaveAttribute('data-state', 'live', { timeout: 30_000 });
    await expect.poll(async () => (await sampleVideo(page)).width, { timeout: 30_000 }).toBeGreaterThan(0);

    const first = await sampleVideo(page);
    await expect
      .poll(async () => (await sampleVideo(page)).currentTime, { timeout: 15_000 })
      .toBeGreaterThan(first.currentTime + 0.2);
    await expect.poll(async () => (await sampleVideo(page)).range, { timeout: 15_000 }).toBeGreaterThan(8);
  } finally {
    if (sourceStarted) {
      await postPipeline(controlURL, '/stop').catch(() => undefined);
    }
  }

  await expect(status).toHaveAttribute('data-state', 'waiting', { timeout: 30_000 });
});

test(`${instance} bus monitor ignores aborted video playback requests`, async ({ page }) => {
  const status = page.locator('#status');

  await page.addInitScript(() => {
    HTMLMediaElement.prototype.play = () => {
      return Promise.reject(new DOMException('playback aborted', 'AbortError'));
    };
  });

  await postPipeline(controlURL, '/stop');
  await page.goto('/');

  let sourceStarted = false;
  try {
    await postPipeline(controlURL, '/start');
    sourceStarted = true;

    await expect(status).toHaveAttribute('data-state', 'live', { timeout: 30_000 });
    await expect(status).not.toContainText('Could not start video playback');
  } finally {
    if (sourceStarted) {
      await postPipeline(controlURL, '/stop').catch(() => undefined);
    }
  }
});
