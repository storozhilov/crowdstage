import {
  Room,
  Track,
  VideoPresets,
  createLocalVideoTrack,
  type LocalVideoTrack,
} from 'livekit-client';

interface TokenResponse {
  url: string;
  room: string;
  identity: string;
  token: string;
}

interface CameraPublisher {
  label: string;
  room: Room;
  track: LocalVideoTrack;
}

interface CameraEntry {
  deviceId: string;
  label: string;
}

export interface CameraSourcesOptions {
  listSelector: string;
  statusSelector: string;
  tokenURL?: string;
}

const DEFAULT_TOKEN_URL = '/token?room=studio-camera';
const CONNECT_TIMEOUT_MS = 15_000;
const PUBLISH_TIMEOUT_MS = 15_000;
const UNPUBLISH_TIMEOUT_MS = 5_000;
const DISCONNECT_TIMEOUT_MS = 5_000;

function getElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function deviceLabel(device: MediaDeviceInfo, index: number): string {
  const label = device.label.trim();
  return label === '' ? `Camera ${index + 1}` : label;
}

function aliasPreference(device: MediaDeviceInfo): number {
  const label = device.label.toLowerCase();
  let score = 0;

  if (/\b(rgb|color|colour)\b/.test(label)) {
    score += 2;
  }
  if (/\b(ir|infrared|depth|thermal)\b/.test(label)) {
    score -= 2;
  }
  if (/\bmetadata\b/.test(label)) {
    score -= 4;
  }

  return score;
}

function normalizedCameraName(device: MediaDeviceInfo): string {
  return device.label
    .toLowerCase()
    .replace(/\b(rgb|color|colour|ir|infrared|depth|thermal|metadata)\b/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

function devicesAreAliases(left: MediaDeviceInfo, right: MediaDeviceInfo): boolean {
  const leftLabel = left.label.trim().toLowerCase();
  const rightLabel = right.label.trim().toLowerCase();
  if (leftLabel === rightLabel) {
    return true;
  }

  const leftName = normalizedCameraName(left);
  return leftName !== '' && leftName === normalizedCameraName(right);
}

function uniqueCameraDevices(allDevices: MediaDeviceInfo[]): MediaDeviceInfo[] {
  const selected: Array<{
    device: MediaDeviceInfo;
    index: number;
    key: string;
    score: number;
  }> = [];

  allDevices.forEach((device, index) => {
    if (device.kind !== 'videoinput') {
      return;
    }

    // Some camera drivers expose several video nodes for the same physical
    // device, such as a regular and an infrared sensor. Collapse only devices
    // whose labels identify them as aliases; unrelated cameras can share a
    // browser device group.
    const key = device.groupId || device.deviceId || `camera-${index}`;
    const score = aliasPreference(device);
    const existingIndex = selected.findIndex(
      (entry) => entry.key === key && devicesAreAliases(entry.device, device),
    );

    if (existingIndex < 0) {
      selected.push({ device, index, key, score });
    } else if (score > selected[existingIndex].score) {
      selected[existingIndex] = { device, index, key, score };
    }
  });

  return selected
    .sort((left, right) => left.index - right.index)
    .map((entry) => entry.device);
}

function withTimeout<T>(
  operation: Promise<T>,
  timeoutMs: number,
  message: string,
): Promise<T> {
  let timer: number | undefined;

  return Promise.race([
    operation,
    new Promise<never>((_resolve, reject) => {
      timer = window.setTimeout(() => reject(new Error(message)), timeoutMs);
    }),
  ]).finally(() => {
    if (timer !== undefined) {
      window.clearTimeout(timer);
    }
  });
}

export function connectCameraSources(options: CameraSourcesOptions): void {
  const list = getElement<HTMLElement>(options.listSelector);
  const status = getElement<HTMLParagraphElement>(options.statusSelector);
  const tokenURL = options.tokenURL ?? DEFAULT_TOKEN_URL;

  const publishers = new Map<string, CameraPublisher>();
  const pending = new Set<string>();
  let devices: MediaDeviceInfo[] = [];

  function setStatus(state: string, text: string): void {
    status.dataset.state = state;
    status.textContent = text;
  }

  function currentEntries(): CameraEntry[] {
    const entries = devices.map((device, index) => ({
      deviceId: device.deviceId,
      label: deviceLabel(device, index),
    }));

    for (const [deviceId, publisher] of publishers) {
      if (!entries.some((entry) => entry.deviceId === deviceId)) {
        entries.push({ deviceId, label: publisher.label });
      }
    }

    return entries;
  }

  function createButton(entry: CameraEntry): HTMLButtonElement {
    const added = publishers.has(entry.deviceId);
    const busy = pending.has(entry.deviceId);

    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'camera-device';
    button.dataset.deviceId = entry.deviceId;
    button.dataset.cameraLabel = entry.label;
    button.dataset.state = busy ? 'busy' : added ? 'added' : 'available';
    button.setAttribute('aria-pressed', added ? 'true' : 'false');
    button.disabled = busy;

    const name = document.createElement('span');
    name.className = 'camera-device-name';
    name.textContent = entry.label;

    const action = document.createElement('span');
    action.className = 'camera-device-action';
    action.textContent = busy ? '...' : added ? 'Remove' : 'Add';

    button.append(name, action);
    button.addEventListener('click', () => {
      void toggle(entry.deviceId, entry.label);
    });

    return button;
  }

  function render(): void {
    const entries = currentEntries();
    list.dataset.count = String(entries.length);
    list.replaceChildren(...entries.map(createButton));

    if (entries.length === 0) {
      setStatus('empty', 'No camera devices found.');
    } else if (
      status.dataset.state === 'loading' ||
      status.dataset.state === 'empty'
    ) {
      setStatus('ready', '');
    }
  }

  async function refreshDevices(): Promise<void> {
    if (!navigator.mediaDevices?.enumerateDevices) {
      devices = [];
      setStatus('error', 'Camera devices are not available in this browser.');
      render();
      return;
    }

    const allDevices = await navigator.mediaDevices.enumerateDevices();
    devices = uniqueCameraDevices(allDevices);
    render();
  }

  async function acquireCameraPermission(): Promise<void> {
    if (!navigator.mediaDevices?.getUserMedia) {
      return;
    }

    try {
      const stream = await navigator.mediaDevices.getUserMedia({ video: true });
      for (const track of stream.getTracks()) {
        track.stop();
      }
    } catch {
      // Device enumeration still works when permission is denied, although the
      // browser may hide labels until a camera is actually opened.
    }
  }

  async function addCamera(deviceId: string, label: string): Promise<void> {
    const response = await fetch(`${tokenURL}&name=${encodeURIComponent(label)}`);
    if (!response.ok) {
      throw new Error(`token request failed with status ${response.status}`);
    }

    const credentials = (await response.json()) as TokenResponse;
    if (!credentials.url || !credentials.token || !credentials.room) {
      throw new Error('token response is incomplete');
    }

    const room = new Room({ adaptiveStream: false, dynacast: false });
    let track: LocalVideoTrack | null = null;

    try {
      track = await createLocalVideoTrack({
        deviceId: { exact: deviceId },
        resolution: VideoPresets.h360.resolution,
      });
      await withTimeout(
        room.connect(credentials.url, credentials.token),
        CONNECT_TIMEOUT_MS,
        'connecting to the studio room timed out',
      );
      await withTimeout(
        room.localParticipant.publishTrack(track, {
          name: label,
          source: Track.Source.Camera,
          simulcast: false,
        }),
        PUBLISH_TIMEOUT_MS,
        'publishing the camera stream timed out',
      );
    } catch (error) {
      track?.stop();
      await withTimeout(
        room.disconnect(),
        DISCONNECT_TIMEOUT_MS,
        'disconnecting from the studio room timed out',
      ).catch(() => undefined);
      throw error;
    }

    publishers.set(deviceId, { label, room, track });
  }

  async function removeCamera(deviceId: string): Promise<void> {
    const publisher = publishers.get(deviceId);
    if (!publisher) {
      return;
    }

    try {
      await withTimeout(
        publisher.room.localParticipant.unpublishTrack(publisher.track),
        UNPUBLISH_TIMEOUT_MS,
        'unpublishing the camera stream timed out',
      );
    } finally {
      publisher.track.stop();
      try {
        await withTimeout(
          publisher.room.disconnect(),
          DISCONNECT_TIMEOUT_MS,
          'disconnecting from the studio room timed out',
        );
      } finally {
        publishers.delete(deviceId);
      }
    }
  }

  async function toggle(deviceId: string, label: string): Promise<void> {
    if (pending.has(deviceId)) {
      return;
    }

    const adding = !publishers.has(deviceId);
    pending.add(deviceId);
    setStatus('busy', adding ? `Adding ${label}...` : `Removing ${label}...`);
    render();

    try {
      if (adding) {
        await addCamera(deviceId, label);
      } else {
        await removeCamera(deviceId);
      }
      setStatus('ready', '');
    } catch (error) {
      setStatus(
        'error',
        `${adding ? 'Could not add' : 'Could not remove'} ${label}: ${errorMessage(error)}`,
      );
    } finally {
      pending.delete(deviceId);
      await refreshDevices().catch((error: unknown) => {
        setStatus('error', `Could not list camera devices: ${errorMessage(error)}`);
      });
      render();
    }
  }

  navigator.mediaDevices?.addEventListener('devicechange', () => {
    void refreshDevices().catch((error: unknown) => {
      setStatus('error', `Could not list camera devices: ${errorMessage(error)}`);
    });
  });

  let pageHidden = false;
  function stopAllPublishers(): void {
    if (pageHidden) {
      return;
    }
    pageHidden = true;

    for (const publisher of publishers.values()) {
      publisher.track.stop();
      void publisher.room.disconnect().catch(() => undefined);
    }
    publishers.clear();
    render();
  }

  window.addEventListener('pagehide', stopAllPublishers);
  window.addEventListener('beforeunload', stopAllPublishers);

  setStatus('loading', 'Looking for cameras...');
  void acquireCameraPermission()
    .then(refreshDevices)
    .catch((error: unknown) => {
      setStatus('error', `Could not list camera devices: ${errorMessage(error)}`);
      render();
    });
}
