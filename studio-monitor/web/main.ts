import { Room, RoomEvent, Track } from 'livekit-client';

interface TokenResponse {
  url: string;
  room: string;
  identity: string;
  token: string;
}

const video = getElement<HTMLVideoElement>('#video');
const status = getElement<HTMLDivElement>('#status');

let room: Room | null = null;

function getElement<T extends Element>(selector: string): T {
  const element = document.querySelector<T>(selector);
  if (!element) {
    throw new Error(`missing element: ${selector}`);
  }
  return element;
}

function setState(state: string, message: string): void {
  document.body.dataset.state = state;
  status.dataset.state = state;
  status.textContent = message;
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function hasRemoteVideo(): boolean {
  if (!room) {
    return false;
  }

  for (const participant of room.remoteParticipants.values()) {
    for (const publication of participant.trackPublications.values()) {
      if (publication.kind === Track.Kind.Video) {
        return true;
      }
    }
  }

  return false;
}

function setWaitingIfNoVideo(): void {
  if (!hasRemoteVideo()) {
    setState('waiting', 'Waiting for studio stream...');
  }
}

async function connect(): Promise<void> {
  setState('connecting', 'Connecting to studio...');

  try {
    const response = await fetch('/token');
    if (!response.ok) {
      throw new Error(`token request failed with status ${response.status}`);
    }

    const credentials = (await response.json()) as TokenResponse;
    if (!credentials.url || !credentials.token) {
      throw new Error('token response is incomplete');
    }

    room = new Room({ adaptiveStream: false, dynacast: false });

    room.on(RoomEvent.TrackSubscribed, (track) => {
      if (track.kind !== Track.Kind.Video) {
        return;
      }

      track.attach(video);
      void video.play().catch((error: unknown) => {
        setState('error', `Could not start video playback: ${errorMessage(error)}`);
      });
      setState('live', 'Studio stream is live');
    });

    room.on(RoomEvent.TrackUnsubscribed, (track) => {
      if (track.kind !== Track.Kind.Video) {
        return;
      }

      track.detach(video);
      setWaitingIfNoVideo();
    });

    room.on(RoomEvent.TrackUnpublished, (publication) => {
      if (publication.kind === Track.Kind.Video) {
        setWaitingIfNoVideo();
      }
    });

    room.on(RoomEvent.Reconnecting, () => {
      setState('reconnecting', 'Reconnecting to studio...');
    });

    room.on(RoomEvent.Reconnected, () => {
      if (hasRemoteVideo()) {
        setState('live', 'Studio stream is live');
      } else {
        setState('waiting', 'Waiting for studio stream...');
      }
    });

    room.on(RoomEvent.Disconnected, (reason) => {
      setState('disconnected', `Disconnected from studio (${String(reason)})`);
    });

    await room.connect(credentials.url, credentials.token);

    if (document.body.dataset.state === 'connecting') {
      setWaitingIfNoVideo();
    }
  } catch (error) {
    setState('error', `Studio monitor error: ${errorMessage(error)}`);
  }
}

void connect();
