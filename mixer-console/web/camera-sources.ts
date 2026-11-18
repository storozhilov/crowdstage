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
    } else if (status.dataset.state === 'empty') {
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
    devices = allDevices.filter((device) => device.kind === 'videoinput');
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
      await room.connect(credentials.url, credentials.token);
      track = await createLocalVideoTrack({
        deviceId: { exact: deviceId },
        resolution: VideoPresets.h360.resolution,
      });
      await room.localParticipant.publishTrack(track, {
        name: label,
        source: Track.Source.Camera,
        simulcast: false,
      });
    } catch (error) {
      track?.stop();
      await room.disconnect();
      throw error;
    }

    publishers.set(deviceId, { label, room, track });
  }

  async function removeCamera(deviceId: string): Promise<void> {
    const publisher = publishers.get(deviceId);
    if (!publisher) {
      return;
    }

    publishers.delete(deviceId);
    try {
      await publisher.room.localParticipant.unpublishTrack(publisher.track);
    } finally {
      publisher.track.stop();
      await publisher.room.disconnect();
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

  setStatus('loading', 'Looking for cameras...');
  void acquireCameraPermission()
    .then(refreshDevices)
    .catch((error: unknown) => {
      setStatus('error', `Could not list camera devices: ${errorMessage(error)}`);
      render();
    });
}
