import { Room, RoomEvent, Track } from 'livekit-client';

interface TokenResponse {
  url: string;
  room: string;
  identity: string;
  token: string;
}

const video = getElement<HTMLVideoElement>('#broadcast-video');
const status = getElement<HTMLParagraphElement>('#broadcast-status');

let room: Room | null = null;
let roomName = 'the broadcast room';

function getElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function setState(state: string, message: string): void {
  status.dataset.state = state;
  status.textContent = message;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

function playVideo(): void {
  void video.play().catch((error: unknown) => {
    if (isAbortError(error)) {
      return;
    }
    setState('error', `Could not start broadcast playback: ${errorMessage(error)}`);
  });
}

function remoteVideoTracks(): Track[] {
  if (!room) {
    return [];
  }

  const tracks: Track[] = [];
  for (const participant of room.remoteParticipants.values()) {
    for (const publication of participant.trackPublications.values()) {
      if (publication.kind === Track.Kind.Video && publication.track) {
        tracks.push(publication.track);
      }
    }
  }

  return tracks;
}

function hasRemoteVideo(): boolean {
  return remoteVideoTracks().length > 0;
}

function attachFirstRemoteVideo(): boolean {
  const [track] = remoteVideoTracks();
  if (!track) {
    return false;
  }

  track.attach(video);
  playVideo();
  return true;
}

function setWaitingIfNoVideo(): void {
  if (hasRemoteVideo()) {
    return;
  }

  setState('waiting', `Waiting for broadcast media in ${roomName}...`);
}

async function connect(): Promise<void> {
  setState('connecting', 'Connecting to the broadcast room...');

  try {
    const response = await fetch('/token');
    if (!response.ok) {
      throw new Error(`token request failed with status ${response.status}`);
    }

    const credentials = (await response.json()) as TokenResponse;
    if (!credentials.url || !credentials.token || !credentials.room) {
      throw new Error('token response is incomplete');
    }

    roomName = credentials.room;
    setState('connecting', `Connecting to ${roomName}...`);

    room = new Room({ adaptiveStream: false, dynacast: false });

    room.on(RoomEvent.TrackSubscribed, (track) => {
      if (track.kind !== Track.Kind.Video) {
        return;
      }

      track.attach(video);
      playVideo();
      setState('live', `Broadcast is live in ${roomName}`);
    });

    room.on(RoomEvent.TrackUnsubscribed, (track) => {
      if (track.kind !== Track.Kind.Video) {
        return;
      }

      track.detach(video);
      if (attachFirstRemoteVideo()) {
        setState('live', `Broadcast is live in ${roomName}`);
      } else {
        setState('waiting', `Waiting for broadcast media in ${roomName}...`);
      }
    });

    room.on(RoomEvent.TrackUnpublished, (publication) => {
      if (publication.kind === Track.Kind.Video) {
        setWaitingIfNoVideo();
      }
    });

    room.on(RoomEvent.Reconnecting, () => {
      setState('reconnecting', `Reconnecting to ${roomName}...`);
    });

    room.on(RoomEvent.Reconnected, () => {
      if (hasRemoteVideo()) {
        setState('live', `Broadcast is live in ${roomName}`);
      } else {
        setState('waiting', `Waiting for broadcast media in ${roomName}...`);
      }
    });

    room.on(RoomEvent.Disconnected, (reason) => {
      setState('disconnected', `Disconnected from ${roomName} (${String(reason)})`);
    });

    await room.connect(credentials.url, credentials.token);

    if (status.dataset.state === 'connecting') {
      if (!attachFirstRemoteVideo()) {
        setWaitingIfNoVideo();
      }
    }
  } catch (error) {
    setState('error', `Broadcast error: ${errorMessage(error)}`);
  }
}

export function connectBroadcast(): void {
  void connect();
}
