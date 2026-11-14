import { RoomServiceClient, TrackType } from 'livekit-server-sdk';

const WAIT_TIMEOUT_MS = 30_000;
const POLL_INTERVAL_MS = 500;

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
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

async function studioPublisherReady(
  client: RoomServiceClient,
  room: string,
  identityPrefix: string,
): Promise<boolean> {
  const participants = await client.listParticipants(room);
  return participants.some(
    (participant) =>
      participant.identity.startsWith(identityPrefix) &&
      participant.tracks.some((track) => track.type === TrackType.VIDEO),
  );
}

export default async function waitForStudioPublisher(): Promise<void> {
  const room = requireEnv('MIXER_CONSOLE_STUDIO_ROOM');
  const identityPrefix = requireEnv('STUDIO_PUBLISHER_IDENTITY');
  const client = new RoomServiceClient(
    livekitHTTPURL(requireEnv('LIVEKIT_URL')),
    requireEnv('LIVEKIT_API_KEY'),
    requireEnv('LIVEKIT_API_SECRET'),
  );

  const deadline = Date.now() + WAIT_TIMEOUT_MS;
  while (Date.now() < deadline) {
    try {
      if (await studioPublisherReady(client, room, identityPrefix)) {
        return;
      }
    } catch {
      // LiveKit may not be ready to answer yet; retry until the deadline.
    }
    await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
  }

  throw new Error(
    `studio publisher ${identityPrefix}* did not publish video to room ${room} within ${
      WAIT_TIMEOUT_MS / 1000
    }s`,
  );
}
