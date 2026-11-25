import { randomUUID } from 'node:crypto';
import http from 'node:http';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

import { AccessToken } from 'livekit-server-sdk';

const DEFAULT_LISTEN_ADDR = ':7982';
const DEFAULT_MEDIA_MIXER_URL = 'http://127.0.0.1:7981';
const DEFAULT_MOSAIC_COMPOSER_URL = 'http://127.0.0.1:7980';
const DEFAULT_HEALTHCHECK_URL = 'http://127.0.0.1:7982/health';
const DEFAULT_IDENTITY = 'control-plane';
const MAX_TOKEN_NAME_LENGTH = 64;
const MAX_REQUEST_BODY_BYTES = 1024 * 1024;
const TOKEN_TTL = '1h';
const SUBSYSTEM_TIMEOUT_MS = 5000;

const REQUIRED_ENV = [
  ['livekitUrl', 'CONTROL_PLANE_LIVEKIT_URL'],
  ['apiKey', 'LIVEKIT_API_KEY'],
  ['apiSecret', 'LIVEKIT_API_SECRET'],
  ['broadcastRoom', 'CONTROL_PLANE_BROADCAST_ROOM'],
  ['guestsRoom', 'CONTROL_PLANE_GUESTS_ROOM'],
  ['guestsSourceRoom', 'CONTROL_PLANE_GUESTS_SOURCE_ROOM'],
  ['studioRoom', 'CONTROL_PLANE_STUDIO_ROOM'],
  ['allowedOrigins', 'CONTROL_PLANE_ALLOWED_ORIGINS'],
];

const TOKEN_ROOMS = new Map([
  ['broadcast', { room: (config) => config.broadcastRoom }],
  ['guests', { room: (config) => config.guestsRoom }],
  ['guests-sources', { room: (config) => config.guestsSourceRoom }],
  ['studio', { room: (config) => config.studioRoom }],
  [
    'studio-camera',
    {
      room: (config) => config.studioRoom,
      canPublish: true,
      canSubscribe: false,
      defaultName: 'Mixer Console Camera',
    },
  ],
]);



const SUBSYSTEMS = [
  {
    id: 'media-mixer',
    title: 'Media mixer',
    urlKey: 'mediaMixerURL',
    pipelineState: (health) => health?.subsystems?.mixer?.state ?? null,
    parameters: (health) => {
      const livekit = health?.subsystems?.livekit ?? {};
      const mixer = health?.subsystems?.mixer ?? {};
      return [
        parameter('LiveKit connection', livekit.connection_state),
        parameter('Studio LiveKit room', livekit.room),
        parameter('Broadcast LiveKit room', livekit.target_room),
        parameter('Inputs', mixer.inputs),
        parameter('Outputs', mixer.outputs),
        parameter('Guest broadcast', formatSeconds(mixer.guest_broadcast_seconds)),
      ];
    },
    studio: (health) => {
      const mixer = health?.subsystems?.mixer ?? {};
      const broadcasted = mixer.broadcasted_participant_identity;
      const publishing = mixer.publishing_participant_identities;
      return {
        broadcastedParticipantIdentity:
          typeof broadcasted === 'string' && broadcasted !== '' ? broadcasted : null,
        publishedParticipantIdentities: Array.isArray(publishing)
          ? publishing.filter((identity) => typeof identity === 'string')
          : [],
      };
    },
  },
  {
    id: 'mosaic-composer',
    title: 'Mosaic composer',
    urlKey: 'mosaicComposerURL',
    pipelineState: (health) => health?.subsystems?.mosaic?.state ?? null,
    parameters: (health) => {
      const livekit = health?.subsystems?.livekit ?? {};
      const mosaic = health?.subsystems?.mosaic ?? {};
      return [
        parameter('LiveKit connection', livekit.connection_state),
        parameter('Guests LiveKit room', livekit.room),
        parameter('Mosaic LiveKit room', livekit.target_room),
        parameter('Guests', mosaic.guests),
        parameter('Grid', formatGrid(mosaic.grid)),
        parameter('Rotation timeout', formatSeconds(mosaic.rotation_timeout_seconds)),
      ];
    },
  },
];

export function configFromEnv(env = process.env) {
  const config = {
    listenAddr: env.CONTROL_PLANE_LISTEN_ADDR || DEFAULT_LISTEN_ADDR,
    mediaMixerURL: env.CONTROL_PLANE_MEDIA_MIXER_URL || DEFAULT_MEDIA_MIXER_URL,
    mosaicComposerURL: env.CONTROL_PLANE_MOSAIC_COMPOSER_URL || DEFAULT_MOSAIC_COMPOSER_URL,
    healthcheckURL: env.CONTROL_PLANE_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL,
    livekitUrl: env.CONTROL_PLANE_LIVEKIT_URL || '',
    apiKey: env.LIVEKIT_API_KEY || '',
    apiSecret: env.LIVEKIT_API_SECRET || '',
    broadcastRoom: env.CONTROL_PLANE_BROADCAST_ROOM || '',
    guestsRoom: env.CONTROL_PLANE_GUESTS_ROOM || '',
    guestsSourceRoom: env.CONTROL_PLANE_GUESTS_SOURCE_ROOM || '',
    studioRoom: env.CONTROL_PLANE_STUDIO_ROOM || '',
    identityPrefix: env.CONTROL_PLANE_IDENTITY || DEFAULT_IDENTITY,
    allowedOrigins: parseAllowedOrigins(env.CONTROL_PLANE_ALLOWED_ORIGINS),
  };

  const missing = REQUIRED_ENV
    .filter(([key]) => {
      const value = config[key];
      return Array.isArray(value) ? value.length === 0 : value === '';
    })
    .map(([, name]) => name);

  if (missing.length > 0) {
    throw new Error(`missing required setting(s): ${missing.join(', ')}`);
  }

  return config;
}

export function parseListenAddr(address) {
  const separator = address.lastIndexOf(':');
  if (separator === -1) {
    throw new Error(`invalid listen address: ${address}`);
  }

  const host = address.slice(0, separator);
  const port = Number(address.slice(separator + 1));

  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new Error(`invalid listen address: ${address}`);
  }

  return { host: host || undefined, port };
}

export function createRequestHandler(config) {
  return async (request, response) => {
    const origin = request.headers.origin;

    if (origin && !config.allowedOrigins.includes(origin)) {
      sendJson(response, 403, { status: 'error', error: 'origin not allowed' });
      return;
    }

    if (origin) {
      response.setHeader('Access-Control-Allow-Origin', origin);
      response.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
      response.setHeader('Access-Control-Allow-Headers', 'Content-Type');
      response.setHeader('Vary', 'Origin');
    }

    try {
      if (request.method === 'OPTIONS') {
        if (!origin) {
          sendJson(response, 400, { status: 'error', error: 'origin is required' });
          return;
        }
        response.writeHead(204);
        response.end();
        return;
      }
      await handleRequest(config, request, response);
    } catch (error) {
      console.error('control-plane: request failed:', error);
      if (!response.headersSent) {
        sendJson(response, 500, { status: 'error', error: 'internal server error' });
      } else {
        response.end();
      }
    }
  };
}

async function handleRequest(config, request, response) {
  const url = new URL(request.url ?? '/', 'http://control-plane.local');

  if (url.pathname === '/health') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }
    sendJson(response, 200, { status: 'ok' });
    return;
  }

  if (url.pathname === '/token') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }

    const roomKey = url.searchParams.get('room') ?? 'broadcast';
    const tokenRoom = TOKEN_ROOMS.get(roomKey);
    const directRoom = configuredRooms(config).has(roomKey);
    if (!tokenRoom && !directRoom) {
      sendJson(response, 400, { status: 'error', error: 'unknown token room' });
      return;
    }
    const room = tokenRoom ? tokenRoom.room(config) : roomKey;
    const canPublish = tokenRoom?.canPublish === true;
    const requestedName = sanitizeTokenName(url.searchParams.get('name'));
    const identityPrefix =
      sanitizeIdentityPrefix(url.searchParams.get('identity_prefix')) || config.identityPrefix;

    const identity = canPublish
      ? `${identityPrefix}-camera-${randomUUID()}`
      : `${identityPrefix}-${randomUUID()}`;
    const defaultName = tokenRoom?.defaultName ?? (tokenRoom ? 'Mixer Console' : 'Bus Monitor');
    const accessToken = new AccessToken(config.apiKey, config.apiSecret, {
      identity,
      name: canPublish && requestedName ? requestedName : defaultName,
      ttl: TOKEN_TTL,
    });
    accessToken.addGrant({
      roomJoin: true,
      room,
      canPublish,
      canSubscribe: tokenRoom?.canSubscribe ?? true,
    });

    sendJson(response, 200, {
      url: config.livekitUrl,
      room,
      identity,
      token: await accessToken.toJwt(),
    });
    return;
  }

  if (url.pathname === '/api/subsystems') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }
    sendJson(response, 200, await readStatus(config));
    return;
  }

  const actionMatch = url.pathname.match(/^\/api\/subsystems\/([^/]+)\/(start|stop|switch)$/);
  if (actionMatch) {
    if (request.method !== 'POST') {
      methodNotAllowed(response, ['POST']);
      return;
    }

    const subsystem = SUBSYSTEMS.find((entry) => entry.id === actionMatch[1]);
    if (!subsystem) {
      sendJson(response, 404, { status: 'error', error: 'unknown subsystem' });
      return;
    }

    if (actionMatch[2] === 'switch') {
      if (!subsystem.studio) {
        sendJson(response, 404, { status: 'error', error: 'unknown subsystem action' });
        return;
      }

      const body = await readJsonRequest(request);
      const participantIdentity = body?.participant_identity;
      if (typeof participantIdentity !== 'string' || participantIdentity.trim() === '') {
        sendJson(response, 400, {
          status: 'error',
          error: 'participant_identity is required',
        });
        return;
      }

      const result = await setSubsystemSource(
        subsystem,
        config[subsystem.urlKey],
        participantIdentity.trim(),
      );
      sendJson(response, result.statusCode, result.body);
      return;
    }

    const result = await setSubsystemPipeline(
      subsystem,
      config[subsystem.urlKey],
      actionMatch[2],
    );
    sendJson(response, result.statusCode, result.body);
    return;
  }

  sendJson(response, 404, { status: 'error', error: 'not found' });
}

async function readStatus(config) {
  const subsystems = await Promise.all(
    SUBSYSTEMS.map((subsystem) => readSubsystemStatus(subsystem, config[subsystem.urlKey])),
  );

  return {
    status: subsystems.every((subsystem) => subsystem.reachable && subsystem.status === 'ok')
      ? 'ok'
      : 'degraded',
    subsystems,
  };
}

async function readSubsystemStatus(subsystem, baseURL) {
  let response;
  try {
    response = await fetch(`${baseURL}/health`, {
      signal: AbortSignal.timeout(SUBSYSTEM_TIMEOUT_MS),
    });
  } catch (error) {
    return {
      id: subsystem.id,
      title: subsystem.title,
      reachable: false,
      status: 'unreachable',
      state: null,
      parameters: [],
      error: errorMessage(error),
    };
  }

  const health = await parseJsonBody(response);
  if (health === null) {
    return {
      id: subsystem.id,
      title: subsystem.title,
      reachable: true,
      status: 'error',
      state: null,
      parameters: [],
      error: `health endpoint returned HTTP ${response.status} without a JSON body`,
    };
  }

  const status = {
    id: subsystem.id,
    title: subsystem.title,
    reachable: true,
    status: typeof health.status === 'string' ? health.status : 'unknown',
    state: subsystem.pipelineState(health),
    parameters: subsystem.parameters(health).filter((entry) => entry !== null),
  };
  if (subsystem.studio) {
    status.studio = subsystem.studio(health);
  }
  return status;
}

async function setSubsystemSource(subsystem, baseURL, participantIdentity) {
  try {
    const response = await fetch(`${baseURL}/switch`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ participant_identity: participantIdentity }),
      signal: AbortSignal.timeout(SUBSYSTEM_TIMEOUT_MS),
    });
    const body = await parseJsonBody(response);

    const result = {
      id: subsystem.id,
      title: subsystem.title,
      ok: response.ok,
    };
    if (body !== null) {
      result.response = body;
    }
    if (!response.ok) {
      result.error = body?.error ?? `subsystem returned HTTP ${response.status}`;
      return { statusCode: 502, body: { status: 'error', subsystem: result } };
    }
    return {
      statusCode: 200,
      body: {
        status: 'switched',
        subsystem: result,
      },
    };
  } catch (error) {
    return {
      statusCode: 502,
      body: {
        status: 'error',
        subsystem: {
          id: subsystem.id,
          title: subsystem.title,
          ok: false,
          error: errorMessage(error),
        },
      },
    };
  }
}

async function setSubsystemPipeline(subsystem, baseURL, action) {
  try {
    const response = await fetch(`${baseURL}/${action}`, {
      method: 'POST',
      signal: AbortSignal.timeout(SUBSYSTEM_TIMEOUT_MS),
    });
    const body = await parseJsonBody(response);

    const result = {
      id: subsystem.id,
      title: subsystem.title,
      ok: response.ok,
    };
    if (body !== null) {
      result.response = body;
    }
    if (!response.ok) {
      result.error = `subsystem returned HTTP ${response.status}`;
      return { statusCode: 502, body: { status: 'error', subsystem: result } };
    }
    return {
      statusCode: 200,
      body: {
        status: action === 'start' ? 'started' : 'stopped',
        subsystem: result,
      },
    };
  } catch (error) {
    return {
      statusCode: 502,
      body: {
        status: 'error',
        subsystem: {
          id: subsystem.id,
          title: subsystem.title,
          ok: false,
          error: errorMessage(error),
        },
      },
    };
  }
}

function sanitizeTokenName(value) {
  if (typeof value !== 'string') {
    return '';
  }
  const printable = Array.from(value, (character) => {
    const code = character.codePointAt(0) ?? 0;
    return code < 0x20 || (code >= 0x7f && code <= 0x9f) ? ' ' : character;
  }).join('');
  return printable.replace(/\s+/g, ' ').trim().slice(0, MAX_TOKEN_NAME_LENGTH);
}

function sanitizeIdentityPrefix(value) {
  if (typeof value !== 'string') {
    return '';
  }
  return sanitizeTokenName(value).replace(/\s+/g, '-');
}

function parseAllowedOrigins(value) {
  if (typeof value !== 'string') {
    return [];
  }
  return value
    .split(',')
    .map((origin) => origin.trim())
    .filter((origin) => origin !== '');
}

function configuredRooms(config) {
  return new Set([
    config.broadcastRoom,
    config.guestsRoom,
    config.guestsSourceRoom,
    config.studioRoom,
  ]);
}

function parameter(name, value) {
  if (value === null || value === undefined) {
    return null;
  }
  return { name, value: String(value) };
}

function formatGrid(grid) {
  if (
    typeof grid !== 'object' ||
    grid === null ||
    !Number.isInteger(grid.columns) ||
    !Number.isInteger(grid.rows)
  ) {
    return null;
  }
  return `${grid.columns}x${grid.rows}`;
}

function formatSeconds(value) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return null;
  }
  return `${value}s`;
}

async function readJsonRequest(request) {
  const chunks = [];
  let totalBytes = 0;
  for await (const chunk of request) {
    totalBytes += chunk.length;
    if (totalBytes > MAX_REQUEST_BODY_BYTES) {
      return null;
    }
    chunks.push(chunk);
  }
  try {
    const body = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (typeof body !== 'object' || body === null || Array.isArray(body)) {
      return null;
    }
    return body;
  } catch {
    return null;
  }
}

async function parseJsonBody(response) {
  try {
    const body = await response.json();
    if (typeof body !== 'object' || body === null || Array.isArray(body)) {
      return null;
    }
    return body;
  } catch {
    return null;
  }
}


function sendJson(response, statusCode, body) {
  const payload = JSON.stringify(body);
  response.writeHead(statusCode, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
    'Cache-Control': 'no-store',
  });
  response.end(payload);
}

function methodNotAllowed(response, allowedMethods) {
  const payload = JSON.stringify({ status: 'error', error: 'method not allowed' });
  response.writeHead(405, {
    Allow: allowedMethods.join(', '),
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
  });
  response.end(payload);
}


export async function startControlPlane(config) {
  const server = http.createServer(createRequestHandler(config));
  const { host, port } = parseListenAddr(config.listenAddr);

  await new Promise((resolve, reject) => {
    const onError = (error) => {
      reject(error);
    };
    const onListening = () => {
      server.off('error', onError);
      resolve();
    };

    server.once('error', onError);
    if (host) {
      server.listen(port, host, onListening);
    } else {
      server.listen(port, onListening);
    }
  });

  return server;
}

async function runHealthcheck(url) {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
    if (!response.ok) {
      console.error(`healthcheck GET ${url} status = ${response.status}`);
      return 1;
    }
    return 0;
  } catch (error) {
    console.error(`healthcheck GET ${url}: ${errorMessage(error)}`);
    return 1;
  }
}

async function main() {
  if (process.argv[2] === 'healthcheck') {
    const url = process.env.CONTROL_PLANE_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL;
    process.exitCode = await runHealthcheck(url);
    return;
  }

  const config = configFromEnv();
  const server = await startControlPlane(config);

  console.log(`control-plane: listening on ${config.listenAddr}`);
  console.log(`control-plane: MediaMixer at ${config.mediaMixerURL}`);
  console.log(`control-plane: MosaicComposer at ${config.mosaicComposerURL}`);
  console.log(`control-plane: LiveKit room ${config.broadcastRoom} at ${config.livekitUrl}`);
  console.log(`control-plane: LiveKit room ${config.guestsRoom} at ${config.livekitUrl}`);
  console.log(
    `control-plane: LiveKit room ${config.guestsSourceRoom} at ${config.livekitUrl}`,
  );
  console.log(`control-plane: LiveKit room ${config.studioRoom} at ${config.livekitUrl}`);

  const shutdown = (signal) => {
    console.log(`control-plane: received ${signal}; shutting down`);
    server.close((error) => {
      if (error) {
        console.error(`control-plane: shutdown failed: ${errorMessage(error)}`);
        process.exit(1);
      }
      process.exit(0);
    });
  };

  process.once('SIGINT', () => shutdown('SIGINT'));
  process.once('SIGTERM', () => shutdown('SIGTERM'));
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

const entryPoint = process.argv[1]
  ? pathToFileURL(path.resolve(process.argv[1])).href
  : '';

if (entryPoint === import.meta.url) {
  main().catch((error) => {
    console.error(`control-plane: ${errorMessage(error)}`);
    process.exit(1);
  });
}
